---
phase: 31-stabilization-ci-unblock
plan: 01
subsystem: testing
tags: [ci, file-size-gate, vite, vitest, coverage, go-embed, dist-freshness, verify-only]

# Dependency graph
requires:
  - phase: 23-cockpit-shell
    provides: committed Vite bundle embedded via //go:embed all:dist (internal/webui/dist)
  - phase: 25-cockpit-overhaul
    provides: frontend test suite + vitest 85% coverage thresholds
provides:
  - "Attributable GREEN baseline for the QUAL-01 stabilization criteria (C1 file-size, C2 dist freshness, C4 frontend coverage) measured on the CI-matching Node 24.16.0 / npm 11.17.0 toolchain"
  - "Proof (not assumption) that no Wave-2 regression can be blamed on a pre-existing red gate"
affects: [31-02-ci-hygiene, 31-03-ssrf-guard, gsd-verify-work]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Verify-only baseline plan: assert existing CI gates green via their authoritative commands, zero source mutation, single docs commit"

key-files:
  created:
    - .planning/phases/31-stabilization-ci-unblock/31-01-SUMMARY.md
  modified: []

key-decisions:
  - "No remediation needed — all three gates were already green; the contingency rebuild (C2) and contingency branch-tests (C4) were not triggered."
  - "Marked QUAL-01 complete: this plan verifies all three of its criteria (C1/C2/C4); C3 (F-015) and C5 (SEC-08) are separate requirements owned by 31-02/31-03."

patterns-established:
  - "Baseline-then-implement: prove the pre-existing gates green before any wave mutates CI/code so later regressions are attributable."

requirements-completed: [QUAL-01]

# Metrics
duration: ~3min
completed: 2026-06-29
---

# Phase 31 Plan 01: Stabilization Baseline (QUAL-01) Summary

**All three QUAL-01 CI gates re-asserted GREEN on the CI-matching Node 24.16.0 / npm 11.17.0 toolchain with zero source mutation — file-size cap (C1) exit 0, committed dist byte-identical to a fresh build (C2), and all four vitest thresholds ≥85 (C4, branches binding at 85.62%).**

## Performance

- **Duration:** ~3 min
- **Started:** 2026-06-29T18:11:45Z
- **Completed:** 2026-06-29T18:14:52Z
- **Tasks:** 3 (all verify-only)
- **Files modified:** 0 source files (1 doc created: this SUMMARY)

## Accomplishments

- **C1 — 600-LOC file-size cap GREEN.** `bash scripts/check-file-size.sh` exits 0 with "all source files within the 600-LOC cap." The two Codex-split files are confirmed under the cap and unchanged: `cmd/aura/serve_webui.go` = 525 LOC, `web/src/__tests__/LoginPage.test.tsx` = 592 LOC. The only tracked file > 600 is the generated `internal/db/sqlc/assets.sql.go` (722 LOC), which the script's `^internal/db/sqlc/` filter exempts.
- **C2 — dist freshness GREEN.** A clean `npm ci` (705 packages, 0 vulnerabilities) + `npm run build` (built in 7.12s) into `internal/webui/dist`, then `git diff --exit-code -- internal/webui/dist/` exits 0 — the committed embedded bundle is byte-identical to a fresh Node-24 build. No incidental web/ churn; no non-test web/src or tokens/ file modified.
- **C4 — frontend coverage GREEN.** `cd web && npm run test` (= `vitest run --coverage`) exits 0 with all four global thresholds met: Statements 91.3% (3778/4138), Branches 85.62% (2626/3067), Functions 91.36% (1174/1285), Lines 93.01% (3501/3764). Branches is the binding figure (floor 85) and sits 0.62 pp above — consistent with 31-RESEARCH's measured ~85.45%. `web/vitest.config.ts` thresholds unchanged (still 85/85/85/85).

## Task Commits

This plan is verify-only — it produces no source changes, so there are no per-task source commits. All evidence is captured in this SUMMARY; the single commit is the plan-metadata (docs) commit below.

1. **Task 1: Assert 600-LOC file-size cap (C1)** — no source change (gate already green, exit 0)
2. **Task 2: Assert dist freshness vs Node-24 rebuild (C2)** — no source change (zero diff after rebuild)
3. **Task 3: Assert four vitest thresholds ≥85 (C4)** — no source change (gate already green, exit 0)

**Plan metadata:** see final `docs(31-01)` commit (SUMMARY + STATE + ROADMAP + REQUIREMENTS).

## Gate Evidence (authoritative commands)

| Criterion | Command | Result |
|-----------|---------|--------|
| C1 file-size | `bash scripts/check-file-size.sh` | exit 0 — "all source files within the 600-LOC cap" |
| C2 dist freshness | `cd web && npm ci && npm run build` → `git diff --exit-code -- internal/webui/dist/` | exit 0 — empty diff (built in 7.12s) |
| C4 frontend coverage | `cd web && npm run test` | exit 0 — S 91.3% / B 85.62% / F 91.36% / L 93.01% (all ≥85) |

## Files Created/Modified

- `.planning/phases/31-stabilization-ci-unblock/31-01-SUMMARY.md` — this baseline record (only new file).
- No source, web/src, tokens/, or internal/webui/dist file was modified.

## Decisions Made

- None beyond the plan: all gates were already green, so neither contingency path (C2 dist rebuild-and-commit, C4 targeted branch-test additions) was triggered. No threshold lowered, no file re-split, no cap raised.

## Deviations from Plan

None - plan executed exactly as written. Expected modification set was EMPTY and it stayed EMPTY.

## Issues Encountered

None. The working tree after all three gate runs shows only the pre-existing modified planning files (`.planning/STATE.md`, `.planning/graphs/*`) — no source/web/dist churn introduced by this plan.

## Threat Model Verification

Per the plan's `<threat_model>` (T-31-VERIFY-01, Repudiation/assurance, disposition: mitigate): each gate fails loud on regression and none has a `$CI` skip branch, satisfying no-skip-as-green. All three ran live and returned real PASS (not skipped): file-size printed the within-cap line, the dist diff actually rebuilt + diffed, and vitest reported concrete coverage counts. `npm ci` installed only from the committed lockfile (T-31-SC, disposition: accept) — no new packages, 0 vulnerabilities, no legitimacy checkpoint required.

## Next Phase Readiness

- QUAL-01 baseline proven green and attributable. Wave 2 (31-02 CI hygiene / C3, 31-03 SSRF guard / C5) can proceed; any future red on C1/C2/C4 is now attributable to a Wave-2 change.
- No blockers.

## Self-Check: PASSED

- SUMMARY created: `.planning/phases/31-stabilization-ci-unblock/31-01-SUMMARY.md` — FOUND
- Asserted artifact `scripts/check-file-size.sh` — FOUND
- Asserted artifact `internal/webui/dist/index.html` — FOUND
- Asserted artifact `web/vitest.config.ts` — FOUND
- No source commits expected (verify-only); none claimed.

---
*Phase: 31-stabilization-ci-unblock*
*Completed: 2026-06-29*
