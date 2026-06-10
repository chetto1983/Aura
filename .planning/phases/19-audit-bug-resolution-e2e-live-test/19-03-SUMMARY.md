---
phase: 19-audit-bug-resolution-e2e-live-test
plan: 03
subsystem: llm-agent
tags: [sse, streaming, openai-compatible, chunk-error]
requires: []
provides:
  - llm.Chunk terminal Err contract
  - OpenAI-compatible stream error emit-before-close behavior
  - main agent loop rejection of partial text on stream errors
affects: [agent-loop, llm-client, openai-compatible-provider]
tech-stack:
  added: []
  patterns: [terminal Err chunks, partial-stream rejection]
key-files:
  created: []
  modified:
    - internal/llm/client.go
    - internal/llm/openai_compat/client.go
    - internal/llm/openai_compat/client_test.go
    - internal/agent/agenttest/fakeclient.go
    - internal/agent/llm_agent.go
    - internal/agent/llm_agent_completion.go
    - internal/agent/llm_agent_finalize.go
    - internal/agent/llm_agent_reasoning.go
    - internal/agent/llm_agent_test.go
key-decisions:
  - "Represent post-open streaming failures as terminal llm.Chunk.Err values emitted before channel close."
  - "The main agent loop surfaces Chunk.Err as an infra error and never finalizes accumulated partial text."
patterns-established:
  - "Secondary LLM drainers stop accumulating on Err and fall back through their existing safe paths."
requirements-completed: [H9]
duration: 35 min
completed: 2026-06-10
---

# Phase 19 Plan 03: Streaming Error Contract Summary

**Mid-stream SSE failures now surface as terminal chunk errors instead of being mistaken for complete assistant answers.**

## Performance

- **Duration:** 35 min
- **Started:** 2026-06-10T11:12:00Z
- **Completed:** 2026-06-10T11:47:00Z
- **Tasks:** 2
- **Files modified:** 9

## Accomplishments

- Added `Err error` to `llm.Chunk` as the terminal streaming error contract.
- Updated the OpenAI-compatible producer to emit `Chunk{Err: ...}` before closing on parse errors and EOF without finish_reason.
- Consumed the orphan `premature_close.sse` fixture in a regression test.
- Updated the main agent loop so a stream Err returns through the iterator error slot and never becomes a terminal answer.
- Updated completion critic, forced-finalization, and adaptive reasoning drainers to stop on Err and use their existing fail-open/fallback behavior.

## Task Commits

Each task was committed atomically:

1. **Task 1: Add Err to llm.Chunk and producer emit-before-close** - `57202e64` (fix)
2. **Task 2: Main agent loop rejects partial streams on Err** - `99163a36` (fix)

**Plan metadata:** this SUMMARY.md commit.

## Files Created/Modified

- `internal/llm/client.go` - `llm.Chunk.Err` contract.
- `internal/llm/openai_compat/client.go` - emits terminal Err chunks for malformed or finishless streams.
- `internal/llm/openai_compat/client_test.go` - premature-close and EOF-without-finish regressions.
- `internal/agent/agenttest/fakeclient.go` - `TextThenErr` scripted turn helper.
- `internal/agent/llm_agent.go` - stream Err detection in the main loop drain.
- `internal/agent/llm_agent_completion.go` - completion critic fail-open on Err.
- `internal/agent/llm_agent_finalize.go` - finalize synthesis returns an error on Err.
- `internal/agent/llm_agent_reasoning.go` - adaptive reasoning falls back on Err.
- `internal/agent/llm_agent_test.go` - partial-stream-is-not-terminal regression.

## Decisions Made

Followed the plan's Option (a): add a first-class `Err` field to `llm.Chunk` rather than overloading finish reasons or text markers.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

- The recovered side-worktree commit contained only producer changes; the consumer half was ported separately from the side-worktree diff and verified before commit.

## Verification

- `go test -run 'TestStream_PrematureCloseEmitsErrBeforeClose|TestStream_EOFWithoutFinishReasonEmitsUsageThenErr' ./internal/llm/openai_compat/` - passed.
- `go test -run TestLlmAgent_StreamErrDoesNotFinalizePartialText ./internal/agent/` - passed.
- `rg "premature_close\\.sse" internal/llm/openai_compat -n` - confirmed fixture is referenced by `client_test.go`.
- `go vet ./internal/llm/... ./internal/agent/` - passed.
- `go build ./...` - passed.
- `go test -race ./internal/llm/openai_compat/ ./internal/agent/` - passed.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

Wave 2 agent-loop work can assume stream failures are explicit and cannot silently produce a complete answer from partial accumulated text.

---
*Phase: 19-audit-bug-resolution-e2e-live-test*
*Completed: 2026-06-10*
