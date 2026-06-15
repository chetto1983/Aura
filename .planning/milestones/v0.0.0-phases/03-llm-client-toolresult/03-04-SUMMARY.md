---
phase: 03-llm-client-toolresult
plan: 04
subsystem: agent-runtime
tags: [llm-agent, run-loop, otel, tracing, span-id, system-prompt, tool-dispatch, budget, kv-cache]
requires:
  - "internal/agent: Agent interface + InvocationContext + Event + Budget + budget_dedup (Phase 2)"
  - "internal/llm: Client interface + Chunk/Message/ToolCall/ToolDef/Request + Config (Plan 01/02)"
  - "internal/llm/openai_compat: Client.Stream + Usage (Plan 02)"
  - "internal/agent/tools: Registry + Tool + ToolResult + NewResult + WithToolCallContext + TextResponse (Plan 03)"
  - "internal/canonicaljson.Marshal (dedup fingerprint, Phase 2)"
  - "go.opentelemetry.io/otel + sdk + exporters (v1.44.0 train, pinned Plan 01)"
provides:
  - "agent.LlmAgent — budget-gated tool-dispatch run-loop implementing agent.Agent (first real Agent impl)"
  - "agent.SystemPrompt + systemMessage() — byte-stable EN tool-aware system prompt (D-09)"
  - "agent.newTracerProvider (otlp/stdout/none) + full-tree crypto/rand SpanID minting + per-call llm.request span helper"
  - "tools.Registry.RenderToolDefs() []llm.ToolDef — alphabetical-sorted ManifestEntry -> llm.ToolDef mapping"
  - "agenttest.FakeClient — deterministic goleak-clean llm.Client for run-loop tests"
  - "llm.Usage + llm.Chunk.Usage — provider-neutral final token+cost summary on the stream channel"
affects:
  - "Plan 05 (aura chat REPL): drives LlmAgent.Run, renders chunk/tool/final Events, prints cost footer"
  - "Phase 9 swarm: reuses the budget-gated run-loop shape + RenderToolDefs"
tech-stack:
  added: []
  patterns:
    - "budget gate BEFORE each LLM call -> terminal Event (never iter.Seq2 error, D-04)"
    - "full-tree 8-byte crypto/rand SpanID minting (resolves Phase-2 agent.go:51-52 deferral)"
    - "byte-stable system prompt constant, no timestamp, mechanism-not-enumeration (D-08/D-09)"
    - "sequential multi-tool dispatch, RoleTool results in tool_call_id order (D-14)"
    - "error tool-result self-correction; iter.Seq2 error slot reserved for real infra fail (D-15)"
    - "trailing llm.Usage chunk surfaces token+cost through the provider-neutral channel"
key-files:
  created:
    - internal/agent/llm_agent.go
    - internal/agent/llm_agent_events.go
    - internal/agent/llm_agent_test.go
    - internal/agent/tracing.go
    - internal/agent/tracing_test.go
    - internal/agent/prompt.go
    - internal/agent/prompt_test.go
    - internal/agent/main_test.go
    - internal/agent/agenttest/fakeclient.go
    - internal/agent/tools/manifest_test.go
  modified:
    - internal/agent/tools/manifest.go
    - internal/llm/client.go
    - internal/llm/openai_compat/client.go
    - internal/llm/openai_compat/usage.go
  deleted:
    - internal/agent/otel_deps.go
decisions:
  - "otel_deps.go blank-import anchor DELETED in the same commit that introduced tracing.go (the four v1.44.0 modules stay pinned because tracing.go imports them for real)"
  - "Added llm.Usage + llm.Chunk.Usage and a trailing Usage chunk from openai_compat.Stream — the llm.Client interface had no other path to carry the final token+cost summary to the agent's span (blocking-fix, Rule 3)"
  - "Event has no tool_call_id field this phase (AG-UI fan-out is Phase 12); the tool-result correlation lives in the appended RoleTool history message"
  - "Dedup gated at tool-dispatch time via Budget.BeforeToolCall/AfterToolResult with canonicaljson args (caller-canonicalizes contract); a trip is a terminal Event reason 'dedup'"
  - "ConsumeStep reason strings are the REAL 'max_steps'/'wallclock' (NOT the AI-SPEC's 'max_wallclock')"
  - "System prompt names only the stable contract tools (tool_search, text_response) as MECHANISM; it does NOT enumerate volatile builtins (current_time/read_tool_output) — enumeration cache-busts the prefix (D-09)"
metrics:
  duration: ~45min
  completed: 2026-05-30
  tasks: 2
  files: 14
---

# Phase 3 Plan 4: LlmAgent Run-Loop + OTel Tracing + System Prompt Summary

Implemented `LlmAgent` — Aura's first real `Agent`: a budget-gated tool-dispatch
run-loop driving the `llm.Client`, threading `ToolResult` into in-memory history,
streaming chunk/tool-call/tool-result/final Events, terminating via `text_response`
with a content-stop fallback. Wired the real OTel `TracerProvider`
(otlp/stdout/none) replacing the `otel_deps.go` anchor, full-tree crypto/rand
SpanID minting (resolving the Phase-2 deferral), the per-call `llm.request` span,
the byte-stable EN system prompt, and `Registry.RenderToolDefs()`. SPEC Req#9/#10/
#13 + the loop halves of #2/#12/#14 are green under `-race` + goleak, with
`golangci-lint` 0 issues.

