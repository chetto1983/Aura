---
spike: 015
name: agui-event-surface
type: standard
validates: "Given the pinned SDK, when core/events is enumerated against the PRD Slice-8 acceptance list, then every required event exists with conformant JSON wire shape — gaps become amendments"
verdict: VALIDATED
related: [014-agui-sdk-module-pin, 016-agui-sse-roundtrip]
tags: [agui, events, wire-format, phase-12]
---

# Spike 015: AG-UI Go SDK — event surface vs PRD acceptance

## What This Validates

Given the SDK pinned at `e9e910b` (spike 014), when `core/events` + `core/types` are enumerated against the PRD Slice-8 acceptance list (RUN_\*, STEP_\*, TEXT_MESSAGE_\*, TOOL_CALL_\*+RESULT, MESSAGES/STATE_SNAPSHOT, STATE_DELTA, REASONING_\* amendment #33, interrupt/resume for Slice 1.5 multi-pause), then every required event constructs, `Validate()`s and marshals to a conformant wire shape — and every PRD-vs-SDK divergence is documented as amendment material.

## Research

Source-level enumeration of the module cache (`pkg/core/events/*.go`, `pkg/core/types/types.go`):

- **34 EventType constants**: 28 active (PRD says "~17-25" — undercount), 5 deprecated THINKING_\* aliases, 1 UNKNOWN. Active families: TEXT_MESSAGE (4 incl. CHUNK), TOOL_CALL (5 incl. CHUNK+RESULT), STATE (2), MESSAGES_SNAPSHOT, ACTIVITY (2, new), RAW, CUSTOM, RUN (3), STEP (2), REASONING (7 incl. CHUNK + ENCRYPTED_VALUE).
- **REASONING_\* is the canonical family** at this pin; THINKING_\* survives only as deprecated constants. Amendment #33's exact names (`REASONING_START/MESSAGE_CONTENT/END`) all exist.
- `types.RunAgentInput` is **provided** (ThreadID, RunID, ParentRunID, State, Messages, Tools, Context, ForwardedProps, `Resume []ResumeEntry`) with hand-written `UnmarshalJSON` accepting **both camelCase and snake_case** for every field.
- `types.Interrupt`: ID, Reason, Message, ToolCallID, **ResponseSchema (JSON Schema of the expected answer)**, ExpiresAt, Metadata. `types.ResumeEntry`: InterruptID, Status (`resolved`/`cancelled`), Payload.
- `events.NewEventDecoder(logger *logrus.Logger)` — the logrus import lives here (client-side decode path, not on Aura's emit path).
- No cross-event sequence validator in the SDK — per-event `Validate()` only. The PRD's property-based "sequence correctness" tests stay Aura's responsibility.

## How to Run

```bash
# dependency in go.mod first (see spike 014), then:
go run -tags spike_agui ./.planning/spikes/015-agui-event-surface
```

## What to Expect

21 `[WIRE]` lines (one golden JSON per PRD-required event), `[INPUT]` camelCase + snake_case/resume parse assertions, `[OUTCOME]` divergence probe, `golden-events.json` artifact, `[SUMMARY] VALIDATED`, exit 0.

## Investigation Trail

1. Enumerated the const block: found BOTH `reasoning_events.go` and `thinking_events.go` → resolved as canonical-vs-deprecated (doc comments explicit). #33 satisfied natively.
2. Read `run_events.go`: `RunFinishedOutcome{Type, Interrupts}` exists with `WithSuccessOutcome()`/`WithInterruptOutcome([]types.Interrupt)` helpers — but `RunFinishedOutcomeType` has only `success` and `interrupt`. PRD acceptance says `outcome.type ∈ {success, interrupted, errored}`: **two of three PRD literals don't exist** (`interrupted` → SDK `interrupt`; `errored` → not an outcome at all, the error path is the RUN_ERROR event).
3. Read `types.go`: discovered the full first-class interrupt/resume input contract (the 2026-05-14 commit's whole point). The PRD resume contract ("client re-POSTs with RoleTool answers matching tool_call_id in messages") predates this — the protocol now has a dedicated `resume[]` array keyed by `interruptId`.
4. Harness: constructed all 21 PRD-required events → every `Validate()` passed, every marshal produced spec-conformant camelCase JSON. `TOOL_CALL_RESULT` auto-injects `"role":"tool"`. `STATE_DELTA` validates RFC 6902 op names at `Validate()` time.
5. Parsed the PRD smoke curl payload verbatim with `types.RunAgentInput` → fields land correctly; snake_case variant + `resume[{interrupt_id, status, payload}]` also parses.

## Results

**VALIDATED ✓** — full coverage, zero missing events; four PRD amendments emerge:

1. **Outcome literals (PRD §Slice 8 acceptance)**: `outcome.type ∈ {success, interrupt}` (not `interrupted`); `errored` is expressed by the RUN_ERROR event, not an outcome variant.
2. **Resume contract (PRD §Slice 8 acceptance)**: supersede the "RoleTool answers in messages" design with the protocol-native `RunAgentInput.Resume []ResumeEntry` (`interruptId`/`status`/`payload`). Mapping for Slice 1.5: `PausedState` → `types.Interrupt{ID: pause-token, Reason: "ask_user"|"tool_call", ToolCallID, ResponseSchema: question-shape}`; `ResumeBatch(answers)` keyed by `InterruptID`. `ResumeStatusCancelled` gives HITL-cancel for free. `ExpiresAt` maps to pause TTL if Aura wants it.
3. **`internal/agui/types.go` shrinks**: `RunAgentInput` parser+validation is SDK-provided (incl. dual-case tolerance). Aura keeps only semantic validation (threadId is a conversation UUID that exists, messages non-empty policy) — budget ~80 → ~40 LOC.
4. **Event-count language**: "~17-25 event types" → 28 active types; the translator's property-based test matrix should target the 21 Aura emits (golden set in `golden-events.json`).

Surprises: `timestamp` (ms epoch) auto-attached by all constructors; ACTIVITY_SNAPSHOT/DELTA family exists (post-PRD protocol addition, not needed by Slice 8); `Message.EncryptedContent`/`EncryptedValue` + REASONING_ENCRYPTED_VALUE exist for reasoning-state continuity (irrelevant for DeepSeek/vLLM plain-text reasoning, ignore).
