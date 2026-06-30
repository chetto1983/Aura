---
phase: 33-runtime-profiles-config-validation
plan: 02
subsystem: agent/tools
tags: [prof-02, d-12, f-002, destructive-shell, secure-by-default, truth-table, tdd, env-example]

# Dependency graph
requires: []
provides:
  - "internal/agent/tools/shell_exec_env.go — destructiveShellPatterns() with D-12 empty→defaults semantics: UNSET or EMPTY (whitespace TrimSpace-collapses) returns defaultDestructivePatterns (gate ACTIVE); only case-insensitive \"off\" disables. Closes F-002 (copying .env.example no longer silently disables the advisory destructive-command gate)."
  - "internal/agent/tools/shell_exec_destructive_default_test.go — TestDestructiveShellPatterns: the full unset/empty/whitespace/off/OFF/Off/custom-replaces-default/custom-matches-own truth table (PROF-02 / criterion #2)."
  - ".env.example — corrected destructive-gate comment (empty keeps defaults) + two new commented operator knobs (#AURA_PROFILE=dev, #AURA_OBJECTSTORE_REPLICATION_FACTOR=1)."
affects: [agent/tools, shell_exec, config, garage]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Secure-by-default env leaf (ASVS V14): the advisory gate's disable path is a single explicit sentinel (\"off\"), so the absence of configuration — unset OR the documented-but-empty sample line — resolves to the SAFE state (defaults active), never the open state. The TrimSpace happens before the branch so whitespace-only collapses to empty → defaults."
    - "Profile-agnostic runtime leaf (D-12): destructiveShellPatterns() signature is unchanged (no profile param); the production 'forbid off' policy is enforced one level up in the config validator (plan 04), which reads the raw env value. The leaf only decides active-set-vs-off, not policy."
    - "Truth-table test over (envValue, envSet, probe, wantMatch): one table covers the absence cases (unset/empty/whitespace → ACTIVE), the explicit opt-out (off/OFF/Off → disabled), and the replace-not-merge custom cases (custom matches its own pattern; the default rm -rf / is no longer matched)."

key-files:
  created:
    - .planning/phases/33-runtime-profiles-config-validation/33-02-SUMMARY.md
  modified:
    - internal/agent/tools/shell_exec_destructive_default_test.go
    - internal/agent/tools/shell_exec_env.go
    - .env.example
  deleted: []

key-decisions:
  - "TDD ordering observed live (WSL): the truth table was committed FIRST (test(33-02) 8c15daf9) and run before the fix — the empty + whitespace rows were RED (destructiveShellMatch(\"rm -rf /\") = false while the gate should be active), the other six rows green and the three pre-existing tests green. The flip (feat(33-02) bb89183b) turned the two RED rows GREEN. Unlike plan 01's TDD tasks, the RED here is a value-mismatch (the package compiles), so it was committable through the vet hook as a real failing test — committed as test(...) then feat(...), the canonical RED→GREEN gate sequence."
  - "Leaf stays profile-agnostic per the plan prohibition (D-12): NO profile parameter or branch was added to destructiveShellPatterns(); the server_production 'forbid explicit off' check is plan 04's gateDestructiveShell in config_validate.go (reads the raw env value), cross-referenced in the updated func doc but out of this plan's file scope."
  - "The three pre-existing destructive tests (TestDestructiveShellDefaultOnFlagsRmRf / DefaultPatternsCoverConservativeSet / DefaultIsOverridable) were left byte-for-byte untouched — none asserts empty=disabled, so none breaks under D-12; the new behavior is proved only by the new table (plan + prompt prohibition honored)."
  - "This plan owns the phase's .env.example edits, so the two operator-facing knobs introduced by sibling plans (AURA_PROFILE in plans 01/04, AURA_OBJECTSTORE_REPLICATION_FACTOR in plans 01/04) are documented here as COMMENTED lines — discoverability per the established 'every AURA_* knob appears in .env.example' pattern, without activating an override."

patterns-established:
  - "Absence-resolves-to-safe sentinel gate: unset/empty → defaults active, single explicit token to opt out. Reusable for any advisory env guardrail."

requirements-completed: []  # PROF-02 implementation landed here; the phase-level requirement flip is owned by the verifier/orchestrator at phase close (the destructive truth table is one of PROF-02's acceptance rows in 33-VALIDATION.md).

