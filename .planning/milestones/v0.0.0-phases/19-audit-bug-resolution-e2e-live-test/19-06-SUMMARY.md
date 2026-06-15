---
phase: 19-audit-bug-resolution-e2e-live-test
plan: 06
subsystem: conversations
tags: [context-window, microcompact, tool-rounds, boundary-drop]
requires: []
provides:
  - boundary-aware dropOldestPairs behavior
  - dangling RoleTool head cleanup after L2.5 reduction
  - tool-round compaction regression fixture
affects: [conversation-history, llm-provider-wire-shape]
tech-stack:
  added: []
  patterns: [drop-by-conversation-round, tool-round integrity assertions]
key-files:
  created: []
  modified:
    - internal/conversations/context.go
    - internal/conversations/context_boundary_test.go
key-decisions:
  - "Keep the historical dropOldestPairs signature and count contract, but treat the count as dropped conversational rounds."
  - "Drop to the next RoleUser boundary and defensively skip any leading RoleTool that remains after reduction."
patterns-established:
  - "Context-window reductions preserve assistant(tool_calls)->tool-result adjacency for any retained tool round."
requirements-completed: [H8]
duration: 20 min
completed: 2026-06-10
---

# Phase 19 Plan 06: Microcompact Tool-Round Boundary Summary

**Microcompact now drops whole conversational rounds instead of fixed two-message slices, preventing orphan tool results from reaching the provider.**

## Performance

- **Duration:** 20 min
- **Completed:** 2026-06-10
- **Tasks:** 1
- **Files modified:** 2

## Accomplishments

- Replaced fixed `body = body[2:]` trimming with a boundary-aware helper that advances to the next `RoleUser`.
- Preserved the protected head behavior for system L0 and the injected always-block.
- Added a post-reduction guard that removes dangling leading `RoleTool` entries before projecting to LLM messages.
- Added a regression fixture for an assistant tool-call round with two tool results, plus a retained recent tool round integrity assertion.

## Task Commits

1. **Task 1: Drop by conversational boundary and add tool-round fixture** - `6c585510` (fix)

**Plan metadata:** this SUMMARY.md commit.

## Files Created/Modified

- `internal/conversations/context.go` - boundary-aware round dropping and dangling `RoleTool` head cleanup.
- `internal/conversations/context_boundary_test.go` - H8/D-04 regression fixture and tool-round integrity helper.

## Decisions Made

The exported/internal function signature stayed unchanged to avoid disturbing rot-event accounting; the existing `pairsDropped` value now records conversational rounds dropped.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## Verification

- `go test -run 'TestDropOldest|TestContextBoundary|TestMicrocompact|TestCompact' ./internal/conversations/` - passed.
- `go vet ./internal/conversations/` - passed.
- `go build ./internal/conversations/` - passed.
- `go test ./internal/conversations/` - passed.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

Wave 1 can continue with scheduler notification durability, MCP reconnect behavior, and environment loading fixes; conversation compaction now preserves provider-valid tool histories.

---
*Phase: 19-audit-bug-resolution-e2e-live-test*
*Completed: 2026-06-10*
