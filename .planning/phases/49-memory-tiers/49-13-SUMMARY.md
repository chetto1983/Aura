---
phase: 49-memory-tiers
plan: 13
subsystem: memory
tags: [arcadedb, memory-recall, opentelemetry, evaluator, active-context, tdd]

requires:
  - phase: 49-03
    provides: "Unified mixed fact/conversation recall with separate contribution and backend paths"
  - phase: 49-08
    provides: "Host-derived active-context carrier and authenticated negative-only recall scope"
provides:
  - "Live OAuth MCP proof of mixed fact and historical-conversation recall with active and foreign exclusion"
  - "Same-call response and OTel evidence for hybrid query, graph entity, and forced lexical fallback paths"
  - "Fail-closed mixed_tier_recall evaluator tier and calculated one-retrieval-operation surface evidence"
affects: [49-14, MEM-02, TOOL-05, memory-recall, agent-memory-evaluator]

actuals:
  tokens: 11237
  tasks: 2
  commits: 5

tech-stack:
  added: []
  patterns:
    - "Capture tool-call telemetry through official MCP server receiving middleware, not an HTTP request span"
    - "Emit bounded JSON evidence markers from live tests and validate them independently in the evaluator"
    - "Use bounded scalar exclusion parameters where indexed ArcadeDB collection predicates mis-filter live candidates"

key-files:
  created: []
  modified:
    - cmd/arcadedb-mcp/memory_live_integration_test.go
    - cmd/arcadedb-mcp/memory_live_integration_helpers_test.go
    - cmd/arcadedb-mcp/tool_memory_recall_test.go
    - internal/arcadedb/memory_recall_exclusion.go
    - internal/agent/mcptools/bridge_memory_surface_test.go
    - scripts/agent_memory_eval.py
    - scripts/agent_memory_eval_phase49.py
    - scripts/agent_memory_eval_test.py

key-decisions:
  - "Use mcp.Server.AddReceivingMiddleware so memory.recall attributes and the response come from the same authenticated tools/call context."
  - "Replace collection NOT IN with one bound scalar inequality per canonical active conversation after live ArcadeDB 26.8.1 dropped an eligible historical candidate."
  - "Keep agent_memory_eval.py below 600 lines by extending the existing Phase 49 evaluator module and exposing a thin mixed_tier_recall CLI branch."

patterns-established:
  - "Route evidence is complete only when returned provenance, response counts, and same-call OTel counts agree independently from backend selection."
  - "Required live-evaluator markers are small, one case per marker, so go test -json cannot split and corrupt evidence."

requirements-completed: [MEM-02, TOOL-05]

coverage:
  - id: D1
    description: "An authenticated live memory_recall returns an owner fact plus eligible historical conversation while excluding active and foreign sources."
    requirement: MEM-02
    verification:
      - kind: integration
        ref: "cmd/arcadedb-mcp/memory_live_integration_test.go#TestAgentMemoryMCPLive_MixedTierRecall under WSL -race"
        status: pass
    human_judgment: false
  - id: D2
    description: "Response and same-call OTel evidence agree separately on mixed tier contribution, exact counts, and hybrid/graph/lexical backend execution."
    requirement: TOOL-05
    verification:
      - kind: integration
        ref: "cmd/arcadedb-mcp/memory_live_integration_test.go#TestAgentMemoryMCPLive_BackendPath under WSL -race"
        status: pass
      - kind: other
        ref: "python3 scripts/agent_memory_eval.py --tier mixed_tier_recall"
        status: pass
    human_judgment: false
  - id: D3
    description: "The evaluator rejects absent tiers, active leakage, dishonest route/count evidence, zero scenarios, and any sibling model-facing retrieval operation."
    requirement: TOOL-05
    verification:
      - kind: unit
        ref: "scripts/agent_memory_eval_test.py#MixedTierRecallEvaluatorTest"
        status: pass
      - kind: unit
        ref: "internal/agent/mcptools/bridge_memory_surface_test.go#TestMemorySurfacePolicy_AliasKeepsIsolationAndHiddenSurface"
        status: pass
    human_judgment: false

duration: 48min
completed: 2026-09-01
status: complete
---

# Phase 49 Plan 13: Live Mixed-Tier Recall Evidence Summary

**Published OAuth memory recall now proves active-safe mixed evidence and independently attests tier contribution, counts, and hybrid/graph/lexical execution through same-call telemetry.**

## Performance

- **Duration:** 48 min
- **Started:** 2026-09-01T00:39:29Z
- **Completed:** 2026-09-01T01:27:56Z
- **Tasks:** 2
- **Files modified:** 8

## Accomplishments

