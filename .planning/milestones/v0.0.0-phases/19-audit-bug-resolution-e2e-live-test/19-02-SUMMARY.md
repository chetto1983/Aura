---
phase: 19-audit-bug-resolution-e2e-live-test
plan: 02
subsystem: agent-loop
tags: [agent, completion-gate, finalize, stream-retry]
requires: [19-01, 19-03]
provides:
  - content-stop veto keeps vetoed prose out of durable assistant history
  - completion critic stream open uses shared retry helper
  - finalize synthesis stream open uses shared retry helper
  - adaptive reasoning router stream open uses shared retry helper
affects: [llm-agent, completion-gate, finalize, reasoning-router]
tech-stack:
  added: []
  patterns: [bounded stream-open retry, veto nudge-only persistence]
key-files:
  created:
    - internal/agent/llm_agent_completion_retry_internal_test.go
  modified:
    - internal/agent/llm_agent.go
    - internal/agent/llm_agent_completion.go
    - internal/agent/llm_agent_completion_test.go
    - internal/agent/llm_agent_finalize.go
    - internal/agent/llm_agent_reasoning.go
    - internal/agent/llm_agent_reasoning_test.go
key-decisions:
  - "A content-stop completion veto appends only the feedback nudge; the vetoed answer is never durable RoleAssistant history."
  - "The critic, finalize synthesis, and reasoning router reuse streamWithOpenRetry instead of direct client.Stream opens."
patterns-established:
  - "Transient stream-open failures are retried once at helper level while final-error fallback behavior remains local to each caller."
requirements-completed: [M-a, M-b]
duration: 35 min
completed: 2026-06-10
---

# Phase 19 Plan 02: Agent Loop Veto and Stream Retry Summary

**The completion gate now holds on budget finalization, and the three direct stream-open bypasses use the shared retry helper.**

## Performance

- **Duration:** 35 min
- **Completed:** 2026-06-10
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments

- Removed the durable `RoleAssistant` append for vetoed content-stop prose; only the user-role completion nudge persists.
- Added a regression proving a vetoed content-stop answer is absent from both `a.history` and the forced finalize request.
- Routed completion critic, finalize synthesis, and adaptive reasoning router stream opens through `streamWithOpenRetry`.
- Added transient-open retry tests for critic, finalize, and reasoning router success paths.
- Added retry-exhaustion tests proving final fallback behavior remains unchanged: critic fails open, finalize returns its final stream error to synthesize fallback, and reasoning falls back to low.

## Task Commits

1. **Tasks 1-2: Veto persistence and stream-open retry** - `88bc012a` (fix)

**Plan metadata:** this SUMMARY.md commit.

## Files Created/Modified

- `internal/agent/llm_agent.go` - content-stop veto appends only the feedback nudge.
- `internal/agent/llm_agent_completion.go` - critic opens through `streamWithOpenRetry`.
- `internal/agent/llm_agent_finalize.go` - finalize synthesis opens through `streamWithOpenRetry`.
- `internal/agent/llm_agent_reasoning.go` - reasoning router opens through `streamWithOpenRetry`.
- `internal/agent/llm_agent_completion_test.go` and `llm_agent_reasoning_test.go` - black-box retry regressions.
- `internal/agent/llm_agent_completion_retry_internal_test.go` - white-box veto/finalize leak and synthesize retry coverage.

## Decisions Made

No new retry loop was added. All three sites call the existing shared helper, preserving the existing per-site fallback behavior after the bounded retry is exhausted.

## Deviations from Plan

None.

## Issues Encountered

The in-progress race run was interrupted by the operator, then rerun successfully before commit.

## Verification

- `go test -run 'TestContentStopVetoDoesNotPersistIntoFinalize|TestCompletionGate_CriticTransientOpenRetries|TestCompletionGate_CriticRetryExhaustedFailsOpen|TestSynthesizeRetriesTransientOpen|TestSynthesizeRetryExhaustedReturnsFinalError|TestLlmAgent_AdaptiveReasoningRouterRetriesTransientOpen|TestLlmAgent_AdaptiveReasoningRouterRetryExhaustedFallsBackLow|TestStreamOpenRetryDoesNotSleepPastContext|TestRetryableNetworkTextDocumentsPlanMarkers' ./internal/agent/` - passed.
- `go build ./internal/agent/` - passed.
- `go vet ./internal/agent/` - passed.
- `go test ./internal/agent/` - passed.
- `go test -race ./internal/agent/` - passed.

## User Setup Required

None.

## Next Phase Readiness

The Telegram and scheduler Wave 2 plans can proceed with the agent loop no longer re-surfacing vetoed hand-off prose and all loop-adjacent stream opens sharing the retry behavior used by the main path.

---
*Phase: 19-audit-bug-resolution-e2e-live-test*
*Completed: 2026-06-10*