# Coverage metadata
coverage:
  - id: D1
    description: "UNSET or EMPTY (incl. whitespace-only) AURA_SHELL_DESTRUCTIVE_PATTERNS → built-in defaults (gate ACTIVE); copying the commented .env.example sample keeps the gate on (success criterion #2)."
    requirement: "PROF-02"
    verification:
      - kind: unit
        ref: "internal/agent/tools/shell_exec_destructive_default_test.go#TestDestructiveShellPatterns/{unset_copied_sample,empty,whitespace} (go test -race ./internal/agent/tools/)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Only an explicit, case-insensitive \"off\" disables the gate; any other non-empty value REPLACES the defaults (no merge) — the default rm -rf / is no longer matched by an unrelated custom pattern, and the custom pattern matches its own target."
    requirement: "PROF-02"
    verification:
      - kind: unit
        ref: "internal/agent/tools/shell_exec_destructive_default_test.go#TestDestructiveShellPatterns/{off_lower,off_upper,off_mixed,custom_replaces_default,custom_matches_own} (go test -race ./internal/agent/tools/)"
        status: pass
    human_judgment: false
  - id: D3
    description: "destructiveShellPatterns() stays profile-agnostic (signature unchanged, no profile param); doc comments + .env.example corrected to 'empty = defaults; only off disables'; the two new knobs documented (commented) in .env.example."
    requirement: "PROF-02"
    verification:
      - kind: unit
        ref: "grep: func destructiveShellPatterns()=1, 'if !set || raw == \"\"'=1, EqualFold(raw,\"off\")=1, 'raw == \"\" ||'=0; .env.example AURA_PROFILE==1, AURA_OBJECTSTORE_REPLICATION_FACTOR==1"
        status: pass
    human_judgment: false

# Execution metrics
metrics:
  duration: ~15min
  completed: 2026-06-30
  tasks: 2
  files: 3
---

# Phase 33 Plan 02: Destructive-shell empty→defaults semantics flip (D-12 / F-002) Summary

Closed the silent-disable footgun in the advisory destructive-command approval gate: an empty `AURA_SHELL_DESTRUCTIVE_PATTERNS` (the state produced by copying the commented `.env.example` sample line) used to DISABLE the gate. Now **unset OR empty → built-in defaults (gate ACTIVE)** and **only an explicit, case-insensitive `off` disables**. The change is a one-spot branch flip in the runtime leaf, plus refreshed doc comments, a corrected `.env.example` comment, and a complete RED→GREEN truth-table test. The leaf stays profile-agnostic — the production "forbid `off`" policy lives in the config validator (plan 04), not here.

## What was built

**Task 1 — D-12 destructive-shell truth table (`test`, commit `8c15daf9`, TDD RED).**
Appended `TestDestructiveShellPatterns` to `shell_exec_destructive_default_test.go`: a table over `(value string, set bool, probe string, wantMatch bool)` driving `destructiveShellMatch`. Rows: `unset_copied_sample` (set=false → ACTIVE), `empty` (set=true,"" → ACTIVE), `whitespace` ("   " → ACTIVE), `off_lower`/`off_upper`/`off_mixed` (→ disabled, case-insensitive), `custom_matches_own` (`rm -rf /tmp/x,mkfs` matches `rm -rf /tmp/x`), `custom_replaces_default` (same custom set does NOT match the default `rm -rf /`). Uses `t.Setenv` for set-values and `os.Unsetenv` for the unset row (mirrors the existing tests). Observed RED via WSL on `empty` + `whitespace` (`destructiveShellMatch("rm -rf /") = false, want active=true`), the other six rows green; the three pre-existing tests stayed green. The three pre-existing destructive tests were not modified.

