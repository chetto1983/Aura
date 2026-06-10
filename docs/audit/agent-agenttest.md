# Audit: internal/agent/agenttest

**Verdict:** needs-work — one dead exported field and one shallow-copy gap in the snapshot helper.
**Counts:** critical 0 / high 0 / medium 1 / low 1

## Findings

### [MEDIUM][dead-code] `RecordingAgent.Emitted` is written but never read

**Location:** `internal/agent/agenttest/mocks.go:129,151`
**Confidence:** high

`RecordingAgent` declares an exported `Emitted []*agent.Event` field (line 129) and appends to it inside the closure returned by `Run` (line 151). A grep across the entire repo (`D:/Aura`) for `\.Emitted\b` in `*.go` files finds exactly two hits: the declaration and the append — no consumer ever reads this field in test or production code.

The field was presumably added to let callers assert which events were emitted in which order, but `SeenBranches` already covers branch-label assertions and the drain helpers in the test files collect events directly from the iterator. `Emitted` is therefore dead weight: it allocates heap pointers on every `Run` invocation and provides no observable value.

**Suggested fix:** Remove the `Emitted` field and the `a.Emitted = append(...)` line. If order assertions are needed, tests should collect directly from the drain loop or use `SeenBranches`.

---

### [LOW][bug] `FakeClient.Stream` shallow-copies `Messages` but not `Message.ToolCalls`

**Location:** `internal/agent/agenttest/fakeclient.go:57-59`
**Confidence:** medium

```go
snap := req
snap.Messages = append([]llm.Message(nil), req.Messages...)
f.Requests = append(f.Requests, snap)
```

The code creates a new `[]llm.Message` slice and copies each `Message` value into it. Because `Message` is a value type, scalar fields (`Role`, `Content`, `ToolCallID`, `Name`) are correctly isolated. However, `Message.ToolCalls []llm.ToolCall` is a slice header: the copied `Message` value holds the same underlying array pointer as the original. If a caller later appends to or overwrites elements in a `ToolCalls` slice that was shared with the recorded snapshot, the snapshot silently reflects the mutation.

In current production code (`LlmAgent.Run`) the agent always creates new `ToolCalls` slices when it appends to history (e.g., `llm.Message{Role: llm.RoleAssistant, ToolCalls: calls}`) so the risk does not fire today. The danger grows as more callers use `FakeClient` for multi-turn tests that reuse the same `Message` values across calls. Tests in `internal/agent/llm_agent_wire_validity_test.go` and `cmd/aura/cache_audit.go` already introspect deep into recorded messages, making this a latent immutability hazard.

**Suggested fix:** Deep-clone `ToolCalls` inside the loop:

```go
msgs := make([]llm.Message, len(req.Messages))
for i, m := range req.Messages {
    if len(m.ToolCalls) > 0 {
        tc := make([]llm.ToolCall, len(m.ToolCalls))
        copy(tc, m.ToolCalls)
        m.ToolCalls = tc
    }
    msgs[i] = m
}
snap.Messages = msgs
```

## Clean checks performed (no findings)

- **`FakeClient.Stream` exhausted-script fallback:** Returns a closed empty channel (capacity 1, no chunks). Loop callers terminate cleanly. Correct by design; the wasted 1-slot allocation is negligible.
- **`FakeClient` mutex coverage:** `Stream`, `CallCount`, and `LastRequest` all hold `f.mu` before touching `f.next` / `f.Requests`. Exported fields `Turns` and `Requests` are accessed by tests only after agent execution completes (happens-before via channel drain), so no race.
- **`ToolCallTurn` loop-variable aliasing:** Correctly copies `c := calls[i]` before storing `&c`. Safe on all Go versions including pre-1.22.
- **`CountingAgent.Calls` concurrent access:** Each `CountingAgent` instance is passed to exactly one goroutine in all current parallel tests. The `drain()` call provides a proper happens-before via errgroup + channel close before any test reads `leaf.Calls`. No race under current usage.
- **`RecordingAgent.SeenBranches` mutation:** Only written from inside the closure returned by `Run`. `RecordingAgent` is never passed to `workflow.NewParallel` in any test, so no concurrent writes. Read-after-drain is safely sequenced.
- **`EmitNThenEscalate.Run` final-yield semantics:** The terminal `yield(...)` result is correctly discarded (the function body ends naturally). The `for range a.N` exit before the terminal yield correctly guards the D-22 footgun.
- **`InfiniteToolCallAgent` production use:** Used in `cmd/aura/agent.go` for the `dry-run` subcommand. Not dead code.
- **`orDefault` / `selfIfNamed` helpers:** Unexported, referenced by all four mock types. Not dead code.
- **Context propagation in `FakeClient.Stream`:** `ctx` is intentionally unused — the channel is pre-populated and fully buffered; no goroutine is spawned and there is nothing to cancel. Acceptable for a synchronous test fake.
