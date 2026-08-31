---
phase: 49-memory-tiers
plan: 01
subsystem: memory
tags: [memory-recall, arcadedb, postgresql, opentelemetry, tdd, governance]

requires: []
provides:
  - "Measured PRD Amendment #201 extending #91 before reasoning-tier implementation"
  - "Machine-readable Phase 49 baseline/final evidence contract for mixed recall and reasoning isolation"
  - "Executable one-retrieval-surface contract for memory_recall"
affects: [49-02, 49-03, 49-04, 49-08, 49-09, 49-11, 49-12, 49-13]

actuals:
  tokens: 15997
  tasks: 2
  commits: 5

tech-stack:
  added: []
  patterns:
    - "Measure live, commit the PRD amendment alone, then implement"
    - "Report evidence-tier contribution independently from the backend that executed"
    - "Represent absent live evidence as not_observed, never as a pass"

key-files:
  created:
    - scripts/agent_memory_eval_phase49.py
  modified:
    - prd.md
    - scripts/agent_memory_eval.py
    - scripts/agent_memory_eval_test.py
    - internal/agent/mcptools/bridge_memory_surface_test.go
    - .planning/phases/49-memory-tiers/49-01-PLAN.md
    - .planning/phases/49-memory-tiers/49-04-PLAN.md
    - .planning/phases/49-memory-tiers/49-11-PLAN.md
    - .planning/phases/49-memory-tiers/49-12-PLAN.md
    - .planning/phases/49-memory-tiers/49-VALIDATION.md
    - .planning/phases/49-memory-tiers/COVERAGE.md

key-decisions:
  - "Used Amendment #201 because the planned #181 and the provisional #200 were already occupied; all Phase 49 ancestry gates now bind to #201."
  - "Kept final Phase 49 evaluator observations explicitly not_observed until later live plans supply evidence."
  - "Split the Phase 49 evidence contract into a focused module so agent_memory_eval.py remains under 600 lines."

patterns-established:
  - "Retrieval metadata has two independent axes: retrieval.effective_path for contributing tiers and retrieval.path for the executed backend."
  - "The model-facing memory surface contains one retrieval operation plus separately classified mutations."

requirements-completed: [MEM-06, TOOL-05]

coverage:
  - id: D1
    description: "Measured Amendment #201 extends #91 and predates every Phase 49 reasoning implementation path."
    requirement: MEM-06
    verification:
      - kind: integration
        ref: "authenticated live memory_recall baseline against ArcadeDB 26.8.1 and EmbeddingGemma 768d"
        status: pass
      - kind: other
        ref: "git diff-tree/merge-base Amendment #201 six-path ancestry gate"
        status: pass
    human_judgment: false
  - id: D2
    description: "Evaluator reports mixed-tier and reasoning-isolation baseline/final evidence without conflating tier and backend paths."
    requirement: TOOL-05
    verification:
      - kind: unit
        ref: "scripts/agent_memory_eval_test.py#Phase49EvidenceContractTest"
        status: pass
      - kind: other
        ref: "python scripts/agent_memory_eval.py --tier deterministic"
        status: pass
    human_judgment: false
  - id: D3
    description: "memory_recall is the sole model-facing memory retrieval while upsert and forget remain mutations."
    requirement: TOOL-05
    verification:
      - kind: unit
        ref: "internal/agent/mcptools/bridge_memory_surface_test.go#TestMemorySurfacePolicy_AliasKeepsIsolationAndHiddenSurface"
        status: pass
    human_judgment: false

duration: 1h 5m
completed: 2026-08-31
status: complete
---

# Phase 49 Plan 01: Memory Baseline and Unified Retrieval Contract Summary

**Live memory-recall evidence recorded in Amendment #201, with honest mixed-tier/reasoning evaluator states and one model-facing retrieval contract.**

## Performance

- **Duration:** 1h 5m
- **Started:** 2026-08-31T16:37:45Z
- **Completed:** 2026-08-31T17:43:16Z
- **Tasks:** 2
- **Files modified:** 11

## Accomplishments

