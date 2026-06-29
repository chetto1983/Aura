---
phase: 31-stabilization-ci-unblock
plan: 02
subsystem: infra
tags: [ci, github-actions, go-packages, govulncheck, shell-bash, lint, no-skip-as-green, F-015, SEC-07]

# Dependency graph
requires:
  - phase: 31-stabilization-ci-unblock
    provides: "31-01 attributable GREEN baseline (C1/C2/C4) so any Wave-2 CI regression is attributable"
provides:
  - "All four raw root `./...` Go CI gates (unit-test go test -race, windows-unit go build + go test, vulncheck govulncheck) source their package list from $(bash scripts/go_packages.sh) — owned-surface parity with the Makefile GO_PACKAGES"
  - "scripts/check_ci_go_packages.sh — F-015 lint rejecting standalone-root `./...` in CI workflows (scope via AURA_CI_WORKFLOWS_DIR, default .github/workflows/)"
  - "scripts/check_ci_go_packages_test.sh — negative self-test proving the lint exits non-zero on a planted `go test ./...` (no-skip-as-green)"
  - "shell: bash on both windows-unit steps so $(bash …) is Git Bash command substitution, not a pwsh splat"
affects: [31-03-ssrf-guard, gsd-verify-work, Phase 40 SEC-05 @latest pins]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Gate + negative self-test pair (mirror quality_snapshot_gate_test.sh → quality_snapshot_gate.sh): a CI lint must be PROVABLY able to fail, exercised by a self-test that plants a violation in a tmp fixture via an env-pointed scan dir"
    - "Var-assembled violation token in the self-test source (ROOT_PKGS='./...') so the test script's own source never carries a bounded standalone-root `./...` that the lint would self-flag when scoped at scripts/"

key-files:
  created:
    - scripts/check_ci_go_packages.sh
    - scripts/check_ci_go_packages_test.sh
  modified:
    - .github/workflows/ci.yml

key-decisions:
  - "Lint excludes `go list` enumeration lines as well as comments: `go list ./...` is the legitimate package-list source (it is exactly what go_packages.sh wraps), not a gate verb in the F-015 prohibition (go test/build/vet/run + govulncheck). This makes scanning scripts/ exit 0 as the plan AC requires."
  - "Self-test assembles the raw-root token from a quoted variable (ROOT_PKGS) rather than writing it inline, so the test script's source never trips the lint under scripts/ scope; the WRITTEN fixture file still carries the real token to drive the failure path."
  - "shell: bash added ONLY to the two windows-unit steps (windows-latest defaults to pwsh; no defaults.run.shell in ci.yml); Linux unit-test/vulncheck steps left untouched (already bash)."

patterns-established:
  - "F-015 hygiene: every Go CI gate that EXERCISES packages (build/test/vet/vuln) sources $(bash scripts/go_packages.sh); a lint + self-test before Set up Go prevents raw `./...` from rotting back in."

requirements-completed: [F-015]

# Metrics
duration: ~25min
completed: 2026-06-29
---

# Phase 31 Plan 02: F-015 CI Hygiene Summary

**The 4 raw root `./...` Go CI gates (unit-test, windows-unit build+test, vulncheck) now source the owned package set from `$(bash scripts/go_packages.sh)`, guarded by a new `check_ci_go_packages.sh` lint with a negative self-test that provably fails on a planted `go test ./...`, plus `shell: bash` on both windows-unit steps so the command substitution runs under Git Bash.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-06-29T18:05Z (approx)
- **Completed:** 2026-06-29T18:30:48Z
- **Tasks:** 2 (both `type="auto"`)
- **Files modified:** 3 (2 created, 1 edited)

## Accomplishments

