# Audit: internal/agent/agenttest

**Verdict:** needs-work — one latent panic in production-visible code, one shallow-clone gap in the immutability snapshot contract, one ignored yield return value on a terminal event.

**Counts:** critical 0 / high 0 / medium 1 / low 2

## Findings

---

### [MEDIUM][BUG] `LastRequest()` panics with index-out-of-range when `Requests` is empty

**Location:** `internal/agent/agenttest/fakeclient.go:90`

**Confidence:** high

**Detail:**

```go
func (f *FakeClient) LastRequest() llm.Request {
    f.mu.Lock()
    defer f.mu.Unlock()
    return f.Requests[len(f.Requests)-1]  // panics when len == 0
}
```

The doc comment says "panics if none", which makes the behaviour documented but not safe. When a test has a setup error (wrong scripted-turn count, early agent failure that skips the LLM call, or the agent uses a fast-path that bypasses `Stream`), the resulting panic produces no useful diagnostic: the test output shows a raw index-out-of-range from inside `agenttest`, not the actual test assertion that failed.

`FakeClient` is imported from production `cmd/aura` binaries (`agent.go`, `swarm_demo.go`, `cache_audit.go`) not just tests. In those paths `LastRequest()` is not called, but the risk surface is non-zero for future callers.

Every current test call to `LastRequest()` is preceded by a `CallCount()` assertion that would catch a zero-call scenario first. However, this is informal convention with no compiler or linter enforcement.

**Suggested fix:**

Return `(llm.Request, bool)` or return a zero value with a comment:
```go
func (f *FakeClient) LastRequest() (llm.Request, bool) {
    f.mu.Lock()
    defer f.mu.Unlock()
    if len(f.Requests) == 0 {
        return llm.Request{}, false
    }
    return f.Requests[len(f.Requests)-1], true
}
```
Or keep the panic but change to `t.Fatal` (not possible since `FakeClient` has no `testing.T`). At minimum, add a sentinel check and `panic(fmt.Sprintf("LastRequest called on a FakeClient with no recorded requests; use CallCount() first"))` so the panic message is actionable.

---

### [LOW][BUG] `FakeClient.Stream` shallow-clones `Messages` — inner `ToolCalls` slices share backing arrays

**Location:** `internal/agent/agenttest/fakeclient.go:57-58`

**Confidence:** medium

**Detail:**

```go
snap := req
snap.Messages = append([]llm.Message(nil), req.Messages...)
f.Requests = append(f.Requests, snap)
```

`append([]llm.Message(nil), req.Messages...)` copies each `llm.Message` struct by value. `llm.Message` contains `ToolCalls []llm.ToolCall` — a slice header. The copy gets an independent slice header but the two headers point at the same backing array.

If the agent were ever to mutate `msg.ToolCalls[i]` in-place on a message already appended to `history` (rather than replacing the slice), the captured snapshot in `f.Requests` would silently reflect the mutation — defeating the stated purpose of `Requests` as a "deep enough for immutability assertions" snapshot (fakeclient.go:29).

In the current codebase this is harmless: `llm_agent.go` always builds a new `[]llm.ToolCall` slice per turn inside `consume()` and sets it via `llm.Message{ToolCalls: calls}` — it never mutates elements of an existing `ToolCalls` slice. But the contract stated in the doc comment (`snap` comments say "deep enough for immutability") is not fully honoured.

The immutability test at `internal/agent/llm_agent_test.go:340` only asserts `Role` and `Content` string fields, so the gap in `ToolCalls` coverage is invisible to the current test suite.

**Suggested fix:**

Deep-clone each message:
```go
snap := req
msgs := make([]llm.Message, len(req.Messages))
for i, m := range req.Messages {
    mc := m
    if len(m.ToolCalls) > 0 {
        mc.ToolCalls = append([]llm.ToolCall(nil), m.ToolCalls...)
    }
    msgs[i] = mc
}
snap.Messages = msgs
f.Requests = append(f.Requests, snap)
```

---

### [LOW][BUG] `EmitNThenEscalate.Run` discards the `yield` return value on its terminal event

**Location:** `internal/agent/agenttest/mocks.go:106-111`

**Confidence:** high

**Detail:**

```go
yield(&agent.Event{
    Author:  author,
    Branch:  ic.Branch,
    Actions: agent.Actions{Escalate: true},
}, nil) // terminal event — return regardless of the yield result
```

The return value of the terminal `yield` call is silently discarded. This is intentional (`return` follows immediately so no second `yield` is ever called), and Go's range-over-func spec does not require the function to inspect the bool when it will return anyway. The current code is safe.

However, the comment "return regardless of the yield result" misframes the safety invariant. The actual invariant is: "after yield returns false, never call yield again." The code satisfies this because `return` follows unconditionally. But a future edit that inserts any code between the `yield` and `return` — such as cleanup, a second terminal event, or a loop — would silently violate the iter.Seq2 contract with no compiler warning.

The same pattern in `CountingAgent.Run` (mocks.go:194-200) correctly captures and ignores the budget-terminal yield result, also with an immediate `return`. Both are safe today.

**Suggested fix:**

Assign and discard explicitly to make the intent structurally visible:
```go
_ = yield(&agent.Event{
    Author:  author,
    Branch:  ic.Branch,
    Actions: agent.Actions{Escalate: true},
}, nil)
// terminal: return regardless (no further yield calls allowed after this point)
```

## What was checked and found clean

- **`FakeClient.Stream` mutex discipline:** `mu` correctly guards both `Requests` append and `next` increment; `CallCount()` and `LastRequest()` both lock. No race on concurrent Stream calls.
- **`ToolCallTurn` loop-variable capture:** uses `c := calls[i]` (explicit copy per iteration), so `&c` on each iteration produces an independent pointer. No alias bug.
- **`CountingAgent.Calls` concurrent access:** each `CountingAgent` instance is run from exactly one goroutine. The `TestParallelAgent_DepthChainBudgetShared_NotFresh` test creates 9 separate instances (one per leaf goroutine), not one shared instance. The post-run read of `leaf.Calls` happens after the errgroup and channel choreography guarantee all goroutines have exited. No data race.
- **`RecordingAgent.SeenBranches`/`Emitted` concurrent access:** `RecordingAgent` is only used in sequential workflow tests, never in `ParallelAgent` tests. No race.
- **`InfiniteToolCallAgent` yield-after-false guard:** all loop-body `yield` calls check the return value and `return` immediately on false. Correctly prevents the Go 1.23+ range-over-func panic.
- **`agenttest` in production binary (`cmd/aura/agent.go`, `swarm_demo.go`, `cache_audit.go`):** intentional design — `aura agent dry-run` and `aura swarm-demo` are operator-facing smoke proofs that deliberately use the mock infrastructure. Treated as a design decision, not a defect.
- **`orDefault` / `selfIfNamed` helpers:** pure functions, no mutation, no race.
- **Go module version:** go.mod has `go 1.25` which includes the loop-variable-capture fix; no phantom loop-variable findings apply.
