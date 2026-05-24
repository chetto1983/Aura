# Phase-STREAM — Stream-Time Parallel Tool Dispatch

**Status:** ⚪ absorbed into Step 1.LAT US-LAT-06; do not execute as-is
**Provenance:** Codex scout (#1 FuturesOrdered, #2 RwLock parallel-friendly, #10 terminal_outcome_reached), online 2026 scout (§1.3 parallel + streaming tool dispatch)
**Estimated effort:** ~1 session
**LOC delta:** ~+200

---

## Why this phase

Aura's loop today: LLM finishes streaming → call `ExecuteToolCalls` → tools fan out in goroutines → join → next iteration. Tools cannot overlap with later parts of the stream.

Codex pattern: push each tool's future into `in_flight: FuturesOrdered` the moment `OutputItemDone` parses a tool call. Tools begin running WHILE the rest of the LLM stream is still arriving. After `Completed`, drain pending tools before next sampling round.

Production reports (online research): **1.4-3.7× wall-clock speedup** on tool-heavy turns. Aura's typical 8-12s turn with 1-3 tool calls expects 1.5-3s saving.

Dependency: ANALYSIS-DEEP.md §2.2 — Phase-STREAM and Web SSE (Phase-CONS US-CONS-07) BOTH restructure `llm.Client.Stream`. Either:
- Land Phase-STREAM FIRST as foundation, then CONS-07 piggybacks on the new event shape.
- Or bundle them. Plan assumes Phase-STREAM lands first (cleaner — agent core change before transport change).

---

## Stories

### US-STREAM-01 — Restructure `llm.Client.Stream` to emit structured events

- **Scope:** Change `llm.Client.Stream` to deliver structured events on a channel rather than buffering whole turn:
  ```go
  type StreamEvent interface{}
  type TokenDelta struct{ Text string }
  type FunctionCallFinalized struct{ ID, Name string; Args json.RawMessage }
  type StreamCompleted struct{ EndTurn *bool; Usage Usage }
  ```
  Internal fragment accumulator stays (callers don't see partial JSON). On `OutputItemDone` for a tool call, emit `FunctionCallFinalized` immediately.
- **Files:** MODIFY [internal/llm/client.go](internal/llm/client.go), [internal/llm/openai.go](internal/llm/openai.go), [internal/llm/types.go](internal/llm/types.go).
- **LOC delta:** +120 / -40 = +80.
- **Acceptance:**
  - `go test ./internal/llm/...` green including new test that asserts `FunctionCallFinalized` arrives BEFORE `StreamCompleted`.
  - All existing consumers (telegram, web buffered, web SSE) still work.
- **Provenance:** Codex `core/src/session/turn.rs:1750-1751`, `stream_events_utils.rs:343-382`.

### US-STREAM-02 — Stream-time tool dispatch in agent loop

- **Scope:** Agent loop reads `StreamEvent` channel. On each `FunctionCallFinalized`, spawn `go func() { resultCh <- e.executeOneTool(...) }()`. Track in `sync.WaitGroup` (Go's `FuturesOrdered` equivalent). On stream close, `wg.Wait()`. Cancellation contract: if user cancels mid-stream, each in-flight tool's ctx must be cancelled.
- **Files:** MODIFY [internal/agent/loop.go](internal/agent/loop.go), [internal/agent/executor.go](internal/agent/executor.go).
- **LOC delta:** +130 / -40 (sync-only logic in executor becomes dead) = +90.
- **Acceptance:**
  - Probe: turn with `search_memory("X") + read_skill("Y")` emitted mid-stream → tools start running BEFORE stream closes (measured via tool-execute timestamps vs LLM final-chunk timestamp).
  - Wall-clock: tool-heavy turn p50 drops 1.5-3s.
  - Cancellation test: user cancels mid-stream → all in-flight tools receive `ctx.Done()`.
- **Provenance:** Codex `core/src/session/turn.rs:1813-1892`, `:1678-1702 drain_in_flight`.
- **Dependency:** US-STREAM-01, Phase-TOOL US-TOOL-05 (`read_only`/`exclusive` flags — the safety primitive for parallel dispatch).

### US-STREAM-03 — `terminal_outcome_reached` atomic for cancel-after-finish

- **Scope:** Each tool task wraps execution with `atomic.Bool` set true on terminal action. On user cancel: check flag first. If true (tool already completed), return the real output. If false, cancel via ctx and synthesize abort message. Prevents losing work when cancel races with tool's last 5ms.
- **Files:** MODIFY [internal/agent/executor.go](internal/agent/executor.go).
- **LOC delta:** +30 + 20 tests.
- **Acceptance:**
  - Test: tool execution finishes 1ms before cancel; result returned as if no cancel.
  - Test: tool execution still running on cancel; abort message returned.
- **Provenance:** Codex `core/src/tools/parallel.rs:99-160`, `registry.rs:566-568`.
- **Note:** Only relevant if US-STREAM-02 ships.

---

## Sequencing

US-STREAM-01 → US-STREAM-02 → US-STREAM-03. Each is one commit.

---

## Risks

- **R1 (US-STREAM-02)**: progressive Telegram edits already rely on text-portion ordering. Tool results may arrive BEFORE stream finishes; UI's "tools_executed" marker must tolerate arrival order. Mitigation: byte-parity test (`internal/channels/telegram/fixture/byte_parity_test.go`) must stay GREEN.
- **R2**: hidden coupling between "independent" tools (e.g. both touch the same external rate-limited resource → 429 cluster). Mitigation: Phase-TOOL US-TOOL-05 flags (`exclusive`) gate this; web_search + web_fetch may need their own per-host throttle (related to Phase-OUT US-OUT-03 repeated-lookup throttle).
- **R3 (US-STREAM-01)**: existing consumers of `llm.Client.Stream` are scattered. Sweep before merging.

---

## Verification

- `go test ./...` green.
- Wall-clock measurement: `cmd/probe_chat` cases with 2+ tool calls; expect p50 drop 1.5-3s.
- Byte-parity test green every commit.
- New test asserts tool execution overlap (measured via timestamps).

---

*Updated 2026-05-21.*