- Added a non-skipping live MCP fixture that seeds owner fact, active conversation, eligible historical conversation, and foreign distractor evidence, then proves only the fact and historical conversation survive the published call.
- Added exact same-call response/OTel comparisons for mixed contribution, tier counts, hybrid query, graph entity traversal, and deliberately forced lexical fallback.
- Added the direct `mixed_tier_recall` evaluator tier with fail-closed marker parsing and fixtures for zero execution, absent tiers, active/foreign leakage, missing or conflated axes, wrong backends, OTel/count disagreement, and model-surface drift.

## Task Commits

Each task was committed in RED then GREEN order:

1. **Task 1 RED: Live mixed-tier exclusion contract** - `7a99521cc` (`test`)
2. **Task 1 GREEN: Published mixed recall, route telemetry, and live exclusion correction** - `162b95ad8` (`feat`)
3. **Task 2 RED: Fail-closed evaluator fixtures** - `101fb8c3a` (`test`)
4. **Task 2 GREEN: Direct mixed-tier evaluator and surface evidence** - `f6d43d96a` (`feat`)

## Files Created/Modified

- `cmd/arcadedb-mcp/memory_live_integration_test.go` - Live mixed/exclusion and backend-path scenarios with bounded route markers.
- `cmd/arcadedb-mcp/memory_live_integration_helpers_test.go` - Strict dependency mode, per-call header injection, official MCP receiving middleware, tenant control, and shared evidence helpers.
- `internal/arcadedb/memory_recall_exclusion.go` - Bounded scalar active-conversation predicates that preserve eligible indexed candidates.
- `cmd/arcadedb-mcp/tool_memory_recall_test.go` - Unit contract for the measured scalar negative-filter shape.
- `internal/agent/mcptools/bridge_memory_surface_test.go` - Calculated model-facing retrieval-operation marker.
- `scripts/agent_memory_eval.py` - Thin CLI route for `--tier mixed_tier_recall`, kept at 598 lines.
- `scripts/agent_memory_eval_phase49.py` - Live marker runner, route/count/OTel validator, report, and direct-result formatter.
- `scripts/agent_memory_eval_test.py` - 32-test evaluator suite including the mixed-tier fail-closed matrix.

## Decisions Made

- The MCP SDK's `Server.AddReceivingMiddleware` is the authoritative test seam for same-call telemetry. An outer HTTP span ends or loses context before the SDK invokes a session-bound tool handler.
- Active exclusions use canonical scalar parameters (`conversation_id <> :excluded_conversation_id_N`). The previously unit-tested collection `NOT IN` shape dropped the historical indexed candidate on the deployed ArcadeDB engine.
- Each live route case emits its own bounded marker. One combined backend marker exceeded `go test -json`'s output-event boundary and was correctly rejected as truncated rather than treated as evidence.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Live database bug] Preserved eligible history under active negative filtering**
- **Found during:** Task 1 GREEN live verification
- **Issue:** The projected owner active and historical turns were both independently retrievable, but `conversation_id NOT IN :excluded_conversation_ids` inside the nested indexed/vector query removed both, leaving fact-only evidence.
- **Fix:** Replaced the bounded collection predicate with one scalar inequality and parameter per canonical exclusion; retained the defensive hydration filter.
- **Files modified:** `internal/arcadedb/memory_recall_exclusion.go`, `cmd/arcadedb-mcp/tool_memory_recall_test.go`
- **Verification:** The live published call now reports `effective_path=mixed`, fact count 1, conversation count 1, and excludes the active conversation.
- **Committed in:** `162b95ad8`

**2. [Rule 2 - Missing critical evidence plumbing] Extended the shared live harness**
- **Found during:** Task 1 GREEN
- **Issue:** The existing helper could carry only fixed actor headers, skipped absent local dependencies, exposed no server tenant for a deliberate embedder failure, and had no same-call span seam.
- **Fix:** Added opt-in strict dependencies, context-derived header composition, returned tenant control, and official MCP receiving middleware while preserving existing callers' behavior.
- **Files modified:** `cmd/arcadedb-mcp/memory_live_integration_helpers_test.go`
- **Verification:** Missing required live dependencies fail the Plan 49-13 cases; query/entity/fallback and response/OTel equality pass under WSL `-race`.
- **Committed in:** `162b95ad8`

**3. [Rule 2 - Project constraint and evidence completeness] Used the Phase 49 split module and emitted calculated markers**
- **Found during:** Task 2 GREEN
- **Issue:** `agent_memory_eval.py` was already 598 lines, and evaluator output could not honestly report path/count/surface evidence from a bare Go test pass.
- **Fix:** Extended `agent_memory_eval_phase49.py`, kept the main file at 598 lines, and emitted bounded calculated route and model-surface markers from the proving tests.
- **Files modified:** `scripts/agent_memory_eval.py`, `scripts/agent_memory_eval_phase49.py`, `cmd/arcadedb-mcp/memory_live_integration_test.go`, `cmd/arcadedb-mcp/memory_live_integration_helpers_test.go`, `internal/agent/mcptools/bridge_memory_surface_test.go`
- **Verification:** Direct evaluator output names the scenario, both tier counts, `effective_path=mixed`, and query/entity/fallback backends; all malformed fixtures fail closed.
- **Committed in:** `f6d43d96a`