## What Was Built

### Task 1 — TracerProvider + SpanID minting + system prompt (commit 7958fce8)

- **`tracing.go`** (112 LOC): `newTracerProvider(ctx, mode, endpoint)` —
  `none` -> no-exporter no-op provider; `stdout` -> `stdouttrace.New()`; default
  `otlp` -> `otlptracegrpc.New(WithEndpoint, WithInsecure)` (silent-drop without a
  collector, never fail-fast, D-05). `mintSpanID()` (8-byte crypto/rand, D-04),
  `rootSpanIDs`/`childSpanIDs` (full-tree chaining), `startLLMSpan`,
  `setSpanAttrs` (model/provider/prompt/completion/cache_hit tokens + request_id;
  NEVER an api_key attr, D-28).
- **`prompt.go`** (25 LOC): `SystemPrompt` constant + `systemMessage()` — one-line
  Aura identity + tool MECHANISM (tool_search to discover, text_response to
  terminate, time via a tool) WITHOUT enumerating volatile builtins, `Always
  respond in Italian` directive, no timestamp (D-08/D-09).
- **`otel_deps.go` DELETED** — same commit as tracing.go, which now imports the
  v1.44.0 train for real (the pin holds via real use).
- **`llm.Usage` + `llm.Chunk.Usage`** and a trailing usage chunk emitted by
  `openai_compat.Stream` so the agent can read the final token+cost summary through
  the provider-neutral `<-chan llm.Chunk` (the interface had no other path).
- **Tests**: `tracing_test.go` (in-memory `tracetest.SpanRecorder`: exactly 1
  `llm.request` span, all six attrs, valid span_id, no secret-shaped attr key;
  none-mode no-op; otlp no-collector does not fail-fast — goleak-clean Shutdown),
  `prompt_test.go` (directive present, no timestamp, mechanism-not-enumeration,
  byte-stable), `main_test.go` (`goleak.VerifyTestMain`).

### Task 2 — LlmAgent run-loop + RenderToolDefs + FakeClient (commit e1693966)

- **`manifest.go`**: `Registry.RenderToolDefs() []llm.ToolDef` reuses the
  alphabetical `Render()` ordering (cache-stability-load-bearing, no re-sort);
  deferred tools fall back to Summary with no Parameters until `tool_search`
  promotes them; non-deferred carry full Description + Parameters.
- **`agenttest/fakeclient.go`** (133 LOC): `FakeClient` scripts per-call chunk
  turns + Stream errors on a pre-closed buffered channel (goleak-clean by
  construction), records every `llm.Request` for immutability + history assertions;
  helpers `ToolCallTurn`/`TextChunks`/`WithUsage`/`MakeToolCall`.
- **`llm_agent.go`** (311 LOC) + **`llm_agent_events.go`** (76 LOC): `LlmAgent`
  implements `agent.Agent`. The loop gates `ConsumeStep` BEFORE each call (trip ->
  terminal Event reason max_steps/wallclock, D-04), starts the span, builds
  `req.Tools = registry.RenderToolDefs()` against a read-only history, streams via
  `client.Stream` (Stream error -> iter.Seq2 error slot, D-15), re-emits chunk/
  tool-call Events, sets span attrs from usage, then: no calls -> terminal answer
  (content-stop fallback + `[risposta troncata: max_tokens]` on length, D-13/D-21);
  calls present -> append the assistant tool-call message, dispatch sequentially
  (D-14) with a per-call dedup gate (`BeforeToolCall`/`AfterToolResult`,
  canonicaljson args), inject `WithToolCallContext` + `Execute` -> `RoleTool`
  preview in `tool_call_id` order, `text_response` -> final Event + return,
  unknown-tool/parse-error -> `RoleTool` error message the loop self-corrects from.
  Events carry a non-zero UTC Timestamp + the minted SpanID.
- **Tests**: `llm_agent_test.go` (the 11-behavior matrix: EventOrder, StepCap,
  WallclockCap injected-clock, DedupWindow, SpanPerCall, MessagesImmutable,
  ToolError + infra-fail, SequentialToolCalls, PrefixStable, SecretRedaction
  release-gate, LengthTruncation), `manifest_test.go` (`TestRenderToolDefs`
  ordering/mapping).

## How To Verify

```
cd /d/Aura   # (WSL: /mnt/d/Aura or /d/Aura depending on mount)
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
go vet ./internal/agent/... ./internal/llm/... && go build ./...
go test ./internal/agent/ ./internal/agent/tools/ -run 'TestLlmAgent_EventOrder|TestLlmAgent_StepCap_Trips|TestLlmAgent_WallclockCap_Trips|TestLlmAgent_DedupWindow_Trips|TestMessagesImmutable|TestLlmAgent_ToolError|TestLlmAgent_SequentialToolCalls|TestPrefixStable|TestLlmAgent_SecretRedaction|TestLlmAgent_LengthTruncation|TestRenderToolDefs|TestSpan_PerCall'
go test -race ./internal/agent/... ./internal/llm/...
golangci-lint run ./internal/agent/... ./internal/llm/...   # 0 issues
```