- Measured the authenticated `memory_recall` path before implementation: semantic recall used `hybrid`, entity recall used `graph`, an unrelated query abstained explicitly, and 25 published-recall samples measured p50 39.358 ms / p95 85.395 ms / max 122.599 ms.
- Committed PRD Amendment #201 as a `prd.md`-only ancestor, extending #91 with PostgreSQL authority, rebuildable conversation projection, explicit-only reasoning, direct-evidence capture, and 30-day successful / 7-day failed-or-cancelled reasoning retention policy.
- Added deterministic evaluator records for `mixed_tier_recall` and `reasoning_isolation`, including counts, ranks, latency, abstention, separate `effective_path`/`path`, and matching `memory.recall.*` OTel attributes.
- Expanded the bridge contract over the real ten-operation memory server: exactly `memory_recall` is retrieval, while `memory_upsert_fact` and `memory_forget` remain model-facing mutations.

## Task Commits

Each task was committed atomically:

1. **Task 1: Measure the published recall path and amend the PRD** — `f231f15b5` (`docs`)
2. **Task 2 RED: Add failing evaluator and bridge contracts** — `e70fb2776` (`test`)
3. **Task 2 GREEN: Freeze machine-readable evaluation evidence** — `9b7cc96b2` (`feat`)
4. **Rule 1/2 deviation: Correct Phase 49 ancestry references** — `571bc20f4` (`docs`)

## Files Created/Modified

- `prd.md` — Amendment #201 with measured service/tool/retrieval/source/event baselines, ratified contracts, policy-vs-capacity distinction, and explicit non-proofs.
- `scripts/agent_memory_eval_phase49.py` — Phase 49 scenario definitions, baseline/final observations, path/count/rank/OTel validation, and fail-closed unobserved evidence rules.
- `scripts/agent_memory_eval.py` — Includes and validates Phase 49 evidence in every manifest/report.
- `scripts/agent_memory_eval_test.py` — 26-test suite with four Phase 49 evidence-contract tests and repository-root module invocation support.
- `internal/agent/mcptools/bridge_memory_surface_test.go` — Full ten-operation fixture proving one retrieval plus two mutations and identity-safe aliasing.
- Phase 49 plan/validation/coverage files — Corrected ancestry lookups from occupied Amendment #181 to the measured Amendment #201.

## Decisions Made

