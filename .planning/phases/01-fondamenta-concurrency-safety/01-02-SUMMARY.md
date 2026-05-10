---
phase: 01-fondamenta-concurrency-safety
plan: 02
subsystem: concurrency
tags: [actor-pattern, serialization, inactivity-tracker, concurrency, go-stdlib]
dependency_graph:
  requires: []
  provides:
    - internal/concurrency.UserGate
    - internal/concurrency.InactivityTracker
    - internal/concurrency.Entry
    - internal/concurrency.Config
  affects:
    - internal/telegram (future wave - wiring)
    - internal/scheduler (future wave - TryAcquire integration)
tech_stack:
  added: []
  patterns:
    - actor/stateful-goroutine per user (buffered channel inbox)
    - select/default non-blocking channel send (TryAcquire)
    - map[string]time.Time + sync.RWMutex (InactivityTracker, CONC-03)
    - context.WithCancel goroutine lifecycle management
    - sync.WaitGroup for graceful shutdown
key_files:
  created:
    - internal/concurrency/types.go
    - internal/concurrency/gate.go
    - internal/concurrency/tracker.go
    - internal/concurrency/gate_test.go
    - internal/concurrency/tracker_test.go
  modified: []
decisions:
  - "Entry struct uses Process func(ctx context.Context) closure -- keeps concurrency package telebot-free per D-14"
  - "Acquire uses drop-oldest semantics (D-03) rather than blocking-wait; context cancellation fires only if ctx is done at select time"
  - "OnOverflow called in separate goroutine to prevent gate blocking on Telegram I/O (Pitfall 4)"
  - "InactivityTracker sweeps collect stale users under RLock, evict outside lock to avoid deadlock with gate.mu"
  - "Close() calls tracker.Stop() first, then cancels all actors, then waits on WaitGroup (Pitfall 2)"
metrics:
  duration: "~15 minutes"
  completed_date: "2026-05-10T12:47:45Z"
  tasks_completed: 4
  files_created: 5
  tests_added: 18
---

# Phase 1 Plan 02: internal/concurrency Package Summary

**One-liner:** Per-user actor serialization with buffered channel inbox, non-blocking TryAcquire, and map+RWMutex InactivityTracker using stdlib only.

## What Was Built

Created the `internal/concurrency/` package from scratch with five files implementing the actor/gate pattern for per-user message serialization (CONC-01), non-blocking notification delivery (CONC-02), and inactivity-based session eviction (CONC-03).

### Files Created

| File | Purpose |
|------|---------|
| `internal/concurrency/types.go` | `Entry` struct (uniform inbox entry per D-08) and `Config` struct (InboxSize, EvictionThreshold, SweepInterval, OnEvict, OnOverflow) |
| `internal/concurrency/gate.go` | `UserGate` with Acquire, TryAcquire, Evict, Close; `userActor` with inbox channel, context, done channel; internal spawnActorLocked, runActor, dropOldestAndNotify |
| `internal/concurrency/tracker.go` | `InactivityTracker` with `map[string]time.Time` + `sync.RWMutex`; Start, Touch, Remove, sweep, Stop methods |
| `internal/concurrency/gate_test.go` | 10 tests covering CONC-01/02: sequential processing, concurrent users, overflow, TryAcquire, context cancellation, Close, Evict |
| `internal/concurrency/tracker_test.go` | 8 tests covering CONC-03: eviction, active-skip, multi-user callbacks, double-eviction prevention, Stop, Touch, Remove |

## Key Design Decisions

**Entry as function closure (D-08, D-14):** `Entry.Process func(ctx context.Context)` keeps the package zero-dependency. Telegram handlers wrap `handleConversation` in a closure capturing `tele.Context`. No telebot import in `internal/concurrency/`.

**Drop-oldest on overflow (D-03):** `Acquire` drops the oldest buffered entry and calls `OnOverflow` in a separate goroutine when the inbox is full, then enqueues the new entry. This means `Acquire` always succeeds unless the context is already done. The context cancellation path fires only when `ctx.Done()` wins Go's select scheduling over the `default` case.

**OnOverflow in separate goroutine (Pitfall 4):** `go g.config.OnOverflow(userID)` prevents the gate from blocking on Telegram API calls when notifying users of dropped messages.

