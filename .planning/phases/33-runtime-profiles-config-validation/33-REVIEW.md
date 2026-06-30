---
phase: 33-runtime-profiles-config-validation
reviewed: 2026-07-01T00:00:00Z
depth: standard
files_reviewed: 16
files_reviewed_list:
  - internal/config/config.go
  - internal/config/config_runtimeprofile.go
  - internal/config/config_validate.go
  - internal/config/config_knobs.go
  - internal/agent/tools/shell_exec_env.go
  - cmd/aura/chat.go
  - cmd/aura/config.go
  - cmd/aura/config_validate.go
  - .env.example
  - internal/config/config_runtimeprofile_test.go
  - internal/config/config_knobs_test.go
  - internal/config/config_validate_test.go
  - internal/config/config_rundir_test.go
  - internal/config/config_test.go
  - internal/agent/tools/shell_exec_destructive_default_test.go
  - cmd/aura/config_validate_test.go
findings:
  critical: 1
  warning: 5
  info: 5
  total: 11
status: issues_found
---

# Phase 33: Code Review Report

**Reviewed:** 2026-07-01
**Depth:** standard
**Files Reviewed:** 16
**Status:** issues_found

## Summary

Phase 33 adds a runtime-profile config-validation system: the `RuntimeProfile` enum
+ total `ParseProfile`, the `KnobSpec` registry / kind-driven `reparsePass`, the
aggregating `ValidateProfile` with tier-gated bespoke gates, the F-002 destructive-shell
empty→defaults flip, and the `aura config validate` CLI. The tier matrix
(strict {hardened,prod} vs lenient {dev,local_trusted}; the prod-only replication /
destructive-shell / endpoint differentiators) is implemented correctly and is well
covered by table + property tests. The redaction guarantee (no secret VALUE in any
`Violation.Msg`, table, or JSON) holds — every gate names the knob and emits a
value-free message.

The defects are at the **edges** of the validator's fidelity, not its core matrix:

1. **(BLOCKER)** The CLI silently coerces an unknown `--profile` to `dev` and exits 0 —
   a CI gate invoked with a mistyped profile name returns a false green for a posture
   that was never checked.
2. **(WARNING)** `reparsePass` trims whitespace before parsing while the `envutil` leaf
   it claims to mirror does not — a whitespace-padded reliability knob passes validation
   yet is silently dropped to its default at runtime (a false-negative for the exact
   silent-fallback class the pass exists to surface).
3. A profile-typo fail-open at boot, a `null`-vs-`[]` JSON wart, a resource leak on a
   boot error path in a touched file, and an unvalidated custom destructive-pattern set.

Adversarial note: the destructive-shell flip itself is correct and moves in the *safer*
direction (gate now ON for unset/empty); no regression there.

## Critical Issues

### CR-01: `aura config validate --profile <typo>` silently lints `dev` and exits 0 (false-green security gate)

**File:** `cmd/aura/config_validate.go:54-57`
**Issue:** `--profile` is run through the *total* `config.ParseProfile`, which resolves
any unrecognized string to `ProfileDev` (the loudest, most permissive tier). The flag
value is never checked against the documented enum. So a CI pipeline that gates a
production deploy with `aura config validate --profile server_production` but mistypes
the profile (e.g. `server_prod`, `production`, `prod`) silently lints **dev**: the strict
gates (sample creds, empty RPC secret, permissive CORS, replication<2, missing web-auth,
destructive `off`) are all skipped, `anyFatal` is false, and the command exits **0**.
The renderer prints `ok: profile dev — no configuration violations`, but CI keys on the
exit code, not the banner — the production posture is reported as passing without ever
having been evaluated. This is a security-validation tool that is silently bypassable via
a typo on explicit operator input. (`ParseProfile`'s totality is defensible for the
implicit `AURA_PROFILE` env path — see WR-02 — but an *explicitly named* `--profile`
flag is operator intent that must not be silently substituted.)
**Fix:** reject an unrecognized explicit `--profile` value instead of coercing it:
```go
p := cfg.Profile
if *profileFlag != "" {
	valid := map[string]bool{
		string(config.ProfileDev): true, string(config.ProfileLocalTrusted): true,
		string(config.ProfileSingleUserHardened): true, string(config.ProfileServerProduction): true,
	}
	if !valid[strings.TrimSpace(*profileFlag)] {
		fmt.Fprintf(os.Stderr, "config validate: unknown --profile %q (want dev|local_trusted|single_user_hardened|server_production)\n", *profileFlag)
		return 2
	}
	p = config.ParseProfile(*profileFlag)
}
```

## Warnings

### WR-01: `reparsePass` trims whitespace but the `envutil` leaf does not — false-negative diagnostic