All green: 12 named acceptance tests + the tracing/prompt suites pass; `-race` +
goleak clean across the agent + llm trees; `golangci-lint` 0 issues; every file
≤600 LOC (largest: llm_agent_test.go 420, llm_agent.go 311).

## Requirement / Threat Coverage

| Item | Where | Test |
|------|-------|------|
| Req#9 (LlmAgent implements Agent, ordered Events, text_response terminal) | llm_agent.go | TestLlmAgent_EventOrder |
| Req#10 (budget gate -> terminal Event, reason) | llm_agent.go | TestLlmAgent_StepCap_Trips / _WallclockCap_Trips / _DedupWindow_Trips |
| Req#13 (1 span/call, stable span_id, message immutability, no api_key attr) | tracing.go, llm_agent.go | TestSpan_LLMRequest / TestSpan_PerCall / TestMessagesImmutable |
| Req#2/#12 loop halves (consume calls + usage) | llm_agent.go consume | TestLlmAgent_SequentialToolCalls, TestSpan_PerCall |
| Req#14 (byte-stable prefix, no timestamp) | prompt.go, llm_agent.go | TestPrompt_* / TestPrefixStable |
| D-04 SpanID minting full-tree | tracing.go | TestMintSpanID |
| D-14 sequential multi-tool dispatch | llm_agent.go dispatch | TestLlmAgent_SequentialToolCalls |
| D-15 error tool-result self-correction; infra-fail error slot | llm_agent.go | TestLlmAgent_ToolError |
| D-21 length truncation notice, no auto-continue | llm_agent.go | TestLlmAgent_LengthTruncation |
| BLOCKER-4 RenderToolDefs mapping | manifest.go | TestRenderToolDefs |
| T-03-01 secret redaction (release-blocking) | llm_agent.go (no api_key attr) | TestLlmAgent_SecretRedaction |
| T-03-10/11 budget DoS + malformed-arg storm | llm_agent.go | StepCap/Dedup + ToolError |
| T-03-12 KV-prefix stability | prompt.go | TestPrefixStable |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Surfaced llm.Usage through the stream channel**
- **Found during:** Task 1 (wiring the per-call span attrs)
- **Issue:** The plan/AI-SPEC loop reads `usage` off the stream (`a.consume(...)`
  returns usage; the span sets `llm.prompt_tokens`/etc), but `llm.Chunk` had NO
  Usage field and `openai_compat.Stream` discarded the captured `parseResult.usage`
  — the `llm.Client` interface had no path to carry the final token+cost summary to
  the agent. Without it the span attrs would always be zero, failing Req#13.
- **Fix:** Added a provider-neutral `llm.Usage` type + `llm.Chunk.Usage` field and
  had `openai_compat.Stream` emit a trailing `llm.Chunk{Usage: ...}` after the SSE
  parse completes (mirroring the real final usage chunk). `usage.go.toLLMUsage()`
  projects the wire usage onto it. The agent never imports `openai_compat`.
- **Files modified:** internal/llm/client.go, internal/llm/openai_compat/client.go,
  internal/llm/openai_compat/usage.go
- **Commit:** 7958fce8
- **Note:** Plan 02's existing openai_compat tests stay green (the extra trailing
  chunk has no key, carries no text/tool, and the assertions are unaffected).

### Plan/reality reconciliations (no behavior deviation)

- **ConsumeStep reason string** is the REAL `"wallclock"` (NOT the AI-SPEC's
  `"max_wallclock"`) — the plan's `<verification>` explicitly flagged this; tests
  assert `"wallclock"`.
- **Event tool_call_id**: `Event` has no `tool_call_id` field this phase (the AI-SPEC
  loop comment implies one). The RoleTool history message carries the correlation
  (`Message.ToolCallID`); the tool-result Event is content-only. AG-UI fan-out is
  Phase 12.
- **prompt.go** describes time-via-tool as a mechanism rather than naming
  `current_time`, to satisfy the mechanism-not-enumeration rule (D-09) the plan's
  own acceptance test enforces.

## Authentication Gates
None. (Live OpenRouter acceptance is Plan 05's manual `scripts/llm_smoke.sh` gate —
this plan is deterministic and network-free, using agenttest.FakeClient.)

## Known Stubs
None. Every code path is wired and tested.

## Self-Check: PASSED
- internal/agent/llm_agent.go — FOUND
- internal/agent/llm_agent_events.go — FOUND
- internal/agent/tracing.go — FOUND
- internal/agent/prompt.go — FOUND
- internal/agent/agenttest/fakeclient.go — FOUND
- internal/agent/tools/manifest.go (RenderToolDefs) — FOUND
- internal/agent/otel_deps.go — DELETED (confirmed gone)
- commit 7958fce8 — FOUND
- commit e1693966 — FOUND