- Amendment #201 is the governance ancestor. Amendment #181 already belonged to Phase 51, and #200 was also occupied; duplicating either number would have made later `git log --grep` gates bind to unrelated history.
- Baseline fact-only evidence is `observed_partial`; reasoning and all final evidence are `not_observed`. Missing tiers or telemetry do not become green by absence.
- `retrieval.effective_path` derives only from returned evidence tiers. `retrieval.path` records only the actual `graph`, `hybrid`, or `lexical` backend. The corresponding OTel values must match independently.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1/2 - Governance correctness] Replaced occupied Amendment #181 with #201**
- **Found during:** Task 1 required reading
- **Issue:** PRD #181 already described Phase 51 EventSource completion, and #200 already described the spreadsheet-renderer dependency change. Reusing either would corrupt history and make six-path ancestry checks falsely pass against an unrelated commit.
- **Fix:** Parsed the complete amendment-number inventory, selected the next unique number (#201), kept its commit `prd.md`-only, and changed only the six Phase 49 planning files containing executable #181 ancestry references.
- **Files modified:** `prd.md`, `49-01-PLAN.md`, `49-04-PLAN.md`, `49-11-PLAN.md`, `49-12-PLAN.md`, `49-VALIDATION.md`, `COVERAGE.md`
- **Verification:** No Phase 49 `Amendment #181` reference remains; `git diff-tree` for `f231f15b5` returns only `prd.md`; six-path merge-base gates pass.
- **Committed in:** `f231f15b5`, `571bc20f4`

**2. [Rule 2 - Project constraint] Split the evidence contract to preserve the 600-line ceiling**
- **Found during:** Task 2 GREEN
- **Issue:** `scripts/agent_memory_eval.py` was already 594 lines, so implementing the contract inline would violate the refactor-on-touch ceiling.
- **Fix:** Added `agent_memory_eval_phase49.py`; the primary evaluator ends at 598 lines.
- **Files modified:** `scripts/agent_memory_eval.py`, `scripts/agent_memory_eval_phase49.py`
- **Verification:** Line count is 598; evaluator unit and deterministic tiers pass.
- **Committed in:** `9b7cc96b2`

**3. [Rule 3 - Blocking verification] Made the documented unittest module command importable**
- **Found during:** Task 2 RED
- **Issue:** `python -m unittest scripts.agent_memory_eval_test` failed before tests ran because the test imported sibling evaluator modules as top-level names while only the repository root was on `sys.path`.
- **Fix:** Added the test file's own directory to `sys.path`, preserving both direct-script and documented module invocation forms.
- **Files modified:** `scripts/agent_memory_eval_test.py`
- **Verification:** The command executes 26 tests and passes; RED then failed for the intended missing `phase49_evidence` contract instead of an import error.
- **Committed in:** `e70fb2776`

**4. [Rule 3 - Blocking state close-out] Restored canonical plan counters in STATE.md**
- **Found during:** Sequential metadata close-out
- **Issue:** `state.advance-plan` could not parse the legacy Current Position because `Current Plan` and `Total Plans in Phase` labels were absent; `state.update-progress` also reported the phase scope as unscoped and left the prose progress line stale.
- **Fix:** Added the canonical counters, reran `state.advance-plan` successfully to 2/14, and synchronized the prose progress with the handler-updated frontmatter (49/62 milestone plans).
- **Files modified:** `.planning/STATE.md`
- **Verification:** `state.advance-plan` returned `advanced:true`, `previous_plan:1`, `current_plan:2`, `total_plans:14`; STATE frontmatter and Current Position now agree.
- **Committed in:** final plan metadata commit

---

**Total deviations:** 4 auto-fixed (1 governance bug/critical gate, 1 project-constraint split, 2 blocking workflow fixes).
**Impact on plan:** The fixes preserve the intended governance order, prevent false ancestry proof, and add no runtime dependency or model-facing tool.

## Issues Encountered

- The top-level Aura container was running but unhealthy during measurement. PostgreSQL, ArcadeDB, EmbeddingGemma, and `arcadedb-mcp` were healthy, so Amendment #201 explicitly limits the evidence to the memory-service path and does not claim whole-daemon readiness.
- The shared checkout remained dirty in unrelated paths throughout execution. Every commit staged an explicit allowlist, and none of those paths entered Plan 49-01 history.

## TDD Gate Compliance

- **RED:** `e70fb2776` — four evaluator tests failed on missing `phase49_evidence`; the expanded bridge contract compiled and exercised the current surface.
- **GREEN:** `9b7cc96b2` — all 26 evaluator tests and the named bridge test passed.
- **REFACTOR:** No separate behavior-neutral commit was needed; the production split was required in GREEN to satisfy the existing 600-line constraint.

## Verification Evidence

- Authenticated live `memory_recall`: query=`hybrid`, entity=`graph`, weak query=`hybrid` + `no_qualified_candidates`; real ArcadeDB 26.8.1 and 768-dimensional EmbeddingGemma.
- Running published CLI/identity/`memory_search` comparison: 25 samples, p95 97.878 ms; recorded as a backend comparison, not as unified-retrieval proof.
- PostgreSQL aggregate: 2 eligible turns / 331 inline bytes / 0 spills; latest conversation max seq 6 / no compaction / 6 post-compaction rows; 0 reasoning rows; 2 successful terminal tool events.
- `python -m unittest scripts.agent_memory_eval_test`: 26 tests, pass.
- `python scripts/agent_memory_eval.py --tier deterministic`: `DETERMINISTIC_PASS`, 18 scored scenarios, Phase 49 evidence schema present.
- Named Go surface test, repository-wide `go vet ./...`, `go build ./...`, package unit, and package race gates all passed; the tracer verification was repeated before expansion as required.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Amendment #201 is a verified ancestor ready for every Phase 49 reasoning plan.
- Plan 49-02 can build the PostgreSQL-authoritative conversation projection; later live plans must replace `not_observed` final evidence rather than treating it as success.

## Self-Check: PASSED

- All declared key files exist.
- Task commits `f231f15b5`, `e70fb2776`, `9b7cc96b2`, and `571bc20f4` exist and contain no tracked-file deletions.
- Amendment #201 changes only `prd.md`; Phase 49 executable ancestry references resolve to #201.
- Coverage metadata classifies all three deliverables as auto-covered by passing evidence.
- No TODO/FIXME/placeholder stub marker exists in the created or modified implementation/test files.

---
*Phase: 49-memory-tiers*
*Completed: 2026-08-31*