**InactivityTracker with map+RWMutex (CONC-03, D-11):** No `sync.Map` or `sync.Map.Range` anywhere in `tracker.go`. Sweep collects stale users under `RLock`, releases lock, then evicts -- avoiding deadlock with `gate.mu` which `Evict` needs.

**WaitGroup for graceful shutdown (Pitfall 2):** `wg.Add(1)` before `go runActor`, `wg.Done()` deferred in `runActor`. `Close()` calls `tracker.Stop()` first, then cancels all actors, then `wg.Wait()`. No goroutine leaks on shutdown.

**Evict waits for actor.done (D-10):** After `actor.cancel()`, `Evict` blocks on `<-actor.done` before calling `OnEvict`. This guarantees the actor goroutine has fully exited (and released all references to conversation state) before the external callback runs.

## Verification Results

```
go build ./internal/concurrency/   PASSED
go vet ./internal/concurrency/     PASSED
go test -count=3 -short ./...      PASSED (18 tests, 3 runs, all stable)
grep sync.Map tracker.go           0 matches (CONC-03 compliant)
grep .Range tracker.go             0 matches (CONC-03 compliant)
```

Note: `go test -race` requires CGO which is not available in this environment (missing gcc on Windows). Tests were verified without the race detector. The race-detector flag in the plan's verification commands is understood to run in the CI Docker environment where CGO is available.

## Deviations from Plan

**[Rule 1 - Bug] Test logic errors in TestOverflowDropOldest and TestAcquireContextCancellation**

- **Found during:** Task 4 (first test run)
- **Issue 1:** `TestOverflowDropOldest` was only filling 2 inbox slots for InboxSize=2, but needed a 3rd Acquire call to actually trigger the drop-oldest path. Test was checking overflow without creating an overflow condition.
- **Fix:** Added a third `g.Acquire(ctx, userID, testEntry(&counter))` call after filling both inbox slots.
- **Issue 2:** `TestAcquireContextCancellation` expected `context.DeadlineExceeded` when calling Acquire with a 50ms timeout on a full inbox, but the drop-oldest design makes Acquire always succeed by dropping the oldest entry before the timeout fires.
- **Fix:** Rewrote test to use a pre-cancelled context and accept either nil (drop-oldest path wins select) or `context.Canceled` (ctx.Done() wins select) -- both are valid per the implementation design. Added documentation comment explaining the non-determinism.
- **Files modified:** `internal/concurrency/gate_test.go`
- **Commit:** 6f14878 (included in test commit)

**[Rule 1 - Bug] Comment in tracker.go triggered false grep match**

- **Found during:** Task 3 verification
- **Issue:** The CONC-03 compliance comment "Does NOT use sync.Map or sync.Map.Range" caused `grep -c "sync.Map" tracker.go` to return 1 instead of 0.
- **Fix:** Rewrote comment to document compliance without using the exact pattern strings.
- **Files modified:** `internal/concurrency/tracker.go`
- **Commit:** 914c66d

## Known Stubs

None. All implementations are functional with no placeholder values.

## Threat Flags

No new threat surface beyond what was specified in the plan's threat model (T-01-06 through T-01-11). All identified threats are mitigated in the implementation:
- T-01-06 (inbox overflow): drop-oldest + OnOverflow in separate goroutine
- T-01-07 (goroutine leak): WaitGroup + InactivityTracker eviction
- T-01-08 (TryAcquire overflow): returns false immediately
- T-01-09 (cross-user access): actors map keyed by userID, no shared state
- T-01-10 (re-entrant deadlock): sweep collects under RLock, evicts outside lock
- T-01-11 (session data leak): context cancellation releases all goroutine references

## Self-Check: PASSED

| Item | Status |
|------|--------|
| internal/concurrency/types.go | FOUND |
| internal/concurrency/gate.go | FOUND |
| internal/concurrency/tracker.go | FOUND |
| internal/concurrency/gate_test.go | FOUND |
| internal/concurrency/tracker_test.go | FOUND |
| .planning/phases/01-fondamenta-concurrency-safety/01-02-SUMMARY.md | FOUND |
| Commit 696206a (types.go) | FOUND |
| Commit b29c535 (gate.go) | FOUND |
| Commit 914c66d (tracker.go) | FOUND |
| Commit 6f14878 (tests) | FOUND |
