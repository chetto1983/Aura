# Phase 33: Runtime Profiles + Config Validation - Pattern Map

**Mapped:** 2026-06-30
**Files analyzed:** 10 (4 new source + 1 new CLI source + 3 modified source + test set + .env.example)
**Analogs found:** 10 / 10 (every new/modified file has an in-tree analog; zero "no analog")

> This phase is **"wire up and mirror"**, not net-new architecture. Every new file copies a proven, already-tested in-repo idiom. The only genuinely new logic is the `KnobSpec` registry shape and the `AURA_OBJECTSTORE_REPLICATION_FACTOR` / `GARAGE_RPC_SECRET` read path (RESEARCH §Open Q1). Everything else has a verbatim template below.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/config/config_runtimeprofile.go` (NEW) | model (typed enum) + utility (parser) | transform (string→enum, total) | `internal/config/config.go::GuardWebBind` (pure/total) + `cmd/aura/config.go::getConfigKey` (switch dispatch) | role-match |
| `internal/config/config_knobs.go` (NEW) | model (registry data) + validator (re-parse pass) | transform / validation | `internal/envutil/envutil.go::IntDefault/BoolDefault` (the strconv mechanics) + `cmd/aura/config.go::setConfigKey` (kind switch) | role-match |
| `internal/config/config_validate.go` (NEW; splits `Validate()` out of config.go) | validator / service | validation (aggregate → multi-error) | `internal/config/config.go::Validate` + `GuardWebBind` (the EXACT idiom to mirror) | **exact** |
| `internal/config/config.go` (MODIFIED) | model (root composite) | transform (env→struct at load) | itself — `loadBase()` field-population precedent | **exact (self)** |
| `internal/agent/tools/shell_exec_env.go` (MODIFIED) | utility (runtime leaf) | transform (env→[]regexp) | itself — `destructiveShellPatterns()` one-spot flip | **exact (self)** |
| `cmd/aura/config_validate.go` (NEW) | controller (CLI subcommand) | request-response (argv→stdout+exit) | `cmd/aura/config.go::configShow/configGet` (load→render→exit) | role-match |
| `cmd/aura/config.go` (MODIFIED) | controller (dispatcher) | request-response | itself — `runConfig` switch | **exact (self)** |
| `internal/config/config_runtimeprofile_test.go` (NEW) | test (table) | — | `config_webauth_test.go::TestGuardWebBind` + `config_profile_test.go` (env defaults) | **exact** |
| `internal/config/config_knobs_test.go` (NEW) + extend `config_validate_test.go` | test (table + PBT) | — | `config_validate_test.go::TestConfigValidate` + `internal/mcp/manager/envedit_property_test.go` (rapid) | **exact** |
| `cmd/aura/config_validate_test.go` (NEW) | test (e2e CLI) | — | (cmd/aura has no public-test analog for exit-code; see §No Analog) — mirror `config_validate_test.go` knob-name asserts | partial |
| extend `internal/agent/tools/shell_exec_destructive_default_test.go` (NOT `shell_exec_env_test.go`) | test (truth table) | — | itself — `TestDestructiveShellDefaultIsOverridable` | **exact (self)** |

> **Discrepancy flagged for the planner:** RESEARCH.md §Recommended Structure + §Validation Architecture + §Wave 0 Gaps all say "EXTEND existing `internal/agent/tools/shell_exec_env_test.go`". **That file does not exist.** The destructive-shell tests live in `internal/agent/tools/shell_exec_destructive_default_test.go` (85 LOC, 3 tests). Extend THAT file with the D-12 truth table. (`shell_exec_env.go` is the source; its tests are in the `_destructive_default_test.go` sibling.)

---

## Pattern Assignments

### `internal/config/config_validate.go` (NEW — validator/service) — THE LOAD-BEARING ANALOG

**Analog:** `internal/config/config.go::Validate` (lines 265-280) + `GuardWebBind` (lines 292-308). This is an **exact** match — the new file MOVES `Validate()` here verbatim (config.go is 557/600 LOC; the split is mandatory before any addition or the file-size hook blocks every commit — RESEARCH Pitfall 3 / MEMORY) and adds `ValidateProfile` + `Violation` + the ≤10 bespoke gates beside it.

**The aggregate-into-multi-error idiom to mirror** (config.go:265-280) — `ValidateProfile` aggregates `[]Violation` the same way `Validate` aggregates `missing []string`, so it "lists EVERY unmet requirement" (success criterion #1, never first-fail):
```go
func (c *Config) Validate() error {
	var missing []string
	if strings.TrimSpace(c.DB.URL) == "" {
		missing = append(missing, "POSTGRES_PASSWORD (or AURA_DB_URL)")
	}
	if strings.TrimSpace(c.Neo4j.Password) == "" {
		missing = append(missing, "NEO4J_PASSWORD")
	}
	if len(missing) > 0 {
		return fmt.Errorf("config: required secret(s) unset: %s", strings.Join(missing, ", "))
	}
	if c.RunDirErr != nil {
		return fmt.Errorf("config: %w", c.RunDirErr)
	}
	return nil
}
```

**The pure/total bespoke-gate idiom to mirror** (config.go:292-308) — `GuardWebBind` is the template for EVERY new gate: pure, total (no panic), `config:`-prefixed, **names the offending knob**, reusable directly as the `server_production` non-loopback-auth requirement (D-11). Note it uses `net.SplitHostPort` + `net.ParseIP(...).IsLoopback()` — do NOT regex IPs (RESEARCH §Don't Hand-Roll):
```go
func GuardWebBind(bind string, authConfigured bool, trustProxy bool) error {
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		host = bind // tolerate a bare host with no port
	}
	ip := net.ParseIP(host)
	isLoopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if isLoopback {
		return nil
	}
	if authConfigured || trustProxy {
		return nil
	}
	return fmt.Errorf("config: AURA_AGUI_BIND=%q is non-loopback but web auth is not configured; "+
		"set AURA_AUTHULA_SECRET with AURA_AUTHULA_DATABASE_URL or AURA_DB_URL, set "+
		"AURA_WEB_TRUST_PROXY=true (a reverse proxy terminates auth), or bind a loopback address", bind)
}
```

**F-007 sample-cred reject targets** — the sentinels to compare against live at config.go:36-37 (compare with `==`; they are PUBLIC constants so no constant-time concern — RESEARCH §Security V6):
```go
const (
	defaultObjectStoreAccessKey = "GK000000000000000000000000"
	defaultObjectStoreSecretKey = "0000000000000000000000000000000000000000000000000000000000000000"
)
```
The bespoke `gateObjectStoreCreds(p)` returns `nil` for the lenient tier (dev/local_trusted) and `Fatal` Violations naming `AURA_OBJECTSTORE_ACCESS_KEY` / `AURA_OBJECTSTORE_SECRET_KEY` when `c.ObjectStoreAccessKey == defaultObjectStoreAccessKey` under strict tier (RESEARCH §Code Examples, verbatim shape).

**Gates to implement (each mirrors GuardWebBind; aggregate into one `[]Violation`):** sample object-store creds (F-007), `GARAGE_RPC_SECRET` non-empty under strict (F-007/A5 — grep `scripts/garage_bootstrap.sh` for a literal sample to extend the reject set), replication ≥2 under prod (F-018), `AURA_AGUI_CORS_PERMISSIVE=true` forbidden (prod + hardened per A2), `AURA_SHELL_DESTRUCTIVE_PATTERNS=off` forbidden (prod only per D-11), web-auth required under strict (`AURA_AUTHULA_SECRET`), default bucket/loopback endpoint = WARN under prod (A6), plus reuse `GuardWebBind` + existing `Validate` (DB/Neo4j/RunDir).

---

### `internal/config/config_knobs.go` (NEW — model + validator)

**Analog (mechanics):** `internal/envutil/envutil.go::IntDefault/BoolDefault` (lines 22-47) — the registry re-parse pass calls the SAME `strconv.Atoi` / `strconv.ParseBool` mechanics, but **for diagnostics instead of silent fallback** (D-06). `envutil` stays the dumb leaf, UNCHANGED:
```go
func IntDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback   // ← silent. The NEW pass instead emits a Violation here.
	}
	return n
}
```

**Analog (kind switch):** `cmd/aura/config.go::setConfigKey` (lines 180-218) — the `switch` over typed keys with `strconv.ParseFloat`/`ParseBool` + a "not a valid <kind>" error message is the exact shape the kind-driven `reparsePass` mirrors (lines 188-203):
```go
case "llm.temperature":
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("llm.temperature %q: not a valid float", value)
	}
	...
