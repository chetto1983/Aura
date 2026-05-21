# Phase-CONS — Web ↔ Telegram 1+1 Consolidation

**Status:** 🟡 queued after Phase-OUT
**Provenance:** web-telegram-consolidation scout #7 (full deliverable `docs/research-2026-05-21/web-telegram-consolidation.md`)
**Estimated effort:** ~3 sessions
**LOC delta:** net -90 (dedup -810 + parity additions +720)

---

## Why this phase

User mandate verbatim (2026-05-21): "we need consolidate 1 inbound and 1 outbound for all web and telegram no duplication and must have same level".

Today there are TWO InvocationBuilders (`internal/channels/telegram/invocation_builder.go` 688 LOC + `cmd/aura/web_chat.go` 612 LOC, both violate the 600-LOC rule) and THREE `chat.Hub` instances (telegram, web, cron). Web is functionally degraded in 7 features: streaming, voice, markdown render, soft-budget, context compaction, `tools_allowed`, `tools_used` (only `tools_used` works on both today).

**CONS-01 lives in Phase-BUG** because it's the live bug fix (web missing overlays); the stories in this phase are CONS-02..08.

---

## Stories

### US-CONS-02 — Collapse `webChatSessions` + `webAgentState` onto `agent.SessionStore` + `conversation.Context`

- **Scope:** Replace [cmd/aura/web_chat.go](cmd/aura/web_chat.go)'s `webChatSessions` (bespoke `map[string][]llm.Message` with idle eviction) with calls to the shared `agent.SessionStore` (key: `web:<userID>:<threadID>`). Replace `webAgentState` (flat `[]llm.Message` shim, no TrackTokens, no compaction) with `conversation.Context` so web gets the full conversation discipline for free.
- **Files:** MODIFY [cmd/aura/web_chat.go](cmd/aura/web_chat.go) (remove webChatSessions, webAgentState, trimWebMessages); MODIFY [internal/channels/web/chat_service_test.go](internal/channels/web/chat_service_test.go).
- **LOC delta:** +60 / -240 = -180.
- **Acceptance:**
  - `go test ./internal/channels/web/... ./cmd/aura/...` green.
  - Probe: 3 turns same thread_id; turn 3 prompt includes context from turns 1+2; `convCtx.TotalTokensUsed()` accurately tracks.
  - Probe: huge tool result triggers `CompactCompletedToolResults`; round N+1 prompt has tool_result ≤1200 chars.
- **Dependency:** Phase-BUG US-BUG-01 (CONS-01) — shared `composeAgentPrompt` exists.

### US-CONS-03 — Collapse `webToolExecutor` onto `agent.ExecuteToolCalls`

- **Scope:** Delete `webToolExecutor.ExecuteToolCalls` (bespoke goroutine-per-call). Replace with direct call to `agent.ExecuteToolCalls(ctx, deps.Tools, convCtx, userID, conversationID, calls, terminalPolicyEnabled, logger, opts...)`. Add missing `WithToolAttemptRecording` + `WithTokenJuice` options.
- **Files:** MODIFY [cmd/aura/web_chat.go](cmd/aura/web_chat.go) (delete webToolExecutor + helpers); MODIFY [cmd/aura/web_chat_test.go](cmd/aura/web_chat_test.go).
- **LOC delta:** +30 / -180 = -150.
- **Acceptance:**
  - `go test ./internal/agent/... ./cmd/aura/...` green.
  - Probe parity: QA-phase suite end-to-end on both transports; identical failure classes and `tools_used[]`.
- **Dependency:** US-CONS-02.

### US-CONS-04 — Promote `agentcore.Builder` to own full `agent.Invocation` construction

