---
phase: 33-runtime-profiles-config-validation
verified: 2026-07-01T00:00:00Z
status: passed
score: 12/12 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 11/12
  gaps_closed:
    - "golangci-lint exits 0 across the whole tree (errcheck on cmd/aura/config_validate.go renderViolations — 3 unchecked fmt.Fprintf return values)"
  gaps_remaining: []
  regressions: []
---

# Phase 33: Runtime Profiles + Config Validation Verification Report

**Phase Goal:** 4 validated runtime profiles (dev / local_trusted / single_user_hardened / server_production) in `internal/config`; production fails fast on unsafe defaults; all hot-path `AURA_*` knobs catalogued.
**Verified:** 2026-07-01
**Status:** passed
**Re-verification:** Yes — after gap closure (commit `3d50e54f`) + docs flip (commit `c9ac241e`)

## What changed since the prior verification

The prior verification (`status: gaps_found`, 11/12) found exactly one blocking gap: 3
`errcheck` findings in `cmd/aura/config_validate.go`'s `renderViolations` (unchecked
`fmt.Fprintf` return values), the only lint issues anywhere in the repo, which would have
failed CI's `golangci-lint` job. Independently re-running `golangci-lint run ./...` in WSL
now reports **0 issues** — the three calls are explicitly discarded with `_, _ =`, matching
the established `cmd/aura/config.go` `//nolint`-adjacent convention. **The prior blocking
lint gap is resolved**, confirmed by direct re-execution, not by trusting the commit
message.

The same gap-closure commit (`3d50e54f`) also folded in three `33-REVIEW.md` findings,
independently re-verified below (not just read — diffed against the leaf it claims to
mirror, and re-run under `-race`):

- **CR-01 (Critical/Blocker in review)** — an explicit `--profile <typo>` previously ran
  through the total `ParseProfile`, silently coerced to `dev`, skipped every strict gate,
  and exited 0 (false-green CI gate). Now `runConfigValidate` round-trips
  `config.ParseProfile(*profileFlag)` against `strings.TrimSpace(*profileFlag)`; any
  explicit value that doesn't equal one of the 4 known profile strings is rejected with
  exit 2 and a usage message to stderr — confirmed by reading
  `cmd/aura/config_validate.go:53-69` and by an independent `-race` run of the new
  subtest `explicit unknown --profile is rejected, never coerced to dev (CR-01)` (PASS).
  The implicit `AURA_PROFILE` env path is untouched — `ParseProfile` itself stays total,
  preserving documented D-03 boot-side behavior.
- **WR-01 (Warning)** — `reparsePass` trimmed before parsing while `envutil.IntDefault`/
  `BoolDefault` parse the raw, untrimmed value (confirmed by reading
  `internal/envutil/envutil.go:22-47` directly — `strconv.Atoi(v)`/`ParseBool(v)` on the
  raw `os.Getenv` result, no `TrimSpace` anywhere in either leaf). `config_knobs.go`'s
  `reparsePass` now parses `raw` (not `strings.TrimSpace(raw)`) for `KindInt`/`KindBool`,
  while `KindEnum` stays trimmed (mirrors `ParseProfile`, which does trim). Independently
  re-run subtest `padded int ' 128' ⇒ Fatal (raw parse mirrors envutil, WR-01)` — PASS
  under `-race`.
- **WR-03 (Warning)** — `--json` emitted `null` for a clean config (`ValidateProfile`
  returns a nil slice when there are no violations; `json.Encoder` serializes nil as
  `null`), breaking `jq '.[]'`-style CI consumers. `runConfigValidate` now normalizes
  `violations` to `[]config.Violation{}` before encoding. Independently re-run subtest
  `--json emits [] not null for a clean config (WR-03)` — PASS, asserts the raw byte
  output is the literal string `[]` AND that it round-trips through `json.Unmarshal`.