case "llm.adaptive_reasoning":
	b, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("llm.adaptive_reasoning %q: not a valid bool", value)
	}
```

**Registry shape + re-parse pass** (RESEARCH §Code Examples — planner finalizes field shape per Claude's Discretion). Key behaviors: unset OR whitespace-only → skip (uses default, no violation); severity = `Fatal` when `p.Tier()==strict`, else `Warn` (D-07). Catalogue cut = **Tier A + Tier B only** (D-16); Tier C is a documented follow-on. The full grounded Tier A/B knob list is in RESEARCH §"Knob Registry Catalogue".

**Secret redaction:** `KnobSpec.Secret bool` → mirror `redactedAPIKey` (cmd/aura/config.go:24, `const redactedAPIKey = "REDACTED"`); never print `AURA_AUTHULA_SECRET` / object-store key values (RESEARCH §Security V8, Pitfall 6).

---

### `internal/config/config_runtimeprofile.go` (NEW — model + utility)

**Analog:** `GuardWebBind` (pure/total) for the function discipline + `cmd/aura/config.go::getConfigKey` (lines 153-174) for the total switch-with-default shape.

**Pattern (RESEARCH §Architecture Pattern 3):** `type RuntimeProfile string` with 4 constants + a total `ParseProfile(s string) RuntimeProfile` that defaults unknown/empty → `dev` (D-03), and a `Tier()` helper collapsing `{dev,local_trusted}`→lenient vs `{single_user_hardened,server_production}`→strict (this is the single severity decision feeding `reparsePass`).

**CRITICAL naming-collision avoidance (RESEARCH Pitfall 1 / Anti-Patterns).** These names are ALREADY TAKEN by the unrelated **Agent.md per-identity profile** — the runtime deployment profile MUST use distinct names:
- `internal/profile` package (imported at config.go:24) — do NOT add to it.
- `Config.ProfileDir` / `Config.ProfileCertaintyN` (config.go:199-200, populated config.go:468-469 from `AURA_PROFILE_DIR` / `AURA_PROFILE_CERTAINTY_N`).
- `config_profile_test.go::TestProfileConfigDefaultsAndOverrides` (the file you must NOT shadow).
- **Use:** type `RuntimeProfile`, env `AURA_PROFILE` (no `_DIR`), files `config_runtimeprofile*.go`, test `config_runtimeprofile_test.go`. A `TestProfile*` already compiling = warning sign you shadowed the wrong thing.

---

### `internal/config/config.go` (MODIFIED — root composite)

**Analog:** itself — `loadBase()` field population (lines 349-478) is the precedent for the three additions:
1. **`Config.Profile RuntimeProfile`** field, populated in `loadBase()` via `ParseProfile(os.Getenv("AURA_PROFILE"))` (default dev, D-01/D-03).
2. **`Config.ObjectStoreReplicationFactor int`** from `AURA_OBJECTSTORE_REPLICATION_FACTOR` (default 1) — use `envutil.IntDefault` like its object-store siblings at config.go:417-432. (RESEARCH Open Q1 / A1 — the one net-new read for PROF-06.)
3. **`Config.GarageRPCSecret string`** from `os.Getenv("GARAGE_RPC_SECRET")` (upstream name, CLAUDE.md sidecar exception — like `SEARXNG_URL` at config.go:384). (PROF-03.)
4. **MOVE `Validate()` (lines 265-280) OUT** to `config_validate.go` FIRST (frees ~16 LOC; do this before adding fields or the file-size hook blocks all commits — Pitfall 3).

Existing object-store / AGUI / Authula read block to mirror (config.go:413-444):
```go
AGUIBind:           envDefault("AURA_AGUI_BIND", "127.0.0.1:9080"),
AGUICORSPermissive: envutil.BoolDefault("AURA_AGUI_CORS_PERMISSIVE", false),
...
ObjectStoreBucket:    envDefault("AURA_OBJECTSTORE_BUCKET", "aura-assets"),
ObjectStoreAccessKey: envDefault("AURA_OBJECTSTORE_ACCESS_KEY", defaultObjectStoreAccessKey),
ObjectStoreSecretKey: envDefault("AURA_OBJECTSTORE_SECRET_KEY", defaultObjectStoreSecretKey),
...
AuthulaSecret: os.Getenv("AURA_AUTHULA_SECRET"),
WebTrustProxy: envutil.BoolDefault("AURA_WEB_TRUST_PROXY", false),
```

**Split precedent:** the package already splits by concern (`config_env.go` header at config_env.go:1-8 explicitly cites "refactor-on-touch, CLAUDE.md ≤600 LOC NO GOD CLASS"). Follow it: `config_runtimeprofile.go` + `config_knobs.go` + `config_validate.go`.

---

### `internal/agent/tools/shell_exec_env.go` (MODIFIED — runtime leaf, D-12)

**Analog:** itself — `destructiveShellPatterns()` (lines 108-131). The EXACT one-spot diff (RESEARCH §Code Examples):
```go
// CURRENT (lines 109-116) — empty disables (the bug):
raw, set := os.LookupEnv(envShellDestructivePatterns)
raw = strings.TrimSpace(raw)
if !set {
	return defaultDestructivePatterns, nil
}
if raw == "" || strings.EqualFold(raw, "off") {   // ← empty falls into disable
	return nil, nil
}

