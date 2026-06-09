# Audit: internal/agent/workflow

**Verdict:** needs-work — one infinite busy-loop defect (empty-subs + maxIter=0) + one ignored yield return value in LoopAgent.

**Counts:** critical 0 / high 0 / medium 1 / low 1

## Findings

### [MEDIUM][BUG] LoopAgent with empty subs and maxIterations=0 spins forever

**Location:** `internal/agent/workflow/loop.go:67-126`

**Confidence:** high

**Detail:**
`LoopAgent.Run` has two nested loops: the outer `for iterIdx` loop (infinite when `maxIterations == 0`) and the inner `for _, sub := range a.subs`. When `subs` is empty, the inner loop body never executes: no budget step is consumed, no event is yielded, no escalation fires, and `ic.Ctx.Done()` is never checked. The outer loop therefore spins at 100% CPU on one goroutine indefinitely with zero observable effect.

Relevant code path:
```go
// loop.go:67
for iterIdx := uint(0); a.maxIterations == 0 || iterIdx < a.maxIterations; iterIdx++ {
    for _, sub := range a.subs {   // if len(subs)==0: body never runs
        subIC := ic.WithSubAgent(sub)
        ...
        // all budget.ConsumeStep / yield calls are inside this body
    }
    // nothing outside the inner loop breaks the outer one
}
```

All current callers pass at least one sub (verified across the full repo). The public variadic constructor `NewLoop(name string, maxIter uint, subs ...agent.Agent)` accepts zero subs without error. Any future caller that constructs `NewLoop("name", 0)` with no subs triggers an unrecoverable CPU-spin on the calling goroutine.

**Suggested fix:** Guard at the top of `Run` (or in `NewLoop`):
```go
// In NewLoop: validate at construction time.
if len(subs) == 0 {
    panic("workflow.NewLoop: at least one sub-agent is required")
}
// OR in Run: break out of the outer loop immediately.
if len(a.subs) == 0 {
    return
}
```
A panic in the constructor is preferable (fail-fast, caught by tests) over a silent early return in Run.

---

### [LOW][BUG] Ignored yield return value in LoopAgent.Run on error path

**Location:** `internal/agent/workflow/loop.go:74`

**Confidence:** high

**Detail:**
When a sub-agent yields `(ev, err)` with a non-nil error, `LoopAgent.Run` calls:
```go
yield(ev, err) // a REAL failure surfaces through the error slot, then stop
return
```
The bool return of `yield` is discarded. Because `return` follows unconditionally, the consumer's break signal is already honoured (the iterator stops regardless), so there is no functional defect. However, this is inconsistent with the adjacent pattern in `sequential.go:62` (`if !yield(ev, err) { return }`) and with Go's range-over-function contract, which specifies that producers must not call `yield` after it returns `false`. Discarding the bool here is technically permitted (the function never calls yield again), but it obscures intent and could produce a linter warning if the linter is strict.

**Suggested fix:**
```go
// loop.go:73-75
if err != nil {
    _ = yield(ev, err) // bool intentionally ignored: we return immediately
    return
}
```
Or follow the SequentialAgent pattern exactly: `if !yield(ev, err) { return }`.
The explicit `_ =` documents intent; `!yield` + return is equally correct.

---

## What was checked and found clean

- **Goroutine leaks (ParallelAgent):** The done/results/ack channel choreography is correct. `close(results)` is called exactly once (by the background goroutine after `eg.Wait()`). `close(done)` is called exactly once (by `defer close(done)` in the iterator frame). No send-on-closed-channel is possible: `runSub` goroutines are all joined by `eg.Wait()` before `close(results)` executes.
- **Backpressure ack:** Each per-event `ack` channel is created fresh and closed at most once (by the iterator frame at line 121). `runSub` only reads from it. No double-close path.
- **Budget sharing (SC#3):** `ParallelAgent` calls `ic.Budget.Child(len(a.subs))` per child, sharing the `*atomic.Int32` step counter across the whole tree. `LoopAgent` calls `ic.WithSubAgent(sub)` which shares the same Budget (no forking). Correct per D-09/D-10.
- **TOCTOU-safe ConsumeStep:** `budget.go:ConsumeStep` uses atomic decrement-then-restore; concurrent goroutines in `ParallelAgent` cannot over-spend the shared cap.
- **Escalation propagation:** Both SequentialAgent and LoopAgent yield the escalate event before returning (D-21). LoopAgent correctly propagates escalate on non-tool events (line 84-86) and on multi-tool events after all per-call step events are yielded (line 120-122).
- **WR-01 (escalate on exactly one step event):** `scopeToToolCall` clears `Escalate` on all but the last per-call event for multi-tool turns.
- **WR-02 (within-turn dedup counting):** `seenThisTurn` map correctly gates the dedup ring for duplicate (name,args) calls within a single Event.
- **Dead code:** All unexported functions (`joinBranch`, `findInTree`, `iterLabel`, `toolCalls`, `resultPreview`, `canonArgs`, `scopeToToolCall`, `terminalEvent`, `guardToolCall`, `runSub`) are called within the package. All exported types and constructors are referenced in production code (`cmd/aura/agent.go`, `internal/runner/runner.go`).
- **Not-wired code:** All three agents (`LoopAgent`, `SequentialAgent`, `ParallelAgent`) are wired into production paths and tests. No dead constructors or unused struct fields.
- **Race conditions:** LoopAgent and SequentialAgent are single-goroutine. ParallelAgent's concurrency is bounded by errgroup + per-event ack backpressure; no shared mutable state outside the atomic Budget counter (which is safe by construction).
- **Context propagation:** `LoopAgent` and `SequentialAgent` propagate `ic.Ctx` via `WithSubAgent` (shared Ctx). `ParallelAgent` uses `WithContext(egCtx)` with an errgroup-derived context that cancels on first child error or explicit `cancel()`.