**File:** `internal/config/config_knobs.go:136-152` (vs `internal/envutil/envutil.go:22-47`)
**Issue:** `config_knobs.go` advertises that the re-parse pass "re-reads every cataloged
knob ... with the SAME stdlib mechanics envutil uses — but emits a Violation instead of
silently falling back (D-06)." It does not match the leaf. `reparsePass` parses
`strings.TrimSpace(raw)`; `envutil.IntDefault`/`BoolDefault` short-circuit only on
`v == ""` and then call `strconv.Atoi(v)` / `strconv.ParseBool(v)` on the **raw,
untrimmed** value. A whitespace-padded value (e.g. real env `AURA_AGUI_BUFFER_CAP=" 128"`,
common from YAML/templating quoting — and the production path explicitly relies on real
env vars, not the godotenv shim) therefore:
- `reparsePass`: trims to `"128"`, parses OK → **no violation** (`validate` says "ok",
  and under a strict tier no Fatal → boot is **not** refused);
- runtime: `strconv.Atoi(" 128")` fails → `envutil` **silently** returns the default 64.

The validator reports a clean config while runtime ignores the operator's value — exactly
the silent-fallback case the pass was built to catch, and a violation of acceptance
criterion #1 ("lists EVERY unmet requirement") for whitespace-padded inputs. The trim only
ever makes `reparsePass` *more* lenient than the leaf, so this is a pure false-negative.
**Fix:** mirror the leaf precisely — skip only the truly-empty/unset case and re-parse the
**raw** value, so any value `envutil` would silently drop (including whitespace-only and
whitespace-padded) is surfaced:
```go
raw, set := os.LookupEnv(k.Name)
if !set || raw == "" {
	continue // unset/empty ⇒ leaf uses its default, no diagnostic
}
switch k.Kind {
case KindInt:
	if _, err := strconv.Atoi(raw); err != nil { // raw, not TrimSpace(raw)
		vs = append(vs, Violation{Knob: k.Name, Sev: sev, Msg: "not a valid integer"})
	}
// ... KindBool/KindEnum likewise on raw
}
```

### WR-02: typo'd `AURA_PROFILE` silently boots the least-strict tier (fail-open selector)

**File:** `internal/config/config_runtimeprofile.go:38-50`; surfaced at `cmd/aura/chat.go:208-212`
**Issue:** `ParseProfile` resolves any unknown/misspelled `AURA_PROFILE` to `ProfileDev`.
An operator who sets `AURA_PROFILE=production` (correct value is `server_production`) boots
in **dev**: no web-auth required, permissive CORS allowed, sample creds allowed,
destructive-shell `off` allowed — the entire hardening posture is silently disabled. The
only signal is a single `warn: config: AURA_PROFILE: not one of ...` line on stderr
(emitted by the lenient-half loop), easy to lose in container logs, and `Validate()`
returns nil so boot proceeds. For a feature whose stated purpose is "refuse unsafe
production deploys," failing open on the profile *selector* is a notable weakness. This is
the documented D-03 totality ("never silently selects a *stricter* tier"), so it is a
deliberate tradeoff — but the inverse cost (silently selecting the *least* strict tier on a
typo) deserves operator attention.
**Fix:** keep `ParseProfile` total for compatibility, but treat an *unrecognized*
(non-empty) `AURA_PROFILE` as a hard boot error in `Validate()` rather than a Warn, so a
typo fails closed instead of silently downgrading. At minimum, document that an
unrecognized profile boots full-permissive and ensure the warn line is unmissable
(error-level, not `warn:`).

### WR-03: `aura config validate --json` emits `null` for a clean config, breaking array consumers