- **Scope:** Move `agent.Invocation` construction (Options, ToolsProvider, BeforeTool/BeforeLLM/RecordUsage/EstimateCost/TerminalHandler, PhantomToolGuard, PostTurn, MaxIterations) out of both InvocationBuilders into `agentcore.Builder.Build`. Transports supply only `PerTurnHooks{ChatClient, OnPlaceholderReady, OnStreamChunk, OnFinal, OnAskUser, OnSoftBudget}`.
- **Files:** NEW [internal/agentcore/invocation.go](internal/agentcore/invocation.go); MODIFY [internal/channels/telegram/invocation_builder.go](internal/channels/telegram/invocation_builder.go) (shrink to hooks); MODIFY [cmd/aura/web_chat.go](cmd/aura/web_chat.go) (shrink to hooks); MODIFY [internal/channels/telegram/chat_client.go](internal/channels/telegram/chat_client.go); NEW [cmd/aura/web_chat_hooks.go](cmd/aura/web_chat_hooks.go).
- **LOC delta:** +420 / -780 = **-360 LOC** (largest single commit in phase).
- **Acceptance:**
  - `go test ./...` green including `internal/channels/telegram/fixture/byte_parity_test.go`.
  - `golangci-lint run ./...` no new findings on touched files.
  - `dupl -t 60 internal/agentcore/ internal/channels/ cmd/aura/web_chat*.go` no cluster ≥60 tokens.
  - Every touched file ≤600 LOC.
- **Dependency:** US-CONS-03.
- **Risk mitigation:** **Feature-flag** `AURA_AGENTCORE_BUILDER=true` for 1 week of live traffic before deleting legacy. Add transcript-comparison probe: same query through both paths → identical tool-call sequence (names + arg_keys); response text may vary ≤5% (LLM nondeterminism).

### US-CONS-05 — Single Hub: collapse web + telegram Hubs into one

- **Scope:** Move Hub construction from `internal/channels/telegram/invocation_builder.go::NewHub` AND `cmd/aura/web_chat.go::newHubBackedWebChatService` into `cmd/aura/app.go` (composition root). Register both inbound (`telegramadapter.New()` + new `webadapter.New()`) and both outbound (`telegramadapter.Outbound` + `webadapter.Router`) on the SAME Hub. Cron Hub stays separate.
- **Files:** MODIFY [cmd/aura/app.go](cmd/aura/app.go); MODIFY [cmd/aura/app_wire.go](cmd/aura/app_wire.go); MODIFY [internal/channels/telegram/invocation_builder.go](internal/channels/telegram/invocation_builder.go) (delete `NewHub`); MODIFY [cmd/aura/web_chat.go](cmd/aura/web_chat.go) (delete Hub construction, take injected `*chat.Hub`); NEW [internal/channels/web/inbound.go](internal/channels/web/inbound.go).
- **LOC delta:** +120 / -180 = -60.
- **Acceptance:**
  - `go test ./... -run TestHub` green.
  - Hub round-trip test: ChannelTelegram + ChannelWeb run dispatches stay channel-scoped.
  - Add test: ChannelWeb run dispatches ONLY to web outbound (zero deliveries to telegram outbound).
- **Dependency:** US-CONS-04.

### US-CONS-06 — Feature parity wave 1: soft-budget + context compaction + archive on web

- **Scope:** Wire missing telegram-side post-turn features onto web via the shared `agentcore.Build` (already inside Build after CONS-04). Add `BudgetWarning string` to `ChatReply`. Add `OnSoftBudget` hook that writes `budget_warning` into ChatReply. Conversation archive append now runs for web turns.
- **Files:** MODIFY [internal/api/chat.go](internal/api/chat.go), [internal/api/types.go](internal/api/types.go); MODIFY [internal/channels/web/chat_service.go](internal/channels/web/chat_service.go), [internal/channels/web/outbound.go](internal/channels/web/outbound.go); MODIFY [cmd/aura/web_chat_hooks.go](cmd/aura/web_chat_hooks.go); MODIFY [web/src/api/chat.ts](web/src/api/chat.ts) (read budget_warning — follow-up).
- **LOC delta:** +120 (additive feature).
- **Acceptance:**
  - Probe: induce soft_budget (mock budget runtime) → `ChatReply.budget_warning` non-empty.
  - Probe: `conversations` SQLite table has rows tagged `channel=web` after web turns.
- **Dependency:** US-CONS-05.

### US-CONS-07 — Feature parity wave 2: streaming on web (SSE)

