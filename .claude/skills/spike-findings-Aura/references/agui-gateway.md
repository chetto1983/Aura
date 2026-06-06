# AG-UI Gateway (Phase 12 / Slice 8)

Ground truth for building `internal/agui` against the official AG-UI Go SDK. Everything here was executed live on 2026-06-06 (spikes 014-016) — module pin, full event-surface enumeration, and an HTTP SSE round-trip with real `internal/agent` types. **Four PRD amendments are required before implementation** (listed under Requirements).

## Requirements

- **Pin**: `go get github.com/ag-ui-protocol/ag-ui/sdks/community/go@e9e910b230b9329c905e31ca024b4114dedf7918` → go.mod records the pseudo-version `v0.0.0-20260514093510-e9e910b230b9` (2026-05-14, the "interrupt, resume, multimodal" commit; ≥ amendment #6's floor and HEAD as of 2026-06-06). **Amendment #6's CI grep gate (40-hex SHA in go.mod) can never match** — `require <path> <sha>` is not valid go.mod syntax. Replace with a pseudo-version literal grep. The pin is immutable via go.sum; intent fully served.
- **Resume contract is protocol-native**: `RunAgentInput.Resume []ResumeEntry{InterruptID, Status(resolved|cancelled), Payload}` — supersedes the PRD's "RoleTool answers in messages" design. Map Slice-1.5 `PausedState` → `types.Interrupt{ID: pause-token, Reason: "ask_user"|"tool_call", ToolCallID, ResponseSchema: question-shape-JSON-Schema, ExpiresAt: pause-TTL}`. `ResumeStatusCancelled` gives HITL cancel for free.
- **Outcome literals**: `RunFinishedOutcomeType` is `{success, interrupt}` — the PRD's `interrupted`/`errored` don't exist. Error path = RUN_ERROR event, not an outcome variant.
- **Boundary (ROADMAP)**: `internal/agent` MUST NOT import `internal/agui` — verified live: the spike translator consumed `agent.Event` without touching `internal/agent` (D-17 holds).

## How to Build It

**1. Dependency** (two `go get`s if nothing imports the module yet — first build otherwise fails with `missing go.sum entry` for logrus):

```bash
go get github.com/ag-ui-protocol/ag-ui/sdks/community/go@e9e910b230b9329c905e31ca024b4114dedf7918
go get github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events@v0.0.0-20260514093510-e9e910b230b9
```

Packages: `pkg/core/events` (34 EventType constants, constructors, per-event `Validate()`), `pkg/core/types` (`RunAgentInput`, `Message`, `Interrupt`, `ResumeEntry`, `Tool`, `Context`), `pkg/encoding/sse` (`SSEWriter`), `pkg/client/sse` (client, not needed server-side).

**2. Translator** — pure function over `(*LlmAgent).Run`'s exact signature; the working ~60-LOC core is in `sources/016-agui-sse-roundtrip/main.go` (`translate()`):

```go
func Translate(threadID, runID string, seq iter.Seq2[*agent.Event, error]) iter.Seq2[events.Event, error]
// RUN_STARTED first; per agent.Event:
//   ev.Actions.ToolInvocation start → TOOL_CALL_START (+ TOOL_CALL_ARGS if Arguments != "")
//   ev.Actions.ToolInvocation end   → TOOL_CALL_END + TOOL_CALL_RESULT(msgID, callID, ResultPreview)
//   ev.Actions.StateDelta           → STATE_DELTA []JSONPatchOperation{Op:"replace", Path:"/"+k, Value:v}
//   ev.LLMResponse.Content != ""    → TEXT_MESSAGE_START(WithRole("assistant")) / CONTENT / END
//   err != nil                      → RUN_ERROR, stop
// RUN_FINISHED(WithSuccessOutcome()) last — or WithInterruptOutcome([]types.Interrupt) when
// Actions.AwaitingInput fired (map Question/ToolCallID/Options → Interrupt+ResponseSchema).
```

**3. SSE serving** — the SDK writer does framing, escaping, and per-event flush (`http.ResponseWriter` satisfies its flusher seam):

```go
writer := sse.NewSSEWriter()            // .WithLogger(slog) available
w.Header().Set("Content-Type", "text/event-stream")
for ev, _ := range Translate(in.ThreadID, runID, agentSeq) {
    writer.WriteEventWithType(r.Context(), w, ev, string(ev.Type()))  // emits `event: TYPE` line
}
```

`WriteEventWithType` produces the PRD smoke framing exactly: `event: TYPE` + `id: TYPE_<ms>` + `data: {json}` + blank line. The `id:` line is automatic — a free `Last-Event-ID` reconnect hook.

**4. Input parsing** — don't write a parser; `types.RunAgentInput` ships with hand-written `UnmarshalJSON` accepting **both camelCase and snake_case** on every field. Keep only Aura-semantic validation (threadId is an existing conversation UUID, messages-non-empty policy). The PRD's `types.go` budget halves (~80 → ~40 LOC).

**5. Golden fixtures** — `sources/015-agui-event-surface/golden-events.json` holds the verified wire shape of all 21 events Aura emits; seed `translator_test.go` property-based fixtures from it.

## What to Avoid

- **Don't implement the #6 CI gate as written** — 0 matches forever, falsely red. Pseudo-version grep instead.
- **Don't emit empty deltas**: `NewTextMessageContentEvent(id, "").Validate()` → error ("delta field must not be empty"). Real LLM streams produce empty deltas; the translator must skip them.
- **Don't pass empty IDs**: `toolCallId`/`messageId` are `Validate()`-required. Translator owns ID generation (PRD's `IDGenerator` interface).
- **Don't use THINKING_\* events** — deprecated aliases at this pin; REASONING_\* is canonical (amendment #33's exact names exist: `REASONING_START`/`REASONING_MESSAGE_CONTENT`/`REASONING_END`, plus MESSAGE_START/END/CHUNK/ENCRYPTED_VALUE).
- **Don't compare full-sequence goldens over multi-key StateDelta maps** without sorting keys — map iteration order is non-deterministic.
- **Don't assume `event:` framing is the protocol default** — plain `WriteEvent` is data-only (TS-SDK default; type lives inside the JSON). `event:` lines are additive and harmless to data-only parsers, but re-verify Dojo client compat at 8b time.