**4. [Rule 3 - Blocking state close-out] Synchronized out-of-order activity and progress metadata**
- **Found during:** Plan metadata close-out
- **Issue:** `state.update-progress` correctly refused the unscoped/out-of-order Plan 49-13 close and left prose activity/progress on Plan 49-04 even though the canonical completed-plan count advanced to 56.
- **Fix:** Preserved `current_plan: 5` as the next sequential incomplete slot, synchronized canonical/prose activity to Plan 49-13, and reconciled the progress display to 56/62 while the roadmap records 8/14 Phase 49 plans.
- **Files modified:** `.planning/STATE.md`, `.planning/ROADMAP.md`
- **Verification:** STATE keeps current Plan 5, reports 56 completed milestone plans, stops at Plan 49-13, and matches the ROADMAP 8/14 count.
- **Committed in:** final plan metadata commit

---

**Total deviations:** 4 auto-fixed (1 Rule 1 live bug, 2 Rule 2 correctness/project constraints, 1 Rule 3 state close-out).
**Impact on plan:** The additions are the minimum required to turn live behavior into honest evaluator evidence. No new model-facing tool, protocol, environment variable, dependency, endpoint, auth path, or schema was introduced.

## Issues Encountered

- The first HTTP-wrapper telemetry attempt produced no tool-handler attributes because the MCP SDK owns a session-level method context. Official v1.7.0 documentation/source identified receiving middleware as the supported tracing seam.
- A single combined backend marker was split by `go test -json`; the evaluator rejected the truncated JSON. Emitting one bounded marker per case made the evidence transport deterministic.
- The normal GREEN commit hook found and removed the now-unused `anyStringSlice` test helper. Hooks remained enabled for every commit.

## TDD Gate Compliance

- **Task 1 RED:** `7a99521cc` failed on the intended active-conversation leak while the live fact, both owner projections, and foreign isolation were present.
- **Task 1 GREEN:** `162b95ad8` passed both named live cases, full vet/build/package tests, WSL race, and the mandatory post-commit tracer feedback rerun.
- **Task 2 RED:** `101fb8c3a` ran 32 tests and failed 13 assertions solely because the mixed-tier evaluator entrypoint was absent.
- **Task 2 GREEN:** `f6d43d96a` passed all 32 evaluator tests, the direct live tier, surface proof, repository vet/build, package regression, and WSL race.
- **REFACTOR:** No behavior-neutral commit was needed; the existing Phase 49 module split and helper extraction were required during GREEN to keep every touched file below 600 lines.

## Verification Evidence

- `TestAgentMemoryMCPLive_MixedTierRecall` and `TestAgentMemoryMCPLive_BackendPath` pass against disposable identity databases on live ArcadeDB 26.8.1 and the 768-dimensional EmbeddingGemma endpoint under WSL `-race`.
- Live markers show mixed response/OTel counts `facts=1`, `conversations=1`, `reasoning=0`; query=`hybrid`, entity=`graph`, forced fallback=`lexical`.
- `python -m unittest scripts.agent_memory_eval_test`: 32 tests pass.
- `python3 scripts/agent_memory_eval.py --tier mixed_tier_recall`: `MIXED_TIER_RECALL_PASS` with the exact paths and counts above.
- `go vet ./...`, `go build ./...`, package unit, and WSL package race gates pass from committed HEAD.
- No named test skipped, no required suite ran zero scenarios, and no tracked file was deleted.

## Known Stubs

None.

## User Setup Required

None - no new dependency, environment variable, service, migration, or manual configuration is required.

## Next Phase Readiness

- Plan 49-14 and the Phase 49 verifier can consume the direct mixed-tier evaluator report as live route evidence.
- The stable `memory_recall` contract remains the sole model-facing retrieval surface, with contribution and backend axes independently auditable.
- No blocker remains for dependent work.

## Self-Check: PASSED

- All eight declared implementation/test files and this SUMMARY exist.
- Task commits `7a99521cc`, `162b95ad8`, `101fb8c3a`, and `f6d43d96a` exist in RED then GREEN order.
- Coverage classification reports 3/3 deliverables automatically covered by passing evidence.
- No tracked-file deletion, known stub, skipped test, unrun verification, or threat-surface expansion remains.

---
*Phase: 49-memory-tiers*
*Completed: 2026-09-01*