- **Scope:** New `internal/channels/web/streaming_outbound.go` registers `(ChannelWeb, DeliveryModeStreaming)`. New `POST /chat/stream` (SSE) endpoint flushes one SSE frame per `chat.OutboundEvent`. Web ChatClient becomes `NewStreamingChatClient` that flushes `llm.Token` chunks. Buffered `POST /chat` stays for non-stream clients.
- **Files:** NEW [internal/channels/web/streaming_outbound.go](internal/channels/web/streaming_outbound.go); NEW [internal/channels/web/sse_chat_client.go](internal/channels/web/sse_chat_client.go); MODIFY [internal/api/router.go](internal/api/router.go), [internal/api/chat.go](internal/api/chat.go); MODIFY [cmd/aura/web_chat_hooks.go](cmd/aura/web_chat_hooks.go).
- **LOC delta:** +280.
- **Acceptance:**
  - `go test ./internal/channels/web/...` green with SSE-frame assertion test.
  - `curl -N` against `/api/chat/stream` produces stream of `data: {...}\n\n` frames.
  - First byte <500ms; final byte p95 ≤8s on cached `wiki_subgraph` query.
  - Header: `X-Accel-Buffering: no` emitted (nginx-proxy compat).
- **Dependency:** US-CONS-06 + Phase-STREAM (parallel dispatch shares the same `llm.Client.Stream` restructure — bundle if possible).

### US-CONS-08 — Feature parity wave 3: voice + ask_user on web

- **Scope:** TTS synthesis on web as `audio_url` field (in-memory TTL cache, served via `GET /chat/audio/{cache_id}`). `EventQuestionRequested` becomes `pending_question` ChatReply field (buffered) + typed SSE frame (streaming). Follow-up `POST /chat/answer/{question_id}` resumes the run.
- **Files:** NEW [internal/channels/web/voice_outbound.go](internal/channels/web/voice_outbound.go); MODIFY [internal/api/chat.go](internal/api/chat.go), [internal/channels/web/outbound.go](internal/channels/web/outbound.go), [cmd/aura/web_chat_hooks.go](cmd/aura/web_chat_hooks.go); NEW endpoint handlers.
- **LOC delta:** +320.
- **Acceptance:**
  - Probe: `/api/chat?voice=all` returns ChatReply with `audio_url`; GET returns OGG audio bytes `>1024 && content-type=audio/ogg`.
  - Probe: agent calls `ask_user` → ChatReply has `pending_question{id, question, options[], kind}`; follow-up POST `/chat/answer/{id}` resumes the same run; final reply contains post-answer reply.
- **Dependency:** US-CONS-07.

---

## Sequencing

US-CONS-02 → US-CONS-03 → US-CONS-04 (the big one, feature-flagged) → US-CONS-05 → US-CONS-06 → US-CONS-07 → US-CONS-08. Each is one commit (no batching except US-CONS-04's flag-gated 1-week soak before legacy delete).

---

## Risks

- **R1 (US-CONS-02)**: in-flight web sessions don't survive deploy. Mitigation: deploy off-hours; existing `webChatIdleTTL = 30min` self-evicts within 30min.
- **R2 (US-CONS-04)**: largest single commit (-360 LOC). Mitigation: feature flag + byte-parity test + transcript-comparison probe. 1-week soak.
- **R3 (US-CONS-07)**: SSE proxy buffering. Mitigation: `X-Accel-Buffering: no` header + flush after every frame. Document nginx requirement in `docs/deploy.md`.
- **R4 (US-CONS-08)**: voice synth blocking on buffered web. Mitigation: separate GET endpoint with TTL cache.
- **R5**: ChannelData privacy invariant. Mitigation: assertion test that `agentcore.Build` doesn't carry ChannelData into agent.Run outputs.

---

## Verification

- `go test ./... -run "TestHub|TestChatService|TestInvocationBuilder|TestAgentCore"` green.
- Byte-parity test (`internal/channels/telegram/fixture/byte_parity_test.go`) GREEN every commit.
- `cmd/probe_chat` web case suite ON PAR with telegram suite (add web cases if missing).
- Transport-cross probe: same query via both transports → reach parity on `tools_used[]`, `tool_calls`, `llm_calls`, reply substring.

---

*Updated 2026-05-21.*
