# Phase-CONS — D:/tmp Reference Survey

**Date:** 2026-05-24
**Trigger:** User request "tutti gli esempi di riferimento" prior to launching Phase-CONS Wave A
**Repos surveyed:** openhuman, hermes-agent, assistant-ui, nanobot, codex, picobot, elysia, cli-printing-press

## Convergent patterns (3+ repos agree)

1. **Tool registry as RWLock<HashMap>** — already in Aura, continue
2. **Per-sender/per-chat state override map** — openhuman `route_overrides` + nanobot `splitKey`; recommended for voice_mode/provider/model overrides
3. **Append-only telemetry log** — openhuman conversations/store.rs, picobot tools/registry.go; emit structured entry per turn
4. **Streaming as periodic buffer flush** — openhuman Telegram draft edits + assistant-ui Vercel SDK frames

## Divergent (anti-patterns)

1. **Two builders per channel** — current Aura state. Openhuman uses single `Channel` trait with per-channel formatting. REJECT.
2. **Bidirectional ask_user blocking on event bus** — openhuman has a STUB only. Must build from scratch.
3. **Per-channel data silos** — openhuman + nanobot both use unified table with channel column. REJECT separate tables.

## Per-story recommendations for Wave A (CONS-02..08)

### CONS-02 (sessions/state collapse onto agent.SessionStore)
- Openhuman ref: routes.rs:125-131 `route_overrides` HashMap keyed by sender_id pattern → safe to follow.
- No change to AC needed; pattern aligns.

### CONS-03 (webToolExecutor → agent.ExecuteToolCalls)
- No D:/tmp ref needed — this is internal dedup.

### CONS-04 (agentcore.Builder)
- Openhuman ref: `Channel` trait + ChannelRuntimeCommand enum at channels/routes.rs.
- Confirms PerTurnHooks design (transports supply only hooks).
- Risk: openhuman's broadcast hub is *outbound only*; Aura needs bidirectional → ask_user channel design must be separate (see CONS-08).

### CONS-05 (Single Hub)
- Openhuman ref: single `broadcast::Sender<WebChannelEvent>` channel routes all inbound (channels/routes.rs:1-45).
- Confirms one Hub for {telegram, web}, cron stays separate.

### CONS-06 (soft-budget + context compaction on web)
- Openhuman ref: `microcompact()` in context/microcompact.rs:59-100. Pattern: measure prompt size, clear OLDEST tool result PAYLOADS in place when exceeds 0.8× window.
- **Concrete threshold to lift**: soft budget = 0.8 × ModelContextWindow (already in Phase-CTX).
- Aura already has compaction (Phase-CTX shipped); just wire it into web turn flow.

### CONS-07 (SSE streaming on web)
- **BEST REF: assistant-ui** templates/minimal/app/api/chat/route.ts:20-30 — uses Vercel AI SDK `streamText()` + `toUIMessageStreamResponse()`.
- **Strategic decision**: emit Vercel AI SDK wire format from Go backend so assistant-ui frontend (Wave B) works out of the box.
- Frame schema: `data: {"type":"text"|"tool_call"|"stop", ...}\n\n`.
- Header: `X-Accel-Buffering: no` confirmed standard nginx-bypass.
- Openhuman Telegram draft-edit pattern (channel_ops.rs:84-92) is the *buffering* analog — flush every N ms or K bytes. Same algorithm, different transport.

### CONS-08 (voice + ask_user on web)
- **Voice ref**: openhuman ttsClient.ts:48-78 — backend RPC returns `{audio_base64, audio_mime, visemes, alignment}`. Pattern: TTS post-reply, frontend caches as data-URL.
- **Aura tweak**: base64 inflates 33%; for replies >100KB audio, use separate GET `/api/chat/audio/{cache_id}` instead (matches current Aura plan).
- **Voice mode per-conversation**: hermes pattern (per-chat off/voice_only/all override).
- **ask_user**: openhuman has STUB only (ask_clarification.rs:59-85). MUST build bidirectional from scratch:
  - State: `Mutex<HashMap<question_id, ResponseChannel>>` on Hub
  - Emit `EventQuestionRequested` to frontend
  - Frontend posts `/chat/answer/{question_id}` → Hub closes channel → agent resumes
  - **Persistence**: if user closes browser tab + reopens, the pending question must be discoverable → consider 5-min TTL in SQLite

## Per-wave recommendations for Wave B (CONS-09..13, frontend)

### Assistant-ui adoption confirmed
- Purpose-built for streaming agent UIs (React + Vercel AI SDK).
- `useChat()` hook handles streaming + message management + tool render.
- Aura backend must emit Vercel AI SDK format (locked in CONS-07).
- Drop hand-rolled `useState` + `EventSource` from current Vite chat → use assistant-ui primitives.

### Risk
- Vercel AI SDK ties to specific model interfaces. Aura uses OpenAI-compat (OpenRouter) so should map cleanly, but verify with Claude/Gemma test case during CONS-09.

## What's NEW work (no D:/tmp ref to copy)

1. Bidirectional ask_user round-trip with persistence — openhuman's stub doesn't cover.
2. Per-conversation `voice_mode` SQLite column + dashboard toggle.
3. `audio_url` cache TTL endpoint serving OGG bytes.

These 3 are "design from scratch" but informed by partial references above.

## Source references (clickable)

- [openhuman channels/routes.rs](D:/tmp/openhuman/src/openhuman/channels/routes.rs)
- [openhuman channels/providers/telegram/channel_ops.rs](D:/tmp/openhuman/src/openhuman/channels/providers/telegram/channel_ops.rs)
- [openhuman tools/impl/agent/ask_clarification.rs](D:/tmp/openhuman/src/openhuman/tools/impl/agent/ask_clarification.rs)
- [openhuman voice/reply_speech.rs](D:/tmp/openhuman/src/openhuman/voice/reply_speech.rs)
- [openhuman memory/conversations/store.rs](D:/tmp/openhuman/src/openhuman/memory/conversations/store.rs)
- [openhuman context/microcompact.rs](D:/tmp/openhuman/src/openhuman/context/microcompact.rs)
- [openhuman app/voice/ttsClient.ts](D:/tmp/openhuman/app/src/features/human/voice/ttsClient.ts)
- [assistant-ui templates/minimal/app/api/chat/route.ts](D:/tmp/assistant-ui/templates/minimal/app/api/chat/route.ts)
- [hermes-agent tools/voice_mode.py](D:/tmp/hermes-agent/tools/voice_mode.py)
- [nanobot webui/src/lib/api.ts](D:/tmp/nanobot/webui/src/lib/api.ts)