## Constraints

- SDK module: `github.com/ag-ui-protocol/ag-ui/sdks/community/go`, go directive 1.24.4 (Aura 1.26.4 fine). No version tags → pseudo-version is the only possible resolution, forever (until upstream tags `sdks/community/go/vX.Y.Z`).
- Deps it brings: `google/uuid v1.6.0` (already Aura's, shared), `sirupsen/logrus v1.9.3` (imported by `core/events/decoder.go` — links into the binary even though Aura's emit path never logs through it; the SSE writer itself uses slog).
- Event surface at this pin: 28 active types + 5 deprecated THINKING_\* + UNKNOWN. PRD's "~17-25" is an undercount. Includes post-PRD ACTIVITY_SNAPSHOT/DELTA (not needed for Slice 8).
- All constructors auto-attach `timestamp` (ms epoch). `TOOL_CALL_RESULT` auto-injects `"role":"tool"`.
- `STATE_DELTA` validates RFC 6902 op names at `Validate()` time.
- No cross-event sequence validator in the SDK — per-event `Validate()` only; the PRD's property-based sequence tests remain Aura's job.
- Measured loopback floor (Windows, 13-event stream, no LLM): first-byte ~35-38ms, total ~40ms — consistent with PRD OQ#4's 50-150ms `--via-agui` overhead estimate.

## Origin

Synthesized from spikes: 014, 015, 016 (session 4, 2026-06-06)
Source files available in: sources/014-agui-sdk-module-pin/, sources/015-agui-event-surface/, sources/016-agui-sse-roundtrip/
