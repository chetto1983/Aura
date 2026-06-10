---
phase: 19-audit-bug-resolution-e2e-live-test
plan: 01
subsystem: agent-tools
tags: [shell_exec, truncation, process-groups, completion-critic]
requires: []
provides:
  - shell_exec process-group teardown with timeout WaitDelay
  - tail-reserving shell result truncation
  - completion-critic digest that preserves failure tails
affects: [agent-loop, shell-tools, completion-gate]
tech-stack:
  added: []
  patterns: [os-split process helpers, tail-reserving previews]
key-files:
  created:
    - internal/agent/tools/shell_exec_unix.go
    - internal/agent/tools/shell_exec_windows.go
  modified:
    - internal/agent/tools/shell_exec.go
    - internal/agent/tools/shell_exec_test.go
    - internal/agent/tools/result.go
    - internal/agent/tools/result_test.go
    - internal/agent/llm_agent_completion.go
    - internal/agent/llm_agent_completion_internal_test.go
key-decisions:
  - "Use cmd.Cancel plus cmd.WaitDelay with OS-specific process-group kill helpers."
  - "Keep NewResult unchanged and add NewResultReservingTail only for shell footer survival."
patterns-established:
  - "Shell output is body plus reserved footer: status, stderr tail, and structured aura_shell metadata stay visible under truncation."
requirements-completed: [H4, H5, L3, M-f]
duration: 45 min
completed: 2026-06-10
---

# Phase 19 Plan 01: Shell Execution Failure Visibility Summary

**Shell execution now kills orphaned process groups and keeps failure metadata visible even when stdout is huge.**

## Performance

- **Duration:** 45 min
- **Started:** 2026-06-10T09:58:00Z
- **Completed:** 2026-06-10T10:39:00Z
- **Tasks:** 2
- **Files modified:** 8

## Accomplishments

- Added POSIX and Windows process-group helpers and wired `shell_exec` through `cmd.Cancel` plus `cmd.WaitDelay`.
- Preserved stdout/stderr temporal interleave with a synchronized capture path and explicit cancelled/timeout status handling.
- Added `NewResultReservingTail` so shell exit status, stderr tail, and `[aura_shell {...}]` metadata survive preview truncation.
- Updated the completion critic digest to keep the tail of large tool results, so failed shell runs cannot be graded as done after head-only truncation.

## Task Commits

Each task was committed atomically:

1. **Task 1: Cross-platform process-group teardown** - `06478bdd` (fix)
2. **Task 2: Tail-reserving truncation** - `194854e3` (fix)

**Plan metadata:** this SUMMARY.md commit.

## Files Created/Modified

- `internal/agent/tools/shell_exec_unix.go` - POSIX process group setup and group kill.
- `internal/agent/tools/shell_exec_windows.go` - Windows taskkill-based process tree teardown.
- `internal/agent/tools/shell_exec.go` - single capture path, cancel/timeout classification, reserved shell footer.
- `internal/agent/tools/result.go` - additive tail-reserving result helper.
- `internal/agent/llm_agent_completion.go` - tail-preserving completion critic digest.
- `internal/agent/tools/*_test.go` and `internal/agent/llm_agent_completion_internal_test.go` - regressions for orphan reaping and footer survival.

## Decisions Made

Followed the plan's seam choices: OS-specific helpers are private to `tools`, non-shell callers keep `NewResult`, and shell-specific visibility uses an additive helper.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

- The first large-output fixture was too host-specific on Windows Git Bash. It was corrected to use PowerShell with shell-appropriate quoting on Windows and a pure POSIX loop elsewhere.

## Verification

- `go test -run 'TestShellExecTimesOut|TestShellExecLargeFailureKeepsFooter|TestNewResultReservingTail_LargeKeepsFooter' ./internal/agent/tools/` - passed.
- `go test -run TestSideEffectDigest_KeepsLargeShellFailureFooter ./internal/agent/` - passed.
- `go vet ./internal/agent/tools/ ./internal/agent/` - passed.
- `go build ./internal/agent/tools/ ./internal/agent/` - passed.
- `GOOS=windows go build ./internal/agent/tools/` - passed.
- `go test -race ./internal/agent/tools/` - passed.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

Wave 2 plan 19-02 can now build on a shell result stream whose failure footer and critic evidence are not hidden by large stdout.

---
*Phase: 19-audit-bug-resolution-e2e-live-test*
*Completed: 2026-06-10*
