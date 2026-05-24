# Phase-CONS - Web <-> Telegram 1+1 Consolidation

**Status:** active planning repaired 2026-05-24; first executable slice is US-CONS-02
**Provenance:** web-telegram-consolidation scout #7 (full deliverable `docs/research-2026-05-21/web-telegram-consolidation.md`)
**Extended provenance:** `docs/research-2026-05-24-cons-tmp-survey.md`, `PRD.md` section 7.5, and `scripts/ralph/prd-phase-cons-staged.json`
**Estimated effort:** ~5 sessions (Wave A backend CONS-02..08 + Wave B assistant-ui CONS-09..13)
**LOC delta:** net +710 (Wave A dedup -810 + parity +720; Wave B frontend +800)

---

## Why this phase

User mandate verbatim (2026-05-21): "we need consolidate 1 inbound and 1 outbound for all web and telegram no duplication and must have same level".

Today there are TWO InvocationBuilders (`internal/channels/telegram/invocation_builder.go` 688 LOC + `cmd/aura/web_chat.go` 612 LOC, both violate the 600-LOC rule) and THREE `chat.Hub` instances (telegram, web, cron). Web is functionally degraded in 7 features: streaming, voice, markdown render, soft-budget, context compaction, `tools_allowed`, `tools_used` (only `tools_used` works on both today).

**CONS-01 lives in Phase-BUG** because it's the live bug fix (web missing overlays); the executable queue for this phase starts at CONS-02. The original 2026-05-21 plan covered CONS-02..08. The 2026-05-24 Wave B extension adds CONS-09..13 for assistant-ui after the backend SSE/voice/ask_user contracts exist.

Planning contract repaired 2026-05-24:

- `source.md` now names Aura evidence, D:/tmp examples, and current 2026 references.
- `benchmark.md` now defines per-slice ground-truth checks. Smoke tests are prechecks only.
- `progress.md` now records phase status and will be appended after each atomic slice.
- `scripts/ralph/prd-phase-cons-staged.json` is the detailed story queue for CONS-02..13.

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
- **Risk mitigation:** The original `AURA_AGENTCORE_BUILDER` runtime flag is superseded by US-CONS-04A/04B removing the legacy invocation literals outright. US-CONS-04C must keep a real adoption/parity gate: both transports call `agentcore.Builder`, no transport owns a non-empty `agent.Invocation` literal, Telegram fixture byte-parity passes, and the full Go suite stays green. Do not add a no-op runtime flag.

### US-CONS-05 — Single Hub: collapse web + telegram Hubs into one

- **Scope:** Move Hub construction from `internal/channels/telegram/invocation_builder.go::NewHub` AND `cmd/aura/web_chat.go::newHubBackedWebChatService` into the `cmd/aura` composition root (`chat_hub.go`, threaded by `app_wire.go`). Register both inbound (`telegramadapter.New()` + `webadapter.New()`) and both outbound (`telegramadapter.Outbound` + `webadapter.Router`) on the SAME Hub. Cron Hub stays separate.
- **Files:** NEW [cmd/aura/chat_hub.go](cmd/aura/chat_hub.go); MODIFY [cmd/aura/app_wire.go](cmd/aura/app_wire.go); MODIFY [cmd/aura/main.go](cmd/aura/main.go); MODIFY [internal/channels/telegram/invocation_builder.go](internal/channels/telegram/invocation_builder.go) (delete `NewHub`); MODIFY [cmd/aura/web_chat.go](cmd/aura/web_chat.go) (delete Hub construction, take injected `*chat.Hub`); NEW [internal/channels/web/inbound.go](internal/channels/web/inbound.go).
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

