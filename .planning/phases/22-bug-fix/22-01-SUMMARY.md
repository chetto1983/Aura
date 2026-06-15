---
phase: 22-bug-fix
plan: "01"
subsystem: agent-runtime-hardening
tags: [panic-recovery, dedup, race, goleak, metrics]

requires:
  - phase: 22-bug-fix
    provides: audit context and plan split for AG-001, AG-002, AG-039, AG-040
provides:
  - panic recovery at agent tool, workflow child, swarm child, and shell_bg reaper boundaries
  - model-visible panic errors instead of process crashes for planned goroutine sites
  - mutex-protected dedup ring with eviction pruning and repeated-cycle detection
affects: [agent-runtime, workflow, swarm, shell-bg, budget-dedup, observability]

tech-stack:
  added: []
  patterns:
    - bounded panic metric labels through internal/agent/panicobs
    - named-return panic recovery at goroutine boundaries
    - dedup ring state guarded by a private mutex

key-files:
  created:
    - internal/agent/panicobs/panicobs.go
  modified:
    - internal/agent/llm_agent.go
    - internal/agent/llm_agent_parallel.go
    - internal/agent/llm_agent_parallel_test.go
    - internal/agent/metrics.go
    - internal/agent/tools/shell_bg.go
    - internal/agent/tools/shell_bg_test.go
    - internal/agent/workflow/parallel.go
    - internal/agent/workflow/parallel_test.go
    - internal/swarm/swarm.go
    - internal/swarm/swarm_test.go
    - internal/agent/budget_dedup.go
    - internal/agent/budget_dedup_test.go

key-decisions:
  - "Introduced internal/agent/panicobs as a tiny leaf package so agent, workflow, swarm, and tools can publish the same bounded panic metric without import cycles."
  - "Recovered tool panics are returned as normal tool-call error results so the model can observe and route around the failure."
  - "Dedup result cache entries are pruned only after an evicted fingerprint is no longer present elsewhere in the ring."

patterns-established:
  - "Goroutine boundary panic firewall: recover, increment a bounded metric label, convert to per-child/per-call failure, and let siblings continue."
  - "Shared ring state uses internal locking; callers do not coordinate dedup synchronization externally."

requirements-completed: [HARDEN-01, HARDEN-02, HARDEN-09, HARDEN-12]

duration: 14min
completed: 2026-06-15
---

# Phase 22-01: Crash Firewall + Dedup Race Hardening Summary

**Agent panics now degrade to model-visible child/tool errors while the dedup ring is race-clean, bounded, and resilient to repeated cycles.**

## Performance

- **Duration:** 14 min
- **Started:** 2026-06-15T14:21:12Z
- **Completed:** 2026-06-15T14:34:58Z
- **Tasks:** 2
- **Files modified:** 13

## Accomplishments

- Closed AG-001 by recovering panics at `executeBatch`, `LlmAgent.Run`, workflow parallel children, swarm wave children, and the `shell_bg` reaper.
- Added `aura_agent_panic_total{site=...}` through bounded site labels: `execute_batch`, `llm_agent_run`, `workflow_parallel`, `swarm_wave`, and `shell_bg_reaper`.
- Closed AG-002 by serializing `dedupRing` and covering concurrent before/after tool-result access with the race detector.
- Closed AG-039 and AG-040 by pruning evicted result state and detecting stable period-3+ repeated loops.

## Task Commits

1. **Task 1: Goroutine panic firewall** - `62a81cde` (fix)
2. **Task 2: Dedup ring hardening** - `86cb7a22` (fix)

## Files Created/Modified

- `internal/agent/panicobs/panicobs.go` - shared panic metric recorder with bounded site labels.
- `internal/agent/metrics.go` - agent-local wrapper for recovered panic metrics.
- `internal/agent/llm_agent.go` - top-level run backstop for unexpected agent panics.
- `internal/agent/llm_agent_parallel.go` - tool panic conversion for inline and goroutine execution paths.
- `internal/agent/workflow/parallel.go` - workflow child panic conversion to the child error slot.
- `internal/swarm/swarm.go` - swarm child panic conversion to failed child reports while siblings continue.
- `internal/agent/tools/shell_bg.go` - background shell reaper recovery and failed/done state marking.
- `internal/agent/budget_dedup.go` - mutex-protected ring, result pruning, and period-N repeated-cycle detection.
- `internal/agent/*_test.go`, `internal/agent/workflow/*_test.go`, `internal/swarm/*_test.go`, `internal/agent/tools/*_test.go` - focused panic, race, pruning, and loop-regression coverage.

## Verification

- `go test -race ./internal/agent -run 'TestExecuteBatch.*Panic|TestLlmAgent.*Panic' -count=1`
- `go test -race ./internal/agent/workflow -run 'TestParallel.*Panic|TestParallel.*Leak' -count=1`
- `go test -race ./internal/swarm -run 'TestSwarm.*Panic|TestRunWave.*Panic' -count=1`
- `go test -race ./internal/agent/tools -run 'TestBackgroundShell.*Panic|TestShellBg.*Panic' -count=1`
- `go test -race ./internal/agent -run 'TestBudget_Dedup|TestDedupRing|TestBudget_BeforeAfterToolResult_Concurrent|TestDispatch.*Parallel.*Dedup' -count=1`
- `go test -race ./internal/agent/... ./internal/swarm/... -count=1`
- Pre-commit hooks on both commits: `gofmt`, `go vet`, and Go file-size check.

## AG Ledger Status

- **AG-001:** Fixed - panicking planned goroutine/tool sites no longer crash the process and surface as per-call/per-child failures.
- **AG-002:** Fixed - dedup ring state is mutex-protected and race-clean under parallel dispatch.
- **AG-039:** Fixed - evicted fingerprints prune stale cached results when no longer present in the active ring.
- **AG-040:** Fixed - stable repeated cycles with period 3 or greater are detected and terminated.

## Decisions Made

- Used `internal/agent/panicobs` instead of placing metrics in `internal/agent` so lower-level packages could record panic totals without creating import cycles.
- Kept panic recovery local to each goroutine boundary so sibling work continues and callers receive ordinary error/report objects.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Shared panic metric package needed to avoid import cycles**
- **Found during:** Task 1 (goroutine panic firewall)
- **Issue:** Workflow, swarm, and tools packages needed to record the same panic metric, but importing `internal/agent` from those packages would create import cycles.
- **Fix:** Added a small leaf package, `internal/agent/panicobs`, and kept the existing `internal/agent/metrics.go` wrapper for agent-local callers.
- **Files modified:** `internal/agent/panicobs/panicobs.go`, `internal/agent/metrics.go`
- **Verification:** `go test -race ./internal/agent/... ./internal/swarm/... -count=1`
- **Committed in:** `62a81cde`

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Required for the planned metric and recovery sites. No scope creep.

## Issues Encountered

- Red tests reproduced the intended failures first: workflow and swarm child panics crashed their package tests, the dedup concurrent test hit a map race, period-3 cycles were missed, and the shell reaper test required an injectable reaper function. All are resolved and covered by the verification commands above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Wave 2 can build on a crash-contained agent perimeter and a race-clean dedup ring.
- Remaining Phase 22 hardening areas are secrets/observability, MCP/router/budget limits, hooks/provenance/workflow tool behavior, and final AG ledger reconciliation.

---
*Phase: 22-bug-fix*
*Completed: 2026-06-15*