**Task 2 — flip `destructiveShellPatterns()` + comments + .env.example (`feat`, commit `bb89183b`, GREEN).**
Changed the branch from `if !set { return defaults }` / `if raw == "" || EqualFold(raw,"off") { return nil }` to `if !set || raw == "" { return defaultDestructivePatterns, nil }` followed by a separate `if strings.EqualFold(raw, "off") { return nil, nil }`. `raw` is already `strings.TrimSpace`-d, so whitespace-only collapses to empty → defaults. The comma-split custom-parse block was left unchanged. Refreshed the three stale doc comments (const block at the env name, `defaultDestructivePatterns` doc, and the func doc) to "empty = use defaults; only `off` disables", and added a one-line note in the func doc that the prod forbid-`off` check lives in the config validator (deep-refactor-on-touch). In `.env.example`: corrected the destructive-gate comment to state UNSET or EMPTY keeps defaults active and only `off` disables; added a commented `#AURA_PROFILE=dev` (listing the four valid profiles, unset→dev) in the runtime block and a commented `#AURA_OBJECTSTORE_REPLICATION_FACTOR=1` (default 1, prod needs ≥2 per PROF-06) beside `GARAGE_RPC_SECRET`. No profile parameter added — signature unchanged.

## Test results (WSL, CGO_ENABLED=1)

- RED (post-Task-1): `go test ./internal/agent/tools/ -run TestDestructiveShellPatterns -v` → `FAIL` with `--- FAIL: .../empty` and `.../whitespace`, all six other rows PASS; the three pre-existing tests (`-run 'TestDestructiveShellDefaultOnFlagsRmRf|...DefaultPatternsCoverConservativeSet|...DefaultIsOverridable'`) → `ok`.
- GREEN (post-Task-2): `CGO_ENABLED=1 go test ./internal/agent/tools/ -run TestDestructiveShell -race -count=1 -v` → PASS — all four `TestDestructiveShell*` functions, all eight `TestDestructiveShellPatterns` subtests including the now-flipped `empty` + `whitespace`.
- Native `go vet ./internal/agent/tools/` → clean; `go build ./...` → green; pre-commit hooks (gofmt / vet / file-size) green on both commits.

TDD note: unlike plan 01's compile-failure REDs, this RED is a value-mismatch in a compiling package, so it was a genuine failing test committable through the `vet` hook — committed as `test(...)` (RED gate) then `feat(...)` (GREEN gate), the canonical sequence.

## Acceptance criteria — verified

| Criterion | Result |
|-----------|--------|
| `grep -c 'func TestDestructiveShellPatterns' ..._test.go` == 1 | 1 |
| Table includes empty + whitespace + OFF/Off case-insensitive + custom + unset(copied-sample) rows | yes (8 rows) |
| Three pre-existing destructive tests untouched + green | yes (git-verified, `ok`) |
| `grep -c 'if !set \|\| raw == ""' shell_exec_env.go` == 1 | 1 |
| `grep -c 'strings.EqualFold(raw, "off")'` == 1 and no `raw == "" \|\|` precedes it | 1 / 0 |
| `grep -c 'func destructiveShellPatterns()'` == 1 (no profile param) | 1 |
| All `TestDestructiveShell*` pass under `-race` | yes |
| `.env.example` destructive prose: empty/unset keeps defaults, only `off` disables | yes |
| `.env.example` new knobs: `AURA_PROFILE=` ≥1 and `AURA_OBJECTSTORE_REPLICATION_FACTOR=` == 1 | 1 / 1 |

## Deviations from Plan

None — plan executed exactly as written. Prohibitions honored: `destructiveShellPatterns()` was NOT made profile-aware (signature unchanged, no profile branch); the prod forbid-`off` check was left to plan 04; the three pre-existing destructive tests were not modified. No architectural (Rule 4) changes, no auth gates, no package installs (pure-Go, no new deps).

## Known Stubs

None. The flip is fully wired and exercised by the passing truth table. The two `.env.example` knobs are intentionally COMMENTED documentation for knobs whose validation gates land in plans 04–05 (documented phase sequencing, not a stub) — they do not gate any behavior in this plan.

## Threat surface scan

No new security-relevant surface beyond the plan's `<threat_model>`. T-33-02a (empty value → defaults) is the mitigation this plan delivers; T-33-02b (prod forbid-`off`) is cross-referenced and deferred to plan 04 by design. No new endpoints, auth paths, file access, or schema changes.

## Self-Check: PASSED
- FOUND: internal/agent/tools/shell_exec_env.go
- FOUND: internal/agent/tools/shell_exec_destructive_default_test.go
- FOUND: .env.example
- FOUND commit 8c15daf9 (test: D-12 truth table, RED)
- FOUND commit bb89183b (feat: empty→defaults flip, GREEN)