- **Scope:** New `internal/channels/web/streaming_outbound.go` registers `(ChannelWeb, DeliveryModeStreaming)`. New `POST /chat/stream` (SSE) endpoint flushes Vercel AI SDK UI Message Stream frames. Web ChatClient becomes `NewStreamingChatClient` that flushes `llm.Token` chunks. Buffered `POST /chat` stays for non-stream clients.
- **Files:** NEW [internal/channels/web/streaming_outbound.go](internal/channels/web/streaming_outbound.go); NEW [internal/channels/web/sse_chat_client.go](internal/channels/web/sse_chat_client.go); MODIFY [internal/api/router.go](internal/api/router.go), [internal/api/chat.go](internal/api/chat.go); MODIFY [cmd/aura/web_chat_hooks.go](cmd/aura/web_chat_hooks.go).
- **LOC delta:** +280.
- **Acceptance:**
  - `go test ./internal/channels/web/...` green with SSE-frame assertion test.
  - `curl -N` against `/api/chat/stream` produces stream of `data: {...}\n\n` frames plus `data: [DONE]\n\n`.
  - Header: `x-vercel-ai-ui-message-stream: v1` emitted (AI SDK UI custom-backend compat).
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

## Wave B extension

CONS-09..13 are frontend/product stories and are intentionally blocked behind the backend protocol:

- **US-CONS-09** mounts assistant-ui `/chat` with `useDataStreamRuntime` once CONS-07 speaks Vercel AI SDK UI Message Stream frames.
- **US-CONS-10** adds markdown/code/wiki-link rendering and Italian UI strings.
- **US-CONS-11** renders tool calls as React components.
- **US-CONS-12** turns backend `pending_question` into ask_user UI.
- **US-CONS-13** adds voice playback, source attachments, and browser-side dictation.

Do not start Wave B until CONS-07's UI Message Stream benchmark passes. The backend wire format is the contract.

---

## Sequencing

US-CONS-02 -> US-CONS-03 -> US-CONS-04 (the big one, feature-flagged) -> US-CONS-05 -> US-CONS-06 -> US-CONS-07 -> US-CONS-08 -> US-CONS-09 -> US-CONS-10 -> US-CONS-11 -> US-CONS-12 -> US-CONS-13.

Each story is one atomic commit and one dedicated QA pass. No batching except mechanical, low-risk edits that are explicitly named in the story and verified by the same benchmark.

---

## Risks

- **R1 (US-CONS-02)**: in-flight web sessions don't survive deploy. Mitigation: deploy off-hours; existing `webChatIdleTTL = 30min` self-evicts within 30min.
- **R2 (US-CONS-04)**: largest story in the phase, split into 04A/04B/04C. Mitigation: adoption gate, byte-parity fixture, touched-path lint/dupl, and full Go suite before marking the story closed.
- **R3 (US-CONS-07)**: SSE proxy buffering. Mitigation: `X-Accel-Buffering: no` header + flush after every frame. Document nginx requirement in `docs/deploy.md`.
- **R4 (US-CONS-08)**: voice synth blocking on buffered web. Mitigation: separate GET endpoint with TTL cache.
- **R5**: ChannelData privacy invariant. Mitigation: assertion test that `agentcore.Build` doesn't carry ChannelData into agent.Run outputs.

---

## Verification

- `benchmark.md` is authoritative for phase evidence. It names the exact command, fixture, artifact, ground truth, threshold, and PRD gate for every story.
- Smoke checks are allowed only as prechecks. A story is not done until its benchmark asserts rows, API fields, emitted frames, filesystem artifacts, captured LLM messages, or rendered UI state.
- For CONS-02 first: prove web uses `agent.SessionStore` + `conversation.Context` by asserting retained thread context, token accounting, and tool-result compaction against captured request messages.
- For every backend Go slice: run the narrow package tests, `go vet ./...`, `go build ./...`, `golangci-lint --new-from-rev=HEAD` on touched paths, `dupl -t 60` on touched paths, and the LOC gate.
- For frontend Wave B: run `npm --prefix web run build` and browser/e2e verification against the running app.

---

*Updated 2026-05-21.*