- **F-015 lint + negative self-test (Task 1).** `scripts/check_ci_go_packages.sh` greps `${AURA_CI_WORKFLOWS_DIR:-.github/workflows}` for the anchored standalone-root token `(^|[[:space:]])\./\.\.\.([[:space:]]|$)`, excluding comment lines (`:[[:space:]]*#`) and `go list` enumeration. `scripts/check_ci_go_packages_test.sh` plants a `go test ./...` fixture and asserts the lint exits non-zero (no-skip-as-green), then a clean scoped/helper/go-list fixture asserts exit 0. Self-test exits 0; both scripts executable.
- **4 helper-sourced Go gates + Windows shell fix (Task 2).** Edited `.github/workflows/ci.yml` at exactly the 4 target `run:` lines to use `$(bash scripts/go_packages.sh)`, added `shell: bash` to both windows-unit steps, and wired the self-test-then-gate pair into `build-and-lint` before "Set up Go". Lint now finds 0 raw `./...`; diff is 12 insertions / 4 deletions with no scoped/integration/coverage tier and no `@latest` pin touched.
- **Helper output validated (WSL).** `scripts/go_packages.sh` yields 59 owned packages with zero `web/node_modules` entries — proof the substitution produces a valid, non-empty argument list (local proxy for the windows-unit CI backstop).

## Task Commits

Each task was committed atomically:

1. **Task 1: Add the F-015 lint + negative self-test** — `ff3e8019` (ci)
2. **Task 2: Swap the 4 raw `./...` CI steps + wire the lint into build-and-lint** — `32904ce4` (ci)

**Plan metadata:** final `docs(31-02)` commit (this SUMMARY + STATE + ROADMAP + REQUIREMENTS).

## Gate Evidence (authoritative commands)

| Criterion | Command | Result |
|-----------|---------|--------|
| C3b lint self-test (F-015) | `bash scripts/check_ci_go_packages_test.sh` | exit 0 — case 1 planted `./...` → lint exit 1 with 'F-015'; case 2 clean fixture → exit 0 |
| C3b lint clean (F-015) | `bash scripts/check_ci_go_packages.sh` | exit 0 — "no raw root './...' in .github/workflows" |
| C3a swap present | `grep -c 'bash scripts/go_packages.sh' .github/workflows/ci.yml` | 8 (4 new swaps + 4 pre-existing build-and-lint uses; ≥6 required) |
| C3a windows shell | `grep -c 'shell: bash' .github/workflows/ci.yml` | 2 (both windows-unit steps; Linux steps unchanged) |
| Scope correctness | `AURA_CI_WORKFLOWS_DIR=scripts bash scripts/check_ci_go_packages.sh` | exit 0 — go_packages.sh's `go list ./...` NOT flagged |
| YAML well-formed | `python yaml.safe_load(ci.yml)` | 19 jobs parse; windows shell:bash + helper swaps + lint ordering confirmed |
| Helper output | `bash scripts/go_packages.sh` (WSL) | 59 packages, 0 web/node_modules, non-empty |

## Files Created/Modified

- `scripts/check_ci_go_packages.sh` — F-015 lint: rejects standalone-root `./...` in CI workflows; reads `AURA_CI_WORKFLOWS_DIR` (default `.github/workflows/`); excludes comment lines + `go list` enumeration.
- `scripts/check_ci_go_packages_test.sh` — negative self-test: plants a `go test ./...` fixture, asserts the lint exits non-zero, then asserts a clean fixture exits 0.
- `.github/workflows/ci.yml` — 4 Go gates helper-sourced (L96 unit-test, L124 windows build, L136 windows test, L189 vulncheck), `shell: bash` on both windows-unit steps, 2 lint steps in `build-and-lint` before "Set up Go".

## Decisions Made