// D-12 (target) — empty → defaults (gate ACTIVE); only explicit "off" disables:
raw, set := os.LookupEnv(envShellDestructivePatterns)
raw = strings.TrimSpace(raw)
if !set || raw == "" {                 // unset OR empty → built-in defaults
	return defaultDestructivePatterns, nil
}
if strings.EqualFold(raw, "off") {     // ONLY explicit "off" disables
	return nil, nil
}
// ... comma-split custom parse UNCHANGED (lines 117-130)
```
Also update the doc comments at shell_exec_env.go:15-18 (const block) and 102-107 (func doc) — they currently say "the empty string or 'off' disables the gate" (lines 17, 88, 107); change to "empty = use defaults; only `off` disables." Leaf stays **profile-agnostic** (D-12 / Anti-Patterns): the prod "forbid off" check lives in `config_validate.go`, which reads the raw env value — it does NOT branch `destructiveShellPatterns()` on profile.

---

### `cmd/aura/config_validate.go` (NEW) + `cmd/aura/config.go` (MODIFIED — dispatcher)

**Analog:** `cmd/aura/config.go::runConfig` switch (lines 26-42) — `validate` slots in identically; bad usage → `configUsage()` (stderr) + `os.Exit(1)` (lines 44-47):
```go
func runConfig(args []string) {
	if len(args) < 1 {
		configUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "show":
		configShow()
	case "get":
		configGet(args[1:])
	case "set":
		configSet(args[1:])
	// ADD: case "validate": configValidate(args[1:])
	default:
		configUsage()
		os.Exit(1)
	}
}
```

**Load→render→exit analog:** `configShow` (lines 53-72) — load tolerant config, print, `os.Exit(1)` on error. `configValidate` mirrors this: `flag.NewFlagSet` for `--profile` (overrides `AURA_PROFILE`, D-02) + `--json`, call `config.LoadServe()`, run `ValidateProfile`, render human table (default) or `json.NewEncoder(os.Stdout).Encode(violations)` (RESEARCH recommends the ~15 LOC `--json`), `os.Exit(1)` if ANY `Fatal` (fail-closed, RESEARCH §Security). Redact secret knobs in output via the `redactedAPIKey` convention (config.go:24).

---

## Shared Patterns

### Multi-error aggregation ("list every unmet requirement", criterion #1)
**Source:** `internal/config/config.go::Validate` (lines 265-280) — `var missing []string` + append + `strings.Join`.
**Apply to:** `config_validate.go::ValidateProfile` (aggregate `[]Violation`, never first-fail), `config_knobs.go::reparsePass` (append per bad knob).
**Why structured (not `errors.Join`):** the CLI needs per-knob severity + name for table/json rendering — a flat joined error can't give that cleanly (RESEARCH §State of the Art).

### Pure/total gate naming the knob
**Source:** `internal/config/config.go::GuardWebBind` (lines 292-308) + its test `config_webauth_test.go::TestGuardWebBind`.
**Apply to:** every bespoke gate in `config_validate.go`. Each is pure, total, `config:`-prefixed, and the error message **contains the env-var name** — that `strings.Contains(msg, "AURA_...")` assertion is how every test proves criterion #1.

### strconv re-parse mechanics
**Source:** `internal/envutil/envutil.go` (lines 22-47) — `strconv.Atoi` / `strconv.ParseBool` with `os.Getenv`.
**Apply to:** `config_knobs.go::reparsePass`. Same parse calls, opposite reaction: emit a `Violation` instead of silently returning the fallback (D-06). **Do NOT modify `envutil`** (D-06 / Anti-Patterns).

### Secret redaction in operator-facing output
**Source:** `cmd/aura/config.go:24` (`const redactedAPIKey = "REDACTED"`) + `configShow` lines 59-61.
**Apply to:** `cmd/aura/config_validate.go` render + `KnobSpec.Secret` flag. Never print `AURA_AUTHULA_SECRET` / object-store key values.

### CLI subcommand dispatch + usage/exit
**Source:** `cmd/aura/config.go::runConfig` (26-42) + `configUsage` (44-47).
**Apply to:** the new `validate` case + its flag-parse error path.

---

## Test Patterns

### Table-driven pure-function gate test (the house style)
**Source:** `internal/config/config_webauth_test.go::TestGuardWebBind` (lines 12-60) — anonymous-struct table, `t.Run(tc.name, ...)`, and the load-bearing assertion:
```go
msg := err.Error()
if !strings.Contains(msg, "AURA_AUTHULA_SECRET") {
	t.Errorf("error message must name AURA_AUTHULA_SECRET, got %q", msg)
}
```
**Apply to:** `config_runtimeprofile_test.go::TestParseProfile`, `config_validate_test.go` extensions (`TestValidateProfile`, `TestGateObjectStore`, `TestGateReplication`). Every fail-case asserts `strings.Contains(msg, "<AURA_KNOB>")`.

### Validate() multi-error test
**Source:** `internal/config/config_validate_test.go::TestConfigValidate` (lines 14-36, 36 LOC) — builds a `full()` Config, then mutates one field and asserts the error names that var. EXTEND this file (do not replace) with `TestValidateProfile` etc.

### Env-defaults/override loader test
**Source:** `config_webauth_test.go::TestWebAuthConfigLoad` (lines 65-108) + `config_profile_test.go` (lines 10-37) — `clearPostgresEnv(t)` baseline, then `t.Setenv` + `LoadDB()` assertions including a malformed-fallback case.
**Helper:** `clearPostgresEnv(t)` lives at `internal/config/config_test.go:15` and already clears `AURA_AGUI_*`, `AURA_OBJECTSTORE_*` (config_test.go:38-40). **It does NOT yet clear `AURA_PROFILE` / `AURA_OBJECTSTORE_REPLICATION_FACTOR` / `GARAGE_RPC_SECRET` / `AURA_SHELL_DESTRUCTIVE_PATTERNS`** — add the new knobs to the keys slice so the runtime-profile tests run from a known baseline.

### Property-based test (PBT) — registry re-parse invariants
**Source:** `internal/mcp/manager/envedit_property_test.go` (lines 50-85) — the in-tree `pgregory.net/rapid` template:
```go
func TestSetServerEnvPreservesAllSecrets(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		key := genSecretKey(t)                 // rapid.SampledFrom + rapid.StringMatching
		stored := genSecretValue(t, key)
		// ... exercise, then assert the invariant holds for EVERY draw
	})
}
```
**Apply to:** `config_knobs_test.go` — encode the three invariants from RESEARCH §"Property-Based Testing target": strictness (strict⇒Fatal, lenient⇒at-most-Warn over `rapid.SampledFrom(profiles)` × knob × garbage), no-false-positive (valid value ⇒ no violation), aggregation-monotonicity (a 2nd bad knob never removes the 1st). `rapid` is a **direct dep** (go.mod:40) — no install. Other examples if needed: `internal/swarm/swarm_property_test.go`, `internal/agent/workflow/loop_property_test.go`.

### Destructive-shell truth table
**Source / target:** `internal/agent/tools/shell_exec_destructive_default_test.go` (85 LOC) — already has `TestDestructiveShellDefaultOnFlagsRmRf`, `TestDestructiveShellDefaultPatternsCoverConservativeSet`, `TestDestructiveShellDefaultIsOverridable` (the last sets `"off"`+custom). **EXTEND this file** (NOT the non-existent `shell_exec_env_test.go`) with the D-12 cases: unset→defaults (exists), **`""` empty→defaults (the fix, NEW)**, `"  "` whitespace→defaults, `off`/`OFF`/`Off`→nil, custom→compiled, copied-`.env.example`(commented)→defaults. No existing test breaks — none currently asserts empty=disabled.

---

## No Analog Found

| File | Role | Data Flow | Reason / Mitigation |
|------|------|-----------|---------------------|
| `cmd/aura/config_validate_test.go` (the `os.Exit(1)` e2e assertion) | test (CLI exit-code) | request-response | No existing `cmd/aura` test asserts a non-zero exit / captures stdout for a subcommand. Mitigation: factor the logic into a testable `ValidateProfile` in `internal/config` (asserted with the `config_validate_test.go` table style) and keep `cmd/aura/config_validate.go` a thin presenter; assert exit-code via a small `func() int` core that `main` passes to `os.Exit`, OR a subprocess `os.Args`-driven test. RESEARCH §Test Map marks this Wave 0 e2e. |

Everything else maps to a strong in-tree analog — no file needs to fall back to RESEARCH-only patterns.

---

## Metadata

**Analog search scope:** `internal/config/`, `internal/envutil/`, `internal/agent/tools/`, `cmd/aura/`, plus a tree-wide `pgregory.net/rapid` scan (13 files; `internal/mcp/manager/envedit_property_test.go` chosen as the cleanest env-parse PBT analog).
**Files scanned (read in full):** config.go, config_env.go, config_validate_test.go, config_webauth_test.go, config_profile_test.go, config_test.go (head), shell_exec_env.go, shell_exec_destructive_default_test.go, cmd/aura/config.go, envutil.go, envedit_property_test.go, .env.example (destructive block).
**Key line refs:** `Validate` config.go:265-280 · `GuardWebBind` config.go:292-308 · sentinels config.go:36-37 · object-store reads config.go:417-432 · Authula reads config.go:440-444 · `loadBase` return config.go:349-478 · `IntDefault/BoolDefault` envutil.go:22-47 · `destructiveShellPatterns` shell_exec_env.go:108-131 · `defaultDestructivePatterns` shell_exec_env.go:89-100 · `runConfig` cmd/aura/config.go:26-42 · `redactedAPIKey` cmd/aura/config.go:24 · `setConfigKey` cmd/aura/config.go:180-218 · `TestGuardWebBind` config_webauth_test.go:12-60 · `clearPostgresEnv` config_test.go:15 · rapid template envedit_property_test.go:50-85 · `.env.example` destructive lines 57-61.
**Pattern extraction date:** 2026-06-30