**WR-04** (resource leak on `CommandHookManagerFromEnv`'s error path in `chat.go`) is
correctly left untouched by this gap-closure commit (confirmed: `3d50e54f` touches only
`cmd/aura/config_validate.go`, `cmd/aura/config_validate_test.go`,
`internal/config/config_knobs.go`, `internal/config/config_knobs_test.go` — `chat.go` is
not in the diff). REQUIREMENTS.md routes `QUAL-04`'s pool-leak sub-item to Phase 34
(`LOOP-01..11 (+QUAL-04 pool-leak/int32) → Phase 34`); WR-04 is the same boot-path-leak
cluster, pre-existing from commit `3b10f2c7` per the review's own note, not introduced by
Phase 33. This is correctly treated as deferred, not a Phase 33 gap.

**WR-02** (typo'd `AURA_PROFILE` env var fail-open to dev) and **WR-05** (unvalidated
custom destructive-shell pattern set) remain open. Both are classified `Warning` (not
`Critical`) in `33-REVIEW.md`, and WR-02 is explicitly noted by the reviewer as "the
documented D-03 totality ... a deliberate tradeoff" — not a defect requiring a fix to
close the phase. Neither blocks any of the 4 roadmap success criteria. Flagged below as
non-blocking informational carry-overs, not gaps.

## Goal Achievement

### Observable Truths

All 12 truths from the prior verification were re-checked. Truths #1-11 are unchanged
(no source files they depend on were touched by the gap-closure commits, confirmed by
`git show --stat`) — quick regression check only (existence + a representative live
test re-run). Truth #12 (lint) and the 3 review fixes get full re-verification.

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | `aura config validate --profile server_production` exits non-zero listing every unmet requirement, each line naming its `AURA_*` knob (Roadmap SC#1 / PROF-01) | VERIFIED | `TestConfigValidate_ServerProduction/lists_every_unmet_knob_and_exits_1` re-run green (WSL, `-race`): exit 1, names all 7 offending knobs. |
| 2 | Copying `.env.example`→`.env` keeps the destructive-shell gate ACTIVE (Roadmap SC#2 / PROF-02 / D-12) | VERIFIED | `internal/agent/tools/shell_exec_env.go` unchanged since prior verification (last touched by `bb89183b`, before gap-closure). `TestDestructiveShellPatterns` (8 subtests) re-run green. REQUIREMENTS.md PROF-02 checkbox now `[x]` (commit `c9ac241e`, confirmed via direct grep). |
| 3 | Invalid env fails-fast under production (Fatal), warns under dev (Warn, never Fatal) (Roadmap SC#3 / PROF-04 / D-07) | VERIFIED | `TestReparsePass` (now 14 rows incl. the new WR-01 padded-int case) + `TestRapidEnvStrictness` (PBT) re-run green under `-race`. |
| 4 | dev/local_trusted preserve today's full-host behavior unchanged (Roadmap SC#4 / D-09/D-14) | VERIFIED | `cmd/aura/chat.go` not in either gap-closure commit's diff — boot path unchanged. `TestConfigValidate`'s `full()` fixture re-run green. |
| 5 | `AURA_PROFILE` unset resolves to `dev`; total parser never panics on garbage (D-01/D-03) | VERIFIED | `internal/config/config_runtimeprofile.go` unchanged. `TestParseProfile` re-run green. |
| 6 | `Config` exposes runtime-readable `Profile`, `ObjectStoreReplicationFactor`, `GarageRPCSecret` populated at load | VERIFIED | `internal/config/config.go` unchanged. `TestRuntimeProfileFieldsLoad` re-run green. |
| 7 | Every hot-path `AURA_*` knob has exactly one registry row (QUAL-04 / D-08) | VERIFIED | `knobRegistry()` still 43 rows (no rows added/removed by gap closure, only `reparsePass`'s internal parse logic changed). `TestKnobRegistry` re-run green. |
| 8 | `ValidateProfile` aggregates EVERY unmet requirement, never first-fail | VERIFIED | Unchanged (`config_validate.go` itself not modified by gap closure). `TestValidateProfile` re-run green. |
| 9 | `AURA_RUN_DIR` non-absolute is a Fatal violation across ALL tiers (PROF-05/F-041) | VERIFIED | Unchanged. `TestRunDirProfileValidation` re-run green. |
| 10 | Hardened↔prod differentiator (D-15) | VERIFIED | Unchanged. `TestGateCORS`, `TestGateDestructiveShell`, `TestGateReplication` re-run green. |
| 11 | `--profile` overrides `AURA_PROFILE` for the validation run (D-02), AND an unrecognized explicit `--profile` is rejected (exit 2), not silently coerced to `dev` (CR-01 fix) | VERIFIED | `TestConfigValidate_ServerProduction/benign_--profile_dev_exits_0` (known-profile override) AND the new `.../explicit_unknown_--profile_is_rejected,_never_coerced_to_dev_(CR-01)` subtest both re-run green: exit 2, stderr usage message, `out` buffer empty (no report rendered for a rejected profile). |
| 12 | `golangci-lint` exits 0 across the whole tree (CLAUDE.md mandatory phase-end gate) | **VERIFIED** | Independently re-ran `golangci-lint run ./...` in WSL: **`0 issues.`**, exit code 0. The 3 `errcheck` findings from the prior verification are gone — `renderViolations`'s three `fmt.Fprintf(out, ...)` calls now explicitly discard their return with `_, _ =`. |

**Score:** 12/12 truths verified (0 gaps). Prior gap (#12, lint) closed and independently confirmed.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/config/config_runtimeprofile.go` | RuntimeProfile enum + ParseProfile + Strict() | VERIFIED | Unchanged since prior verification; 58 LOC. |
| `internal/config/config_validate.go` | Violation/Severity contract + ValidateProfile aggregator + bespoke gates | VERIFIED | Unchanged since prior verification; 257 LOC (<600). |
| `internal/config/config.go` | Profile/ObjectStoreReplicationFactor/GarageRPCSecret fields | VERIFIED | Unchanged; 550 LOC (<600). |
| `internal/config/config_knobs.go` | KnobSpec registry + reparsePass (now raw-value parse for Int/Bool) | VERIFIED | 165 LOC (<600, was 159 — WR-01 fix added comments/logic). `reparsePass` parses raw for KindInt/KindBool, trimmed for KindEnum. |
| `internal/agent/tools/shell_exec_env.go` | destructiveShellPatterns() empty→defaults | VERIFIED | Unchanged; 154 LOC. |
| `cmd/aura/config_validate.go` | configValidate CLI + --profile/--json + thin presenter, lint-clean, CR-01/WR-03 fixed | VERIFIED | 137 LOC (<600, was 119). `runConfigValidate` rejects unknown explicit `--profile` (exit 2), normalizes nil violations to `[]config.Violation{}` before JSON encode, `renderViolations`'s 3 `fmt.Fprintf` calls discard return values (`_, _ =`). Zero golangci-lint issues. |
| `cmd/aura/config.go` | `case "validate"` dispatch + configUsage doc | VERIFIED | Unchanged. |
| Test files (incl. new CR-01/WR-01/WR-03 regression subtests) | Cover the 3 review fixes | VERIFIED | `cmd/aura/config_validate_test.go` gained `setBenignDevEnv` helper + 2 new subtests (CR-01, WR-03); `internal/config/config_knobs_test.go` gained 1 new row (WR-01). All pass under `-race -count=1` (independently re-run). |

### Key Link Verification

| From | To | Via | Status | Details |
|------|-----|-----|--------|---------|
| `cmd/aura/config_validate.go runConfigValidate` | `config.ParseProfile` | explicit `--profile` round-trip check | WIRED (now fail-closed) | `string(parsed) != strings.TrimSpace(*profileFlag)` rejects any non-canonical value with exit 2; verified via CR-01 subtest. |
| `cmd/aura/config_validate.go runConfigValidate` | `json.Encoder.Encode` | nil→`[]` normalization before encode | WIRED | `if violations == nil { violations = []config.Violation{} }`; verified via WR-03 subtest (raw byte equality to `"[]"`). |
| `internal/config/config_knobs.go reparsePass` | `internal/envutil.IntDefault/BoolDefault` | raw (untrimmed) value parse mirrors the leaf | WIRED | Read `envutil.go:22-47` directly — confirmed no `TrimSpace` in either leaf function; `reparsePass` now matches. |
| `cmd/aura/config_validate.go renderViolations` | `io.Writer` | explicitly-discarded `fmt.Fprintf` return values | WIRED (lint-clean) | `_, _ = fmt.Fprintf(out, ...)` at all 3 call sites; `golangci-lint run ./...` confirms 0 errcheck findings. |
| All prior key links (chat.go boot path, ValidateProfile→gates, config.go→loadBase, etc.) | — | — | WIRED (unchanged) | Source files not touched by either gap-closure commit; prior verification's evidence stands. |

### Behavioral Spot-Checks (independently re-run, not taken from commit messages)

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Whole-tree build | `go build ./...` (native Windows) | Exit 0 | PASS |
| `go vet ./...` | native Windows | Exit 0, clean | PASS |
| `golangci-lint run ./...` | WSL | `0 issues.`, exit 0 | **PASS (was FAIL — prior blocker)** |
| `go test ./internal/config/ ./cmd/aura/ -race -count=1` | WSL | both `ok`, full suite green | PASS |
| `TestConfigValidate_ServerProduction` (6 subtests incl. CR-01 + WR-03) | WSL, `-race -v` | All 6 PASS | PASS |
| `TestReparsePass` (14 rows incl. WR-01 padded-int) + `TestRapidEnvStrictness`/`NoFalsePositive`/`AggregationMonotonic` (PBT) | WSL, `-race -v` | All PASS | PASS |
| `govulncheck ./internal/config/... ./cmd/aura/...` | WSL | 0 reachable vulnerabilities | PASS |

### Requirements Coverage

| Requirement | Status | Evidence |
|---|---|---|
| PROF-01 | SATISFIED | Truths #1, #8, #11 (incl. CR-01 fail-closed fix). REQUIREMENTS.md `[x]`. |
| PROF-02 | SATISFIED | Truth #2. REQUIREMENTS.md `[x]` (flipped by commit `c9ac241e`, confirmed via direct grep of REQUIREMENTS.md). |
| PROF-03 | SATISFIED | Unchanged; REQUIREMENTS.md `[x]`. |
| PROF-04 | SATISFIED | Truth #3 (incl. WR-01 raw-parse fix). REQUIREMENTS.md `[x]`. |
| PROF-05 | SATISFIED | Truth #9. REQUIREMENTS.md `[x]`. |
| PROF-06 | SATISFIED | Unchanged; REQUIREMENTS.md `[x]`. |
| QUAL-04 (env-catalog slice) | SATISFIED | Truth #7. Pool-leak/int32 sub-items correctly routed to Phase 34 per REQUIREMENTS.md's phase-mapping table. |

No orphaned requirements. No checkbox/implementation mismatches remaining.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| — | — | No TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER in any phase-modified file | — | None |
| — | — | Prior `errcheck` blocker (`cmd/aura/config_validate.go:85,96,98`) | — | **Resolved** — `golangci-lint run ./...` confirms 0 issues |

All touched files remain within the 600-LOC CLAUDE.md cap: `config_validate.go` (cmd/aura)
137 LOC, `config_knobs.go` 165 LOC — both well under the cap.

### Non-blocking carry-over findings (informational, not phase-33 gaps)

These are documented in `33-REVIEW.md`, classified `Warning` (not `Critical`/blocker), and
do not affect any of the 4 roadmap success criteria or PROF-01..06/QUAL-04 requirements:

- **WR-02** — a typo'd `AURA_PROFILE` env var still fail-opens to `dev` at boot (the
  documented D-03 totality; reviewer explicitly calls this "a deliberate tradeoff").
- **WR-04** — pre-existing (commit `3b10f2c7`, before Phase 33) resource leak on
  `CommandHookManagerFromEnv`'s error path in `chat.go`. REQUIREMENTS.md routes the
  related `QUAL-04` pool-leak sub-item to Phase 34; this is the same cluster.
- **WR-05** — a malformed custom `AURA_SHELL_DESTRUCTIVE_PATTERNS` regex set passes
  `config validate` but breaks the shell tool at runtime (gate doesn't compile-check the
  custom pattern list).
- IN-01..05 — JSON schema field-name/severity-encoding polish, magic-string dedup,
  `os.Setenv` vs `t.Setenv` in property tests, `isLoopbackEndpoint` substring heuristic.

None require closure before this phase can be considered complete; recommend tracking in
Phase 34/35 scope if not already covered by the existing F-series mapping.

### Human Verification Required

None. This phase is entirely backend config/CLI logic; every observable truth was
independently verified programmatically (re-running the full `-race` test suite,
`go vet`, `go build`, `golangci-lint`, and `govulncheck` directly — not trusting
SUMMARY.md or commit-message claims).

### Gaps Summary

None. The single blocking gap from the prior verification (golangci-lint errcheck ×3 in
`cmd/aura/config_validate.go`) is resolved and independently confirmed via a direct
`golangci-lint run ./...` re-run in WSL (`0 issues.`). The three code-review fixes folded
into the same gap-closure commit (CR-01 fail-open `--profile` typo, WR-01 validator/leaf
parse-fidelity mismatch, WR-03 `null`-vs-`[]` JSON wart) were each independently
re-verified by reading the diff against its stated rationale (cross-checked
`envutil.IntDefault`/`BoolDefault` source for WR-01) and by re-running their dedicated
regression subtests under `-race`, all green. The REQUIREMENTS.md PROF-02 checkbox flip
(commit `c9ac241e`) was confirmed directly against the file. No regressions were found in
any of the 11 previously-verified truths — the gap-closure commit's diff touches only 4
files (`cmd/aura/config_validate.go`, `cmd/aura/config_validate_test.go`,
`internal/config/config_knobs.go`, `internal/config/config_knobs_test.go`), none of which
back any of the other 11 truths' evidence. Phase 33 goal — 4 validated runtime profiles,
production fails fast on unsafe defaults, hot-path `AURA_*` knobs catalogued — is achieved
and CI-clean.

---

_Verified: 2026-07-01T00:00:00Z_
_Verifier: Claude (gsd-verifier)_