**File:** `cmd/aura/config_validate.go:59-67`
**Issue:** `ValidateProfile` returns a nil `[]Violation` when there are no violations (it
only ever `append`s, so all-clean ⇒ `vs == nil`). `json.Encoder.Encode` serializes a nil
slice as `null`, not `[]`. So the advertised "CI-parseable" `--json` output is `null` on
the common success path and `[ ... ]` on failure — an inconsistent schema. A CI consumer
doing `jq '.[]'` or `jq 'length'` over the result errors on `null` ("Cannot iterate over
null"), turning a clean validate into a script failure. Only the *unsafe* (non-null) case
is tested (`cmd/aura/config_validate_test.go:96`), so this is uncaught.
**Fix:** normalize to an empty array before encoding:
```go
violations := cfg.ValidateProfile(p)
if violations == nil {
	violations = []config.Violation{}
}
```

### WR-04: resource leak on the `CommandHookManagerFromEnv` error path

**File:** `cmd/aura/chat.go:282-285`
**Issue:** By the time `agent.CommandHookManagerFromEnv` is called, `pool` is open and
`mcpClosers` has been populated by `buildRegistryWithMCP` (and may include the opened
mcp-neo4j-cypher graph client from the reasoning-learning block, lines 269-280). On its
error path the function `return nil, fmt.Errorf("command hooks: %w", err)` without
`pool.Close()` or `closeMCPServers(mcpClosers)` — every other error return after the pool
is opened (lines 198, 248) closes it. The `aura chat` callsite reaches `os.Exit(1)` so the
OS reclaims it, but `bootServeChatEnv` is meant to return errors so `serve` can shut down
gracefully (the explicit Pitfall-6 contract on this function) — there, the leaked pool
connections and orphaned MCP subprocess persist. Pre-existing (commit 3b10f2c7), but this
file is in the Phase 33 touch-set and CLAUDE.md mandates cleanup-on-touch.
**Fix:**
```go
hookManager, err := agent.CommandHookManagerFromEnv(os.LookupEnv)
if err != nil {
	_ = closeMCPServers(mcpClosers)
	pool.Close()
	return nil, fmt.Errorf("command hooks: %w", err)
}
```

### WR-05: a malformed custom `AURA_SHELL_DESTRUCTIVE_PATTERNS` set passes `config validate` but breaks the shell tool at runtime

**File:** `internal/config/config_validate.go:209-218`, `internal/config/config_knobs.go:76`
**Issue:** The registry catalogs `AURA_SHELL_DESTRUCTIVE_PATTERNS` as `KindString` (no
re-parse check), and `gateDestructiveShell` only checks the literal `off`. A non-`off`,
non-empty value is a comma-separated list of RE2 patterns; if any fails to compile,
`destructiveShellPatterns()` returns an error at call time
(`internal/agent/tools/shell_exec_env.go:128-131`), so `destructiveShellMatch` errors on
every shell invocation. `aura config validate` reports this misconfiguration as clean.
For a validator whose job is to refuse unsafe deploys, not checking that the operator's
own destructive-pattern override even compiles is a gap (the runtime error-handling in
`ShellExec.Execute` is out of this review's file set, but either outcome — gate-off or
hard-block — is bad and undetected here).
**Fix:** in `gateDestructiveShell` (or a dedicated gate that runs in every tier), when the
raw value is non-empty and not `off`, split on `,` and `regexp.Compile` each part,
emitting a `Violation` (Fatal under strict) naming the knob on the first compile error —
without importing the tools leaf (mirror its split/compile logic).

## Info

### IN-01: `--json` schema uses Go-default field names and numeric severity

**File:** `internal/config/config_validate.go:45-49`, `cmd/aura/config_validate.go:61-67`
**Issue:** `Violation` has no json tags and `Severity` has no `MarshalJSON`, so the
"CI-parseable" output keys are `"Knob"`/`"Sev"`/`"Msg"` and `Sev` is an opaque `0`/`1`
(0=Warn, 1=Fatal). Stable, but undocumented and unfriendly for the advertised CI contract.
**Fix:** add json tags (`json:"knob"` etc.) and a `Severity.MarshalJSON` emitting
`"warn"`/`"fatal"`, or document the numeric mapping in the flag help.

### IN-02: duplicated magic strings (drift risk)

**File:** `internal/config/config_validate.go:213,242`
**Issue:** The env name `"AURA_SHELL_DESTRUCTIVE_PATTERNS"` is hardcoded in
`gateDestructiveShell` (the scope fence from the tools-package const is documented and
unavoidable), and `"aura-assets"` in `gateObjectStoreEndpoint` duplicates the config.go
default + the registry `Default`. A rename in one place silently desyncs validation.
**Fix:** reference shared `const`s where the package boundary allows; for the cross-package
shell-pattern name, add a test asserting the two literals stay equal.

### IN-03: `KnobSpec.Default` is never verified against the real `loadBase` fallbacks

**File:** `internal/config/config_knobs.go:58-114`, `internal/config/config_knobs_test.go`
**Issue:** The registry advertises `Default` as the authoritative catalogue mirroring
config.go ("so the registry stays the authoritative catalogue"), and it drives
.env.example/doc generation, but no test cross-checks the strings against the actual
`envutil.*Default`/`envDefault` fallbacks in `loadBase`. They match today; nothing prevents
silent drift.
**Fix:** add a test that asserts each `KindInt`/`KindBool` row's `Default` parses to the
same value `loadBase` produces with the var unset.

### IN-04: rapid tests mutate process env via `os.Setenv` instead of `t.Setenv`

**File:** `internal/config/config_knobs_test.go:200-201,237-238,262-269`
**Issue:** `TestRapidEnv*` use `os.Setenv`/`defer os.Unsetenv` rather than `t.Setenv`,
bypassing the parallel-safety guard and the automatic restore. Harmless while these tests
stay serial, but a stray `t.Parallel()` elsewhere in the package, or a non-deferred panic,
would leak mutated env into sibling tests.
**Fix:** prefer `t.Setenv` (rapid.Check runs the property on the test goroutine, so it is
permitted), or snapshot/restore explicitly.

### IN-05: `isLoopbackEndpoint` is a substring heuristic that both over- and under-matches

**File:** `internal/config/config_validate.go:254-257`
**Issue:** `strings.Contains` over-matches (`http://127.0.0.1.evil.com` → "loopback") and
under-matches (`0.0.0.0`, other `127.0.0.0/8` hosts). The code acknowledges it is an
advisory WARN, so impact is low, but the heuristic can mislabel a real prod endpoint.
**Fix:** parse with `net/url` + `net.ParseIP(host).IsLoopback()` (the gate already parses
binds that way in `GuardWebBind`); keep it WARN-only.

---

_Reviewed: 2026-07-01_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
