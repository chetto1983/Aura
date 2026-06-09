# Audit: internal/agent/workflow

**Verdict:** needs-work — one untested error-abort asymmetry between SequentialAgent and LoopAgent; otherwise the concurrency and budget logic is well-engineered.

**Counts:** critical 0 / high 1 / medium 0 / low 1

## Findings

---

### [HIGH][BUG] SequentialAgent.Run does not abort the chain on a sub error

**Location:** `internal/agent/workflow/sequential.go:56-70`

**Confidence:** high

**Detail:**

`SequentialAgent.Run` iterates sub agents in order. When a sub yields a real error `(ev, err)` through the iterator, the sequential agent calls `yield(ev, err)` and — if the consumer returns `true` (continues) — keeps ranging over the same sub's remaining events and then advances to the **next sub in the chain**. It does NOT abort the sequential chain on error.

`LoopAgent.Run` (loop.go:76-79) has the opposite behavior:

```go
if err != nil {
    yield(ev, err) // surface the error
    return         // ← STOPS the entire loop immediately
}
```

`SequentialAgent.Run` (sequential.go:61-68) has no equivalent guard:

```go
for ev, err := range sub.Run(subIC) {
    if !yield(ev, err) {
        return // only stops on consumer break
    }
    if ev != nil && ev.Actions.Escalate {
        return // only stops on escalate
    }
    // ← falls through to the next iteration of sub.Run, then the next sub
}
```

**Consequence:** if sub1 fails (e.g., LLM timeout), sub2 and sub3 still execute in a degraded state. The consumer sees the error tuple from sub1 and then normal events from sub2/sub3 interleaved — there is no clean abort. For the intended use cases (onboarding loop Phase 14, AG-UI fan-out Phase 12), this can result in partial-work being committed after a known failure.

There is no test that covers the error path in `SequentialAgent.Run`; the two existing tests (`TestSequentialAgent_RunsAllSubsInOrder`, `TestSequentialAgent_PropagatesEscalate`) use only happy-path and escalate fixtures.

**Suggested fix:**

Add the same early-return-on-error guard as LoopAgent:

```go
for ev, err := range sub.Run(subIC) {
    if !yield(ev, err) {
        return
    }
    if err != nil {
        return // abort the chain on a real sub failure (D-04 parity with LoopAgent)
    }
    if ev != nil && ev.Actions.Escalate {
        return
    }
}
```

Update the docstring to mention this invariant, and add a test using an `erroringAgent` fixture (already defined in `parallel_test.go`) to assert sub3 is not invoked after sub2 fails.

---

### [LOW][BUG] ParallelAgent uses `chan bool` as a close-only signal channel

**Location:** `internal/agent/workflow/parallel.go:83`

**Confidence:** high

**Detail:**

```go
done := make(chan bool)
```

`done` is used exclusively as a **close signal**: it is only ever closed (`defer close(done)`) and received from (`case <-done`). No values are ever sent into it. The canonical Go idiom for a close-signal channel is `chan struct{}`, which costs 0 bytes per receive vs 1 byte for `chan bool`. The current code is correct (closing a `chan bool` unblocks all receivers), but deviates from the standard Go convention that `chan struct{}` is the zero-allocation signal type, and could mislead a future reader into thinking boolean values are being communicated.

**Suggested fix:**

```go
done := make(chan struct{})
```

Update the `runSub` signature accordingly:

```go
func runSub(ctx context.Context, ic agent.InvocationContext, sub agent.Agent,
    results chan<- result, done <-chan struct{}) error {
```

---

## What was checked

- All four non-test `.go` files in `internal/agent/workflow/`: `workflow.go`, `loop.go`, `sequential.go`, `parallel.go`.
- All five test files read to understand intended behavior and confirmed coverage gaps.
- All exported symbols grepped across the repo (`D:/Aura`) to verify wiring and usage.
- `go vet ./internal/agent/workflow/` — clean.

**No issues found in:**
- LoopAgent budget/dedup logic (guardToolCall, terminalEvent, scopeToToolCall): correct.
- LoopAgent multi-tool per turn (WR-05): correct — one step Event per tool call, steps_consumed = yielded step Events.
- LoopAgent WR-01 (escalate rides exactly one scoped Event): correct.
- LoopAgent WR-02 (within-turn duplicate skips dedup ring): correct via seenThisTurn map.
- ParallelAgent goroutine drain on early consumer break (D-23): correct — defer close(done) drains runSub goroutines.
- ParallelAgent escalate-cancels-siblings (D-03): correct — captured cancel(), not a fake error.
- ParallelAgent error forwarding (D-04/D-05): correct — real errors surface once through errgroup; intentional cancels return nil.
- ParallelAgent waiter goroutine: no leak — exits after eg.Wait() completes.
- Budget sharing (SC#3): LoopAgent uses ic.WithSubAgent (same Budget), ParallelAgent uses Budget.Child (shared *atomic.Int32, distinct dedup ring) — correct.
- Dead code: none. All unexported helpers (joinBranch, findInTree, iterLabel, toolCalls, resultPreview, canonArgs, scopeToToolCall, terminalEvent, guardToolCall) are used within the package.
- Not-wired: NewLoop is used in cmd/aura/agent.go (dry-run) and spike prototypes. NewSequential and NewParallel are infrastructure for future slices (Phase 12/14) — this is expected given the tabula-rasa phase sequencing.
