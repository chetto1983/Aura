---
phase: 19-audit-bug-resolution-e2e-live-test
plan: 04
subsystem: agui
tags: [ag-ui, sse, fanout, redaction, multimodal]
requires: []
provides:
  - AG-UI boundary frames are non-droppable under fanout backpressure
  - exported SanitizeString redaction seam
  - explicit rejection of unsupported multimodal run input
affects: [telegram-channel, agui-transport, reasoning-trace]
tech-stack:
  added: []
  patterns: [boundary-frame preservation, shared redaction seam]
key-files:
  created: []
  modified:
    - internal/agui/server.go
    - internal/agui/fanout.go
    - internal/agui/fanout_test.go
    - internal/agui/helpers_test.go
key-decisions:
  - "Treat AG-UI boundary frames as non-droppable and reserve drop-on-full for repeatable deltas."
  - "Export SanitizeString from AG-UI for the Wave 2 Telegram error-rendering fix."
patterns-established:
  - "In-process Fanout RUN_ERROR paths sanitize both emitted event messages and trace JSON."
requirements-completed: [H1, M-c, M-d]
duration: 30 min
completed: 2026-06-10
---

# Phase 19 Plan 04: AG-UI Transport Safety Summary

**AG-UI fanout now preserves protocol boundaries, redacts in-process error leaks, and rejects unsupported multimodal run input explicitly.**

## Performance

- **Duration:** 30 min
- **Started:** 2026-06-10T10:40:00Z
- **Completed:** 2026-06-10T11:10:00Z
- **Tasks:** 2
- **Files modified:** 4

## Accomplishments

- Widened the boundary-frame classifier so slow fanout subscribers cannot receive invalid START/CONTENT/END subsequences.
- Exported `SanitizeString` and reused it on Fanout source errors and reasoning-trace event JSON.
- Changed AG-UI run input extraction to return an explicit 400 for unsupported structured/multimodal user content instead of silently replaying old history.
- Added regressions for fanout sequence validity, source-error redaction, reasoning-trace redaction, and multimodal rejection.

## Task Commits

Each task was committed atomically:

1. **Task 1: Preserve AG-UI boundary frames** - `636ef23e` (fix)
2. **Task 2: Sanitize Fanout and reject multimodal input** - `3ea104cf` (fix)

**Plan metadata:** this SUMMARY.md commit.

## Files Created/Modified

- `internal/agui/server.go` - exported `SanitizeString`, explicit multimodal rejection, updated error sanitization callers.
- `internal/agui/fanout.go` - sanitized RUN_ERROR message construction and redacted trace JSON.
- `internal/agui/fanout_test.go` - boundary and redaction regressions.
- `internal/agui/helpers_test.go` - updated `lastUserMessage` contract tests.

## Decisions Made

Multimodal input is rejected rather than text-projected because the current runner accepts only text turns; rejecting is safer than silently dropping content.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

- The partial task-2 signature change temporarily broke package-wide vet until the helper tests were updated to the new `(message, error)` contract.

## Verification

- `go test -run 'TestFanoutSlowSubscriberDropped|TestFanoutSourceErrorRedactedInEventAndTrace|TestLastUserMessage' ./internal/agui/` - passed.
- `go test -run 'TestServer_RunBadRequests|TestServer_RunSSERoundTrip' ./internal/agui/` - passed.
- `go vet ./internal/agui/` - passed.
- `go build ./internal/agui/ ./internal/channels/...` - passed.
- `go test -race ./internal/agui/` - passed.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

Wave 2 plan 19-05 can reuse the exported `SanitizeString` seam for Telegram-facing error rendering, and all AG-UI clients can rely on protocol-valid fanout subsequences.

---
*Phase: 19-audit-bug-resolution-e2e-live-test*
*Completed: 2026-06-10*
