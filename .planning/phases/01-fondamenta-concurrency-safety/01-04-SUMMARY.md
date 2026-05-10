---
phase: 01-fondamenta-concurrency-safety
plan: "04"
subsystem: concurrency
tags: [go, actor-pattern, usergate, concurrency, telegram, scheduler, session]

requires:
  - phase: 01-02
    provides: "UserGate (Acquire/TryAcquire/Evict/Close/IsActive) and InactivityTracker from internal/concurrency"

provides:
  - "Bot.gate field + userGate() accessor wiring UserGate into telegram package"
  - "onMessage serialized through UserGate.Acquire with fallback goroutine when gate is nil"
  - "dispatchReminder uses TryAcquire for non-blocking FIFO notification delivery"
  - "notifyAgentJob uses TryAcquire with silent drop on full inbox"
  - "UserGate.IsActive(userID) method on the gate type"
  - "SessionStore delegates IsActive to gate.IsActive when gate is configured"
  - "NewSessionStore accepts optional *UserGate for gate-aware construction"

affects:
  - "01-05 (setup.go must set b.gate = userGate and b.sessions = NewSessionStore(userGate))"

tech-stack:
  added: []
  patterns:
    - "Actor-inbox pattern: onMessage calls Acquire (blocking enqueue), handleConversation runs in actor goroutine"
    - "TryAcquire non-blocking drop pattern: scheduler notifications never block on full inbox (CONC-02)"
    - "Fallback nil-gate pattern: all gate calls guard with userGate() == nil for backward compat in tests"
    - "Variadic optional gate: NewSessionStore(gate ...*UserGate) allows zero-arg calls from tests"

key-files:
  created: []
  modified:
    - internal/telegram/bot.go
    - internal/telegram/handlers.go
    - internal/telegram/scheduler_handlers.go
    - internal/agentruntime/session.go
    - internal/concurrency/gate.go

key-decisions:
  - "Keep active sync.Map in SessionStore for no-gate fallback so existing tests pass without modification"
  - "Use variadic parameter on NewSessionStore for zero-arg backward compat instead of breaking API change"
  - "Actor context not threaded into handleConversation -- eviction threshold is 30min, users mid-conversation stay touched"
  - "dispatchReminder returns nil on TryAcquire failure (drop-and-retry via scheduler tick) rather than error"
  - "notifyAgentJob returns (false, nil) on TryAcquire failure -- notification is best-effort, not delivery guarantee"

patterns-established:
  - "Gate nil-guard pattern: all callers of userGate() check for nil and fall back to legacy goroutine behavior"
  - "Scheduler drop pattern: TryAcquire false -> return nil; scheduler retries on next tick (D-05)"
  - "Inbox copy pattern: bodyCopy/chatIDCopy/msgCopy capture loop variables before passing to Entry.Process closure"

requirements-completed:
  - CONC-01
  - CONC-02

duration: 15min
completed: "2026-05-10"
---

# Phase 01 Plan 04: Telegram Bot UserGate Wiring Summary

**UserGate actor pattern wired into onMessage (Acquire) and all scheduler notification paths (TryAcquire), with SessionStore delegating IsActive to the gate**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-05-10T12:42:00Z
- **Completed:** 2026-05-10T12:57:40Z
- **Tasks:** 3
- **Files modified:** 5

## Accomplishments

- `onMessage` now serializes per-user message processing through `UserGate.Acquire` instead of spawning raw goroutines (CONC-01, D-15). Prevents concurrent messages from corrupting conversation state.
- `dispatchReminder` and `notifyAgentJob` both use `TryAcquire` for non-blocking inbox enqueue; scheduler notifications can never deadlock the conversation handler (CONC-02, D-06).
- `SessionStore.IsActive` delegates to `UserGate.IsActive` when a gate is configured; the `active sync.Map` is retained as fallback for test code that constructs a store without a gate.
- `UserGate.IsActive(userID)` method added to `internal/concurrency/gate.go` to support the session store delegation.

## Task Commits

Each task was committed atomically:

1. **Task 1: Add userGate field and accessor to Bot struct** - `8106502` (feat)
2. **Task 2: Wire UserGate.Acquire into onMessage for per-user serialization** - `365d244` (feat)
3. **Task 3: Add TryAcquire to scheduler paths and delegate IsActive to UserGate** - `84947d7d` (feat)

## Files Created/Modified

- `internal/telegram/bot.go` - Added `gate *concurrency.UserGate` field to `Bot` struct; added `userGate()` accessor returning `b.gate` with nil guard; added `internal/concurrency` import
- `internal/telegram/handlers.go` - Replaced `go b.handleConversation(c)` with `UserGate.Acquire` path; nil-gate fallback preserves goroutine behavior for tests
- `internal/telegram/scheduler_handlers.go` - `dispatchReminder` uses `TryAcquire` (drops silently on full inbox, scheduler retries); `notifyAgentJob` uses `TryAcquire` (returns false,nil on drop); both fall back to direct send when gate is nil
- `internal/agentruntime/session.go` - `NewSessionStore` accepts optional `*UserGate`; `IsActive` delegates to `gate.IsActive` when gate set; `Begin`/`clearActive`/`Clear` guard on gate presence for active map usage
- `internal/concurrency/gate.go` - Added `IsActive(userID string) bool` method (mutex-guarded actors map lookup)

## Decisions Made

- **Actor context not threaded into handleConversation**: The eviction threshold is 30 minutes; users actively in a conversation are Touched after every inbox entry so they won't be evicted mid-turn. Threading the context would require a signature change to handleConversation and is deferred.
- **Variadic NewSessionStore signature**: `NewSessionStore(gate ...*UserGate)` instead of a breaking change. Existing callers (tests, setup.go lazy-init in bot.go) call `NewSessionStore()` with zero args and get the no-gate fallback behavior unchanged.
- **active sync.Map retained for no-gate case**: Removing it would break `TestSessionFinishAndAbortClearActiveMarker` and related tests that rely on `IsActive` returning false after `Finish()`. The map is only used when `gate == nil`.
- **Drop-and-retry on TryAcquire false**: Both scheduler paths return `nil` (not an error) when TryAcquire returns false. The scheduler treats nil as success, advancing the tick counter; it will retry on the next scheduled tick (D-05). This avoids filling the scheduler error log with transient inbox-full conditions.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Retained active sync.Map in SessionStore for backward compat**
- **Found during:** Task 3 (session.go refactor)
- **Issue:** The plan said to remove `active sync.Map` entirely, replacing fallback with context map check. But `IsActive` returning false after `Finish()` is a contract that existing tests rely on; context map never gets cleared by `Finish()`. Removing active map would break 3 tests.
- **Fix:** Kept `active sync.Map` alongside `gate` field; gate path skips the active map entirely, no-gate path maintains original active-map semantics.
- **Files modified:** `internal/agentruntime/session.go`
- **Verification:** `go test ./internal/agentruntime/` passes all 5 tests including `TestSessionFinishAndAbortClearActiveMarker`
- **Committed in:** `84947d7d` (Task 3 commit)

---

**Total deviations:** 1 auto-fixed (Rule 2 - correctness)
**Impact on plan:** Fix was necessary for correctness. No scope creep -- active map is still a single sync.Map field, just conditionally used.

## Issues Encountered

None beyond the deviation documented above.

## Threat Surface Scan

All new network paths and trust boundaries are covered by the plan's threat register (T-01-15 through T-01-18). No new endpoints, auth paths, or schema changes introduced beyond what the plan specified.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- All `internal/telegram/` and `internal/agentruntime/` changes are in place for Plan 05 (setup.go wiring).
- Plan 05 must: create `UserGate` with `concurrency.New(cfg)`, set `b.gate = userGate`, and update `b.sessions = agentruntime.NewSessionStore(userGate)`.
- The `userGate()` accessor and `NewSessionStore` variadic signature are backward-compat -- Plan 05 can set the fields without worrying about callers in tests.

---
*Phase: 01-fondamenta-concurrency-safety*
*Completed: 2026-05-10*
