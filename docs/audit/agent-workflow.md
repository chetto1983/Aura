# Audit: internal/agent/workflow

**Verdict:** needs-work — one semantic bug (misleading `termination_reason` key on dedup termination), one D-22 footgun violation (ignored yield return value), one naming issue on a signal channel, and an untested branch-label asymmetry between orchestrators.

**Counts:** critical 0 / high 0 / medium 1 / low 3

---

## Findings

### [MEDIUM][BUG] `termination_reason` hardcoded as `"budget_exhausted"` for dedup termination

**Location:** `internal/agent/workflow/loop.go:240`
**Confidence:** high

`LoopAgent.terminalEvent` always sets `StateDelta["termination_reason"] = "budget_exhausted"` regardless of whether the termination cause is step-cap, wallclock, or dedup:

```go
StateDelta: map[string]any{
    "termination_reason": "budget_exhausted",
    "limit_hit":          reason,   // reason ∈ {"max_steps", "wallclock", "dedup"}
```

When the loop trips on a dedup veto (`BeforeToolCall` returns `dedup=true`), the emitted terminal event carries `termination_reason: "budget_exhausted"` and `limit_hit: "dedup"`. Any consumer that routes on `termination_reason` to distinguish a dedup termination from a budget-exhaustion termination cannot do so — `termination_reason` is the same in both cases. Only `limit_hit` is the correct discriminator, but the field name `termination_reason` implies it is the primary distinguishing key.

The same pattern exists in `internal/agent/llm_agent_events.go:227` (`terminalBudgetEvent`) — the codebase is consistent internally, but the naming is misleading across the public Event contract.

**Affected call sites:**
- `loop.go:166–168` — dedup trip
- `loop.go:174–177` — step-cap trip
- `llm_agent_events.go:222–232` — same shape for `LlmAgent`

**Suggested fix:** Either (a) set `termination_reason` to `"dedup_loop"` when `reason == "dedup"` and `"budget_exhausted"` otherwise, or (b) rename `termination_reason` to `termination_class` and document that `limit_hit` is the precise cause. If the two-field design is locked, document in the `Actions` godoc that `termination_reason` is always `"budget_exhausted"` and `limit_hit` is the actual discriminating field.

---

### [LOW][BUG] `yield(ev, err)` return value discarded on error path in `LoopAgent.Run`

**Location:** `internal/agent/workflow/loop.go:77`
**Confidence:** high

```go
for ev, err := range sub.Run(subIC) {
    if err != nil {
        yield(ev, err)   // D-22: return value silently ignored
        return
    }
```

Every other yield in this file is guarded (`if !yield(...) { return }`). This one discards the bool, which the project's D-22 rule explicitly forbids ("every yield is guarded"). Functionally harmless because the function returns immediately after regardless of `yield`'s return value. But it violates the project invariant and leaves the code inconsistent with the same pattern in `sequential.go:62-63` and every other yield site.

**Suggested fix:**
```go
if err != nil {
    yield(ev, err) // return value irrelevant — we return either way, but honor D-22
    return
}
```
Or use `_ = yield(ev, err)` (consistent with the intentional-discard pattern already used at lines 167 and 176 for terminal events).

---

### [LOW][OTHER] `done` signal channel typed `chan bool` instead of `chan struct{}`

**Location:** `internal/agent/workflow/parallel.go:83`
**Confidence:** high

```go
done := make(chan bool)
```

This channel is used purely as a close-to-signal mechanism — no value is ever sent on it; `runSub` and the fan-in goroutine only `<-done` (receive on close). `chan struct{}` is the Go idiom for zero-allocation signal channels. `chan bool` wastes one byte per receive on a closed channel and signals intent incorrectly. Not a correctness issue, but misleading to readers who might expect a boolean value to be sent.

**Suggested fix:**
```go
done := make(chan struct{})
```
Update the parameter type in `runSub` (`done <-chan bool` → `done <-chan struct{}`).

---

### [LOW][OTHER] ParallelAgent inserts its own name in child branch labels; Sequential does not — untested asymmetry

**Location:** `internal/agent/workflow/parallel.go:92` vs `internal/agent/workflow/sequential.go:60`
**Confidence:** medium

`ParallelAgent` constructs child branches as `<parent>.<parallelName>.<childName>`:
```go
childIC.Branch = joinBranch(joinBranch(ic.Branch, a.name), sub.Name())
```

`SequentialAgent` constructs child branches as `<parent>.<childName>` (no orchestrator-name segment):
```go
subIC.Branch = joinBranch(ic.Branch, sub.Name())
```

`LoopAgent` inserts the iteration label (`iter-N`) not the agent name:
```go
subIC.Branch = joinBranch(joinBranch(ic.Branch, iterLabel(iterIdx)), sub.Name())
```

The three orchestrators are inconsistent. Sequential's children cannot be disambiguated from their parent's siblings if two sequential orchestrators at the same level have children with the same name. No test in `parallel_test.go` asserts branch values (the loop test `TestLoopAgent_StopsAtMaxIterations` does), so the parallel branch shape is unverified.

This may be intentional (parallel fan-out genuinely needs the extra segment to keep branches unique), but the asymmetry is undocumented and could produce confusing trace labels in production telemetry.

**Suggested fix:** Document the divergence in the `Run` godoc for `ParallelAgent`, or make sequential and parallel consistent by also inserting the sequential agent's name. Add a test asserting the exact branch label format each orchestrator produces.

---

## What was checked

- All non-test `.go` files in `internal/agent/workflow/` (4 files: `workflow.go`, `sequential.go`, `loop.go`, `parallel.go`).
- All `*_test.go` files read to understand intended invariants (D-21, D-22, WR-01, WR-02, WR-05, SC#2, SC#3, D-03, D-05).
- Cross-repo grep for: all exported symbols (`NewSequential`, `NewLoop`, `NewParallel`, `SequentialAgent`, `LoopAgent`, `ParallelAgent`), all unexported helpers (`joinBranch`, `findInTree`, `scopeToToolCall`, `toolCalls`, `resultPreview`, `canonArgs`, `iterLabel`, `terminalEvent`, `guardToolCall`, `runSub`).
- Budget interaction verified against `internal/agent/budget.go` and `budget_dedup.go`.
- Concurrency model verified: errgroup fan-out, ack channel, done close-signal, cancel propagation.
- Loop variable capture safe (go 1.26.4, loopvar fix applies).
- No goroutine leaks identified beyond what goleak already gates.
- No nil-deref, integer overflow (iterIdx uint wraparound is budget-bounded), or JSON mishandling found.
