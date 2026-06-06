---
spike: 016
name: agui-sse-roundtrip
type: standard
validates: "Given a synthetic iter.Seq2[*agent.Event, error] sequence, when a mini-translator maps it to SDK events served over POST /agent/run SSE, then the client reads the PRD smoke sequence verbatim — validates the −100-LOC design and the ~600-LOC budget"
verdict: VALIDATED
related: [014-agui-sdk-module-pin, 015-agui-event-surface]
tags: [agui, sse, translator, phase-12]
---

# Spike 016: AG-UI SSE round-trip — the −100-LOC translator design, live

## What This Validates

Given a synthetic `iter.Seq2[*agent.Event, error]` (the exact `(*LlmAgent).Run` signature, real `internal/agent` types — D-17's "fan-out adapter, not a refactor" claim), when a pure mini-translator maps it 1:N to SDK events and the SDK's `SSEWriter` serves them over a real HTTP `POST /agent/run`, then an HTTP client reads back the PRD smoke event sequence verbatim with intact payloads. No LLM calls.

## Research

- `pkg/encoding/sse.SSEWriter`: `WriteEvent` emits **data-only** frames (TS-SDK default); `WriteEventWithType` adds the `event: TYPE` line the PRD smoke shows. Both auto-emit `id: TYPE_<ms-timestamp>` (enables SSE `Last-Event-ID` resume semantics for free) and auto-flush per event (`http.ResponseWriter` matches the no-error `Flush()` interface). Newlines in JSON are escaped to keep frame integrity. Uses **slog** (`WithLogger`), unlike the logrus-using decoder.
- `internal/agent/event.go`: Phase-2 shipped `Event` with ThreadID/MessageID/ToolCallID forward-compat fields explicitly for this gateway; `Actions.ToolInvocation` start/end carries ToolCallID/ToolName/Arguments/ResultPreview → maps 1:1 to TOOL_CALL_START/ARGS/END/RESULT; `Actions.StateDelta` → STATE_DELTA JSONPatch; `Actions.AwaitingInput` → `types.Interrupt` (proven in 015).

## How to Run

```bash
# dependency in go.mod first (see spike 014), then:
go run -tags spike_agui ./.planning/spikes/016-agui-sse-roundtrip
```

## What to Expect

`[FRAME]` lines showing every SSE frame (`event:`/`id:`/`data:`), `[SEQUENCE] 13/13`, `[PAYLOAD]` delta integrity, `[LATENCY]` loopback floor, two `[EDGE]` probes, `[SUMMARY] VALIDATED`, exit 0.

## Observability

Forensic log prints the raw SSE wire byte-for-byte (the artifact IS the stream); assertions run on the parsed frames, never on log text.

## Investigation Trail

1. Read `SSEWriter` source: discovered the dual framing (`WriteEvent` vs `WriteEventWithType`) — the PRD smoke's `event: TYPE` framing is a **choice**, not the SDK default. Chose `WriteEventWithType` to match the PRD; flagged for discuss-phase (AG-UI TS clients parse data-only frames; the `event:` line is additive and harmless to them, but Dojo-compat should be re-verified at 8b time).
2. Built the mini-translator (~60 LOC pure function): RUN_STARTED → per-event mapping (LLMResponse.Content → TEXT_MESSAGE triple; ToolInvocation start/end → TOOL_CALL quadruple; StateDelta map → JSONPatch ops) → RUN_FINISHED(success). Compiled against real `internal/agent` types without touching them — D-17 holds.
3. First run: 13/13 events in PRD order, deltas intact (`"Ciao"`, `"A Caraglio: 12°C sereno."`), UTF-8 (`°`) survives, `Content-Type: text/event-stream`, per-event flush observed.
4. Edge probes: `NewTextMessageContentEvent("msg-x", "").Validate()` → **REJECTED** ("delta field must not be empty"); `NewToolCallStartEvent("", "web_search").Validate()` → **REJECTED** ("toolCallId field is required").

## Results

**VALIDATED ✓** — the Slice-8 design works end-to-end exactly as the PRD draws it.

- `iter.Seq2 → translate → SSEWriter → HTTP` round-trip: 13/13 events, PRD order, payloads intact. The translator is a pure function consuming `Run()`'s signature directly — no Emitter interface, no channel plumbing. The −100-LOC saving is real; the spike translator is ~60 LOC for the core mapping.
- Wire framing: `event: TYPE` + `id: TYPE_<ts>` + `data: {json}` via `WriteEventWithType`. The `id:` line is SDK-automatic — free Last-Event-ID hook for future reconnect support.
- Loopback floor (Windows, no LLM): first-byte ~35-38ms, 13-event stream total ~37-40ms — consistent with the PRD OQ#4 estimate of 50-150ms overhead for `--via-agui` mode.
- **Translator obligations** (for PLAN.md): (1) skip empty LLM deltas — SDK `Validate()` rejects them; (2) generate/guarantee non-empty IDs — `toolCallId`/`messageId` are required; (3) `STATE_DELTA` map iteration order is non-deterministic for multi-key deltas — sort keys if golden tests compare full sequences.
- Server-side total surface in the spike: handler ≈ 25 LOC. The PRD's `server.go` ~200 LOC budget (backpressure, CORS, drain) is comfortable.
