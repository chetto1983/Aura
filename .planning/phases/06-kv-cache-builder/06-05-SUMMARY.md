---
phase: 06-kv-cache-builder
plan: 05
subsystem: ci-gates
tags: [kv-cache, ci, invariant-gate, no-skip-as-green, amendment-16]
requires:
  - "cmd/aura cache-audit (06-04): the hidden 20-turn replay subcommand the wrapper drives"
provides:
  - "scripts/cache_invariant_audit.sh: runtime-faithful KV-prefix invariant wrapper"
  - "scripts/cache_invariant_negative_test.sh: SC#5 negative proof (gate is not silently green)"
  - "ci.yml cache-invariant job: Postgres-free gate from Phase 6 onward (amendment #16)"
affects:
  - "every subsequent merge: the cache-invariant CI job now gates all PRs against prefix poisoning"
tech-stack:
  added: []
  patterns:
    - "loop_budget_smoke.sh runtime-faithful gate discipline (set -euo pipefail + grep -c . || true loud-fail-on-empty)"
    - "belt-and-suspenders: the Go subcommand asserts AND the bash wrapper independently diffs the 20 hashes"
    - "AURA_CACHE_AUDIT_CMD seam lets the negative test feed a poisoned hash stream without a real prefix bug"
key-files:
  created:
    - scripts/cache_invariant_audit.sh
    - scripts/cache_invariant_negative_test.sh
  modified:
    - .github/workflows/ci.yml
decisions:
  - "Negative proof exercises the bash wrapper's OWN hash diff via an AURA_CACHE_AUDIT_CMD seam (no Go change); the canonical Go SC#5 proof remains cmd/aura/cache_test.go TestCacheAudit_Mutation_Exit1"
  - "Gate added as a dedicated cache-invariant job (mirrors vulncheck/sqlc-golden per-concern job style) rather than a step on unit-test"
  - "CI gates ONLY the byte-identity invariant, NOT a cache-hit percentage (PRD OQ4: hit-rate is provider-dependent + flaky)"
metrics:
  duration: ~25m
  completed: 2026-06-02
  tasks: 2
  files: 3
---

# Phase 6 Plan 05: Cross-slice KV-cache invariant CI gate Summary

Ships the headline Phase 6 deliverable (amendment #16, Pitfall #3 P0): a runtime-faithful, Postgres-free CI gate that fails any future merge mutating the assembled KV-cache prefix, plus the SC#5 negative proof that the gate is not silently green.

## What was built

- **`scripts/cache_invariant_audit.sh`** — a thin wrapper that drives the hidden `aura cache-audit` (06-04, the real 20-turn `runner.Turn` replay against in-memory fakes). It independently counts the `turn NN: <hex>` lines (must be exactly 20), diffs every `messages[0]` hash (belt-and-suspenders over the Go assertion), forwards the subcommand's exit code + stderr verbatim, and on any drift exits 1 with `messages[0] mutated at turn N`. Uses `set -euo pipefail` + the `| grep -c . || true` loud-fail-on-empty guard so an EMPTY/short run is a HARD failure (no-skip-as-green). Portable shell (no `=~`/process substitution).
- **`scripts/cache_invariant_negative_test.sh`** — the SC#5 NEGATIVE proof. Case 1 feeds the wrapper a poisoned 20-line hash stream (turn 03 differs) via the `AURA_CACHE_AUDIT_CMD` seam and asserts the gate exits non-zero with `mutated`; case 2 feeds empty output and asserts the gate still fails (no-skip-as-green). If either passes silently, this script fails loudly — the "gate is silently green" alarm.
- **`.github/workflows/ci.yml`** — a dedicated `cache-invariant` job (ubuntu-latest, no `services`/DB env, `CI: "true"`) running `cache invariant gate` (the wrapper) and `cache invariant gate (negative)` (the SC#5 proof). Gates every merge from Phase 6 onward; does NOT gate on a cache-hit percentage.

## Tasks

| Task | Name | Commit | Files |
| ---- | ---- | ------ | ----- |
| 1 | Wrapper + SC#5 negative proof | 1919d23b | scripts/cache_invariant_audit.sh, scripts/cache_invariant_negative_test.sh |
| 2 | Wire gate into ci.yml (Postgres-free) | 310d19ba | .github/workflows/ci.yml |

## Verification (run locally, all green)

- `bash scripts/cache_invariant_audit.sh` → `ok ... 20 identical messages[0] hashes (d69144...)`, exit 0. `go run ./cmd/aura cache-audit | grep -cE '^turn [0-9]{2}: [0-9a-f]+$'` → `20`.
- `bash scripts/cache_invariant_negative_test.sh` → case 1: mutated `messages[0]` → gate exit 1 with `mutated`; case 2: empty output → gate exit 1 (no-skip-as-green); `PASS (the gate is NOT silently green)`, exit 0.
- ci.yml parses (gopkg.in/yaml.v3 + pyyaml): `jobs` includes `cache-invariant`, `runs-on: ubuntu-latest`, no `services` key, `env.CI=true`, steps `cache invariant gate` + `cache invariant gate (negative)`.
- Both scripts recorded executable in the index (`100755`) for Linux CI.
- pre-commit hooks (vet + file-size cap) green on both commits.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Portable shell rewrite for the wrapper's hash diff**
- **Found during:** Task 1 (local validation under the w64devkit `bash` the harness uses).
- **Issue:** The first wrapper draft used `done < <(...)` process substitution and a `[[ $line =~ ^turn ([0-9]{2}) ... ]]` regex; that `bash` rejected the regex parens with `syntax error: unexpected "("`.
- **Fix:** Replaced process substitution with a here-string (`done <<< "$OUT"`) and the `=~`/`BASH_REMATCH` match with a portable `case "$line" in "turn "[0-9][0-9]": "*)` guard + `${line#*: }` extraction. No behavior change; works on both the harness shell and ubuntu CI bash.
- **Files modified:** scripts/cache_invariant_audit.sh
- **Commit:** 1919d23b

**Note (not a deviation, design choice):** The plan's Task 1 allowed an `--inject-mutation` test hook in 06-04 OR a poisoned fixture OR a test-only env knob, "least invasive that 06-04 supports". 06-04 ships NO such hook and `messages[0]` (the system prompt) is invariant to fixture perturbation, so a poisoned fixture cannot drift `messages[0]` (it would pass or error exit 2). The least-invasive option that touches NO merged 06-04 Go file is the `AURA_CACHE_AUDIT_CMD` seam in the wrapper: the negative test feeds a poisoned hash stream that exercises the wrapper's OWN independent hash diff. The canonical Go-level SC#5 proof remains `cmd/aura/cache_test.go` `TestCacheAudit_Mutation_Exit1` (drives `reportHashes` directly). Both layers of the belt-and-suspenders gate are thus proven.

## Threat surface

T-06-01 (Tampering — prefix poisoning, the phase's blocking high-severity threat) is mitigated at the CI-gate level here and proven by the SC#5 negative test. T-06-03 (Information Disclosure) holds: the gate prints only per-turn SHA-256 hex + the explicit mutation message, no content/secrets. No new threat surface introduced (two shell scripts + one Postgres-free CI job).

## Known Stubs

None.

## Self-Check: PASSED

- FOUND: scripts/cache_invariant_audit.sh (100755)
- FOUND: scripts/cache_invariant_negative_test.sh (100755)
- FOUND: .github/workflows/ci.yml cache-invariant job
- FOUND commit: 1919d23b (feat(06-05): add cache invariant gate scripts + SC#5 negative proof)
- FOUND commit: 310d19ba (feat(06-05): wire cache invariant gate into CI)