- **`go list` exclusion in the lint token.** The bare anchored `./...` token also matches go_packages.sh's own `go list ./...`. Since `go list` is the legitimate enumeration verb (not a gate verb in the F-015 prohibition set: go test/build/vet/run + govulncheck), the lint additionally excludes `go list` lines. This satisfies the plan's AC that `AURA_CI_WORKFLOWS_DIR=scripts …` exits 0, and is principled — enumeration is what feeds the helper, not a package-exercising gate.
- **Var-assembled token in the self-test** (`ROOT_PKGS='./...'`) so the self-test's own source carries no bounded standalone-root `./...`; otherwise the lint would self-flag the test file when scoped at scripts/. The written fixture still drives the real failure path. (See Deviations — discovered during Task 1.)
- **`shell: bash` placement:** added right after each windows-unit step's `name:` (before the go-test step's comment block), keeping the comments attached to `run:`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Lint token must exclude `go list` + self-test must not self-flag**
- **Found during:** Task 1 (lint + self-test authoring; verifying AC#3 `AURA_CI_WORKFLOWS_DIR=scripts` → exit 0)
- **Issue:** The research/plan token `(^|[[:space:]])\./\.\.\.([[:space:]]|$)` matches `go list ./...` in `scripts/go_packages.sh` (line 7) and would also match the inline `go test ./...` fixture in the self-test's own source — so scanning `scripts/` flagged both, contradicting the plan AC "scripts/ has no standalone-root `./...`, only a scoped pipeline ... does NOT flag go_packages.sh's `go list ./...`".
- **Fix:** (a) Added a `grep -vE 'go list[[:space:]]'` exclusion to the lint (go list is enumeration, not a gate verb in the F-015 prohibition); (b) assembled the self-test's violation token from a quoted `ROOT_PKGS='./...'` variable so the test source never carries a bounded literal token while the written fixture still does.
- **Files modified:** scripts/check_ci_go_packages.sh, scripts/check_ci_go_packages_test.sh
- **Verification:** `bash scripts/check_ci_go_packages_test.sh` exit 0; `AURA_CI_WORKFLOWS_DIR=scripts bash scripts/check_ci_go_packages.sh` exit 0 ("no raw root './...' in scripts"); `.github/workflows/` still flags the 4 targets pre-fix and 0 post-fix.
- **Committed in:** `ff3e8019` (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 bug — token/scope correctness).
**Impact on plan:** Necessary for the plan's own acceptance criterion (scripts/ scope exits 0) and the F-015 intent (gate verbs, not enumeration). No scope creep — the lint still rejects every prohibited raw `go test/build/vet/run ./...` + `govulncheck ./...`; the 4 workflow swaps are exactly as specified.

## Issues Encountered

None beyond the deviation above. Pre-commit hooks (vet + file-size cap) passed on both task commits.

## Threat Model Verification

Per the plan's `<threat_model>`:
- **T-31-CI-01 (Repudiation/DoS-of-assurance, mitigate):** the 4 Go gates now source the owned set from `go_packages.sh` (Makefile parity), and the lint — proven failable by its negative self-test (no-skip-as-green) — rejects standalone-root `./...`. ASVS V1 build/CI integrity satisfied.
- **T-31-CI-WIN (Tampering/silent no-op, mitigate):** `shell: bash` on both windows-unit steps makes `$(bash …)` real Git Bash command substitution; `go_packages.sh` validated to emit 59 non-empty packages (no empty-arg `go build ""`). CI windows-unit pass on merge is the post-push backstop.
- **T-31-CI-SSRF (N/A):** no SSRF/network/secret surface in this plan (owned by 31-03).
- **T-31-SC (Tampering/tool installs, accept):** no package install; the pre-existing `@latest` govulncheck/deadcode pins are untouched (SEC-05/Phase 40 scope). Package Legitimacy Audit = N/A.

No new threat surface introduced — no Threat Flags.

## Next Phase Readiness

- F-015 / SEC-07 closed: CI Go gates run exactly the owned package set, backed by a self-tested lint. Ready for 31-03 (SSRF guard / C5 / SEC-08).
- **Post-push backstop (not local):** confirm the windows-unit job passes green in the Actions run for the merge — the only genuinely new-risk swap (31-RESEARCH Pitfall 1). Local YAML parse + WSL helper-output check are the strongest pre-merge proxies; the live Windows runner is the final confirmation.
- No blockers.

## Self-Check: PASSED

- SUMMARY created: `.planning/phases/31-stabilization-ci-unblock/31-02-SUMMARY.md` — FOUND
- Created file `scripts/check_ci_go_packages.sh` — FOUND
- Created file `scripts/check_ci_go_packages_test.sh` — FOUND
- Modified file `.github/workflows/ci.yml` — FOUND (helper swaps + shell:bash + lint steps)
- Commit `ff3e8019` (Task 1) — present
- Commit `32904ce4` (Task 2) — present

---
*Phase: 31-stabilization-ci-unblock*
*Completed: 2026-06-29*
