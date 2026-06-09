# Audit: internal/agent/agenttest

**Verdict:** needs-work — three findings, no critical defects; one not-wired / architectural concern ships in the production binary.

**Counts:** critical 0 / high 1 / medium 1 / low 2

---

## Findings

### [HIGH][NOT-WIRED] Test-helper package imported unconditionally by production binary

**Location:** `cmd/aura/agent.go:26`, `cmd/aura/swarm_demo.go:22`, `cmd/aura/cache_audit.go:25`
**Confidence:** high

`internal/agent/agenttest` carries no `//go:build` constraint and is imported by three non-`_test.go` files in `cmd/aura`. The production `aura` binary therefore ships with `FakeClient`, all four mock agents (`InfiniteToolCallAgent`, `EmitNThenEscalate`, `RecordingAgent`, `CountingAgent`), and their supporting helpers. This is an intentional design (operator-facing dry-run / swarm-demo / cache-audit commands), but the consequence is that test mock code is compiled into every production release. The three callers themselves are not guarded by any build tag.

**Suggested fix:** Add `//go:build dev` (or a dedicated `diag` tag) to `cmd/aura/agent.go`, `cmd/aura/swarm_demo.go`, and `cmd/aura/cache_audit.go`, and update the Makefile to build the diagnostic binary with that tag. This keeps all agenttest references out of the release binary without removing the diagnostic commands from the developer workflow.

---

### [MEDIUM][BUG] `FakeClient.Stream` returns a silent no-content response when the script is exhausted

**Location:** `internal/agent/agenttest/fakeclient.go:62-65`
**Confidence:** high

```go
var turn FakeTurn
if f.next < len(f.Turns) {
    turn = f.Turns[f.next]
}
f.next++
```

When `f.next >= len(f.Turns)` the zero `FakeTurn` is used: no error, no chunks. The returned channel is a pre-closed empty channel, which the agent loop interprets as an LLM response with no content and no tool calls. If a test overshoots its script (e.g. an unexpected retry or finalize call consumes an extra turn), the agent silently sees an empty completion rather than a test failure. This can make a broken loop appear to pass: the agent's empty-completion recovery code (`maybeRecover`) kicks in, consuming an additional scripted turn, and the whole cascade shifts by one turn with no diagnostic signal.

**Suggested fix:** Change the out-of-bounds path to return an explicit sentinel error (e.g. `fmt.Errorf("FakeClient: script exhausted after %d calls", f.next)`) so an over-calling test fails fast with a clear message, not with silent fallback behaviour.

---

### [LOW][BUG] `RecordingAgent.Emitted` and the yielded pointer alias the same struct

**Location:** `internal/agent/agenttest/mocks.go:151-152`
**Confidence:** medium

```go
a.Emitted = append(a.Emitted, &ev)
if !yield(&ev, nil) {
```

Both `a.Emitted[i]` and the pointer delivered to the consumer through `yield` point to the same `ev` struct. If any consumer calls a mutating method on the received `*agent.Event` (e.g. `SetAuthorIfEmpty`, or any future enrichment pass), the `Emitted` record is silently corrupted. No current test triggers this — all current consumers read, not write — but the invariant "Emitted is a snapshot of what was sent" is broken by design.

**Suggested fix:** Append a fresh pointer so that `Emitted` and the yielded pointer are independent:

```go
snapshot := ev      // copy, not alias
a.Emitted = append(a.Emitted, &snapshot)
if !yield(&ev, nil) {
```

---

### [LOW][BUG] `FakeClient.Stream` shallow-copies `Messages` but not nested `ToolCalls` slices

**Location:** `internal/agent/agenttest/fakeclient.go:57-59`
**Confidence:** low

```go
snap := req
snap.Messages = append([]llm.Message(nil), req.Messages...)
```

Each `llm.Message` struct is copied by value, giving each message its own `Role`, `Content`, and `ToolCallID` strings. However, `Message.ToolCalls []ToolCall` is a slice header: the copy shares the same backing array as the original slice. If a caller appended to an existing message's `ToolCalls` slice in place (sharing the backing array), the snapshot would observe the mutation. In current code the agent always constructs new `llm.Message` values with new `ToolCalls` slices on each `append(a.history, llm.Message{ToolCalls: calls})` call, so no backing array is shared. The risk is latent: a future refactor that reuses and mutates an existing `ToolCalls` slice would silently corrupt recorded snapshots.

**Suggested fix:** Deep-copy `ToolCalls` in the snapshot loop if immutability of `Requests` across mutation needs to be guaranteed. Alternatively, document the current invariant in a comment so future refactors know not to mutate existing `ToolCalls` slices in history.

---

## What was checked and found clean

- **Goroutine safety of `FakeClient`**: `mu sync.Mutex` guards all state mutations (`Turns`, `Requests`, `next`). `Stream`, `CallCount`, and `LastRequest` all lock correctly. No double-unlock, no missing lock path, no goroutine spawned inside `Stream`.
- **`ToolCallTurn` loop-variable capture**: uses `for i := range calls { c := calls[i]; chunks = append(chunks, llm.Chunk{ToolCall: &c}) }`. Under Go 1.26 (go.mod: `go 1.26.4`) `c` is per-iteration, so `&c` is unique per step.
- **`WithUsage` backing-array overlap**: `TextChunks` creates a slice with capacity exactly `len(parts)+1`, filled to capacity; `WithUsage` receives a value copy of the turn, and `append` allocates a new backing array because spare capacity is zero. No aliasing.
- **`EmitNThenEscalate` final yield**: the terminal escalate event is yielded without checking the return value (documented as "terminal event — return regardless"). This matches the D-22 footgun 2 contract: the mock exits after the yield regardless, so the "continued iteration after false return" panic cannot fire.
- **`CountingAgent.Calls` concurrent read**: `Calls` is written in `runSub` goroutines (one goroutine per `CountingAgent` instance — each instance is owned by exactly one goroutine) and read in test code after `drain()` returns. `drain()` waits for the `results` channel to close, which happens only after `eg.Wait()` (all goroutines finished), establishing the happens-before relationship. No race.
- **`RecordingAgent` concurrent use**: `RecordingAgent` is never placed as a sub-agent of a `ParallelAgent` in any test in the repo (verified by grep). Its mutable fields (`SeenBranches`, `Emitted`) are only written in sequential contexts.
- **`orDefault` and `selfIfNamed` dead code**: both are used in 8 and 4 places respectively within `mocks.go`. Not dead.
- **`FakeClient.ctx` unused parameter**: `ctx context.Context` is accepted but not used. The channel is pre-buffered and immediately closed so no blocking and no goroutine is spawned; the documentation correctly states "goleak-clean by construction". Ignoring ctx in a synchronous fake is safe and intentional.
- **Race detector**: `go test -race ./internal/agent/...` including `agenttest`, `workflow`, and all dependent packages — passes clean.
