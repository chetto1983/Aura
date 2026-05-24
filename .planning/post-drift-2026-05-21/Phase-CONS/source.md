# Phase-CONS Source Map

**Role:** source
**Status:** self-audited planning repair, 2026-05-24
**Current slice:** US-CONS-08. CONS-02..07 are committed; next add web voice + ask_user parity on top of the verified UI Message Stream backend.

## Objective

Consolidate web and Telegram into one channel-neutral agent path without losing transport-specific UX. The active backend objective is to move transport-independent `agent.Invocation` assembly into `internal/agentcore.Builder` while preserving transport hooks for Telegram rendering, web responses, ask_user, budget warnings, and finalization.

## Canonical Inputs

| Source | Evidence Used | Decision |
| --- | --- | --- |
| `PRD.md` section 7.5 | Phase-CONS is the next post-CTX phase; PRD expands the phase to CONS-02..13 and requires `internal/agentcore.Builder`, web parity, and assistant-ui later. | Repair this phase plan before code. |
| `PRD.md` section 15 | Web chat is a first-class product surface; threads stay per-channel; `/api/chat` remains a compat wrapper over Chat Hub. | Do not add web-only runtime behavior; keep channel-neutral backend contracts. |
| `.planning/post-drift-2026-05-21/INDEX.md` | Phase-CTX is closed; Phase-CONS is NEXT. | Start Phase-CONS now. |
| `.planning/post-drift-2026-05-21/Phase-CONS/plan.md` | Original backend plan, CONS-02..08. | Keep backend sequencing but repair missing source/benchmark/progress files. |
| `scripts/ralph/prd-phase-cons-staged.json` | Detailed current queue, CONS-02..13, including Wave B assistant-ui. | Use as detailed story queue after planning repair. |
| `docs/research-2026-05-21/web-telegram-consolidation.md` | Audit finds duplicated web/Telegram invocation builders, duplicate web session/state, duplicate tool executor, separate hubs, and web feature drift. | CONS-02 starts with the lowest-risk duplicate: web session/state. |
| `docs/research-2026-05-24-cons-tmp-survey.md` | D:/tmp survey locked assistant-ui, SSE streaming, openhuman microcompact/route patterns, and ask_user gaps. US-CONS-07 refreshed the exact protocol from local `assistant-ui` source and current AI SDK docs before implementation. | Use current UI Message Stream for CONS-07/Wave B; do not implement legacy data-stream unless the frontend explicitly opts into `protocol: "data-stream"`. |

## Aura Code Evidence

| Path | Observed Shape | CONS Decision |
| --- | --- | --- |
| `cmd/aura/web_chat.go` | Defines `webChatSessions`, `webAgentState`, `trimWebMessages`, and `webInvocationBuilder.Build`; `TrackTokens` is a no-op. | US-CONS-02 removes these bespoke state types and starts each web turn from `agent.SessionStore`. |
| `internal/agent/session.go` | `SessionStore.Begin` returns a reusable `conversation.Context`; snapshots and active status are already central. | Reuse this store with a web-scoped key. |
| `internal/conversation/context.go` | `Context` owns system prompt rebuild, agent note/search context, token tracking, tool-result compaction, and message caps. | Web should use this directly, not a flat `[]llm.Message` shim. |
| `internal/channels/telegram/invocation_builder.go` | Telegram already calls `SessionStore().Begin` and mutates `conversation.Context`. | Match this behavioral layer before consolidating builders. |
| `internal/channels/web/chat_service.go` | Web constructs `chat.InboundMessage` inline and calls `ReceiveMessage`. | Not in CONS-02 scope; inbound adapter comes in CONS-05. |
| `internal/channels/web/outbound.go` | Deferred `Router` buffers `chat.OutboundEvent` by run ID. | Keep unchanged for CONS-02; SSE comes later. |
| `cmd/aura/web_chat_test.go` | Existing tests cover web run persistence, tool attempts, terminal `text_response`, tool context, and thread scoping through `webChatSessions`. | Replace thread-scoping tests with `SessionStore`/`Context` assertions. |
| `internal/channels/web/chat_service_test.go` | Existing fake hub verifies deferred web bridge behavior. | Keep as transport bridge safety net. |
| `internal/agent/runtime.go` | `agent.Invocation` is the stable runtime contract consumed by `agent.Run`. | `agentcore.Builder` should assemble this contract without importing Telegram/web delivery packages. |
| `internal/chat/agentloop.go` | Chat Hub calls a per-channel `InvocationBuilder` and then translates `agent.Event` to outbound events. | Agentcore must sit below chat Hub and above transport adapters; the Hub contract stays unchanged in US-CONS-04. |
| `internal/channels/telegram/invocation_builder.go` | Telegram still owns a large `agent.Invocation` literal plus transport finalization, archive, status pane, and ask_user rendering. | Next US-CONS-04 slice moves the literal assembly to agentcore while leaving Telegram-only hooks in this adapter. |

## D:/tmp Example Sweep

| Example Path | Adopt | Reject / Boundary |
| --- | --- | --- |
| `D:/tmp/codex/sdk/typescript/samples/basic_streaming.ts` | Stream consumers handle typed events (`item.completed`, `turn.completed`) while the thread owns continuity. | Not relevant to CONS-02 except confirming event streams should stay outside session state. |
| `D:/tmp/codex/sdk/python/tests/test_app_server_streaming.py` | Tests assert deltas, completion, request text, and interleaved streams by turn/thread ID. | Do not introduce streaming in CONS-02; reserve for CONS-07. |
| `D:/tmp/openhuman/src/core/event_bus/events.rs` | Channel inbound messages carry channel, sender, reply target, and thread to avoid collapsing senders in shared channels. | Use `web:<userID>:<threadID>`; do not key web context on user alone. |
| `D:/tmp/openhuman/src/core/auth.rs` and `D:/tmp/openhuman/src/core/event_bind_tokens.rs` | EventSource cannot send custom headers, so sensitive SSE needs a token workaround. | Not in CONS-02; record for CONS-07. |
| `D:/tmp/openhuman/src/openhuman/webhooks/router.rs` | Route ownership is explicit and isolated by key. | Keep channel/thread keying explicit; do not share web and Telegram thread state accidentally. |
| `D:/tmp/hermes-agent/AGENTS.md` | UI owns presentation; Python/core owns sessions, tools, model calls. Prompt-cache policy warns against mutating past context mid-conversation except compaction. | Adopt separation: backend owns conversation context; frontend Wave B must not create a second memory model. |
| `D:/tmp/assistant-ui/templates/minimal/app/api/chat/route.ts` | Assistant-ui expects streaming runtime contracts later. | Do not shape CONS-02 around frontend concerns. |
| `D:/tmp/assistant-ui/packages/react-data-stream/src/useDataStreamRuntime.ts` | `protocol` defaults to `"ui-message-stream"`; `"data-stream"` is explicitly legacy. | US-CONS-07 backend must emit UI Message Stream by default for Wave B. |
| `D:/tmp/assistant-ui/packages/assistant-stream/src/core/serialization/ui-message-stream/UIMessageStream.ts` | Decoder parses SSE `data:` events, JSON objects, and the literal `[DONE]` terminator. | US-CONS-07 frames are `data: {...}\n\n` plus `data: [DONE]\n\n`. |
| `D:/tmp/assistant-ui/packages/assistant-stream/src/core/serialization/ui-message-stream/chunk-types.ts` | Local accepted chunks include `start`, `text-start`, `text-delta` with `textDelta`, `text-end`, `tool-call-start`, `tool-call-end`, `tool-result`, `finish`, and `error`. | Implement these core chunks first; carry both `textDelta` and `delta` on text deltas for current docs/local compatibility. |
| `D:/tmp/assistant-ui/packages/assistant-stream/src/core/serialization/data-stream/DataStream.ts` | Legacy data-stream uses `text/plain` plus `x-vercel-ai-data-stream`; it is selected only when the runtime option sets `protocol: "data-stream"`. | Reject for Aura default because Wave B wants zero extra frontend protocol override. |

## 2026 Practice Sweep

Checked 2026-05-24:

| Source | Relevant Practice | CONS Use |
| --- | --- | --- |
| WHATWG HTML Living Standard, Server-sent events, last updated 2026-05-19: `https://html.spec.whatwg.org/dev/server-sent-events.html` | SSE is a UTF-8 event stream with named events and `text/event-stream`. | CONS-07 benchmark must assert wire format; no custom hidden streaming transport. |
| MDN "Using server-sent events": `https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events` | Use `text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`, custom `event:` fields, and flush per event. | CONS-07 benchmark must assert headers and frame termination. |
| Go `net/http.ResponseController.Flush`: `https://go.dev/pkg/net/http/#ResponseController.Flush` | Go handlers can explicitly flush buffered data to the client. | CONS-07 should use Go-native flush semantics. |
| AI SDK UI "Stream Protocols", checked 2026-05-24: `https://ai-sdk.dev/docs/ai-sdk-ui/stream-protocol` | Custom backends use SSE and must set `x-vercel-ai-ui-message-stream: v1`; text deltas use start/delta/end and streams terminate with `[DONE]`. | CONS-07 upgrades the stale queue wording from legacy data-stream to UI Message Stream. |
| OpenAI Agents SDK streaming guide: `https://openai.github.io/openai-agents-js/guides/streaming/` | Stream consumers inspect generic run events while preserving provider-specific raw events only when needed. | Agentcore/event contracts should stay transport-neutral. |
| Anthropic Context Engineering cookbook, published 2026-03-20: `https://platform.claude.com/cookbook/tool-use-context-engineering-context-engineering-tools` | Long-running agents need explicit memory, compaction, and tool-result clearing strategies; context is finite. | CONS-02 must prove web now benefits from `conversation.Context` compaction/token tracking. |
| Anthropic harness design, published 2026-03-24: `https://www.anthropic.com/engineering/harness-design-long-running-apps` | Use tractable chunks, explicit sprint contracts, and a separate evaluator/QA pass when the work is beyond trivial. | Each CONS story gets its own benchmark and QA, not a phase-wide smoke run. |

## Adopted For US-CONS-02

- Use `agent.SessionStore` as the single web session owner.
- Use a stable key in the form `web:<userID>:<threadID>`.
- Use `conversation.Context` directly as `agent.State`; preserve system prompt by calling `SetSystemMessage`.
- Keep `internal/channels/web` bridge unchanged in this slice.
- Keep `webToolExecutor` unchanged until US-CONS-03.
- Replace tests of removed `webChatSessions` with tests of retained context, token tracking, and tool-result compaction.

## Adopted For US-CONS-03

- Reuse `agent.ExecuteToolCalls` for web tool dispatch.
- Preserve web-visible tool context with `WithConversationID` and `WithToolTimeout` options.
- Delete the web-only executor and keep stale-symbol checks as negative QA.

## Adopted For US-CONS-04A

- Introduce `internal/agentcore.Builder` as the owner of channel-neutral `agent.Invocation` assembly.
- Start with the web builder because CONS-02/03 already removed web's bespoke state and executor layers.
- Keep Chat Hub, Telegram rendering, archive, and ask_user behavior unchanged until the next US-CONS-04 slice.
- Do not mark US-CONS-04 complete until Telegram uses agentcore and the feature-flag/parity gate is recorded.

## Adopted For US-CONS-04B

- Wire Telegram's existing invocation literal through `internal/agentcore.Builder`.
- Keep Telegram-only behavior in `internal/channels/telegram/invocation_builder.go`: status pane, ask_user rendering, prompt hot reload, archive, soft budget notification, and final Telegram delivery.
- Use existing Telegram fixture tests plus full Go suite as the behavior-preserving safety net.
- Leave US-CONS-04 open until US-CONS-04C records the closure evidence for the final adoption/parity gate.

## Adopted For US-CONS-04C

- Replace the originally planned `AURA_AGENTCORE_BUILDER` soak flag with a static adoption/parity gate because US-CONS-04A/04B removed the legacy invocation construction path instead of leaving two live paths.
- Assert both `cmd/aura/web_chat.go` and `internal/channels/telegram/invocation_builder.go` call `agentcore.Builder.Build` with `agentcore.InvocationInput`.
- Assert transports no longer own non-empty `agent.Invocation` literals; zero-value error returns remain allowed.
- Assert production runtime/config files do not introduce a no-op `AURA_AGENTCORE_BUILDER` shim.
- Use Telegram fixture byte-parity, touched-path lint/dupl, and full Go tests as the closure evidence before setting `US-CONS-04.passes=true`.

## Adopted For US-CONS-05

- Add `cmd/aura/chat_hub.go` as the composition-root helper that constructs one user-facing `chat.Hub` for web and Telegram.
- Use a channel-multiplexing `chat.InvocationBuilder` so the single Hub can route `ChannelWeb` to `webInvocationBuilder.Build` and `ChannelTelegram` to `telegramadapter.InvocationBuilder.Build`.
- Delete `telegramadapter.NewHub`; Telegram now exports only builder/outbound pieces and receives the shared Hub through `AttachHub`.
- Delete `newHubBackedWebChatService`; web chat now receives an injected `*chat.Hub` and `webadapter.Router`.
- Add `internal/channels/web/inbound.go` so `/api/chat` goes through `Hub.Receive(ChannelWeb, raw)` and the same inbound-adapter normalization path as Telegram.
- Keep cron Hub creation unchanged in `app_wire.go` because it has a distinct lifecycle and silent outbound policy.

## Adopted For US-CONS-06

- Add `budget_warning` to the buffered web/API reply path and carry it through `chat.EventUsage` so clients do not need log parsing.
- Wire web turns to the same budget runtime used by Telegram: hard-budget preflight, usage recording, and cost estimation.
- Add `conversations.channel` with default `telegram` plus an idempotent migration so web archive rows can be asserted directly as `channel='web'`.
- Archive web turns through `conversation.ArchiveConversationTurns` using deterministic web archive IDs for `chat_id` and `user_id`.
- Keep web's existing post-turn tool-result compaction and context enforcement, and protect it with existing US-CONS-02 regression tests.

## Adopted For US-CONS-07

- Add a streaming web outbound adapter registered as `(ChannelWeb, DeliveryModeStreaming)` on the shared user-facing Hub.
- Add `POST /api/chat/stream` as an SSE endpoint; the internal router path remains `POST /chat/stream` because `cmd/aura` mounts API routes under `/api`.
- Emit AI SDK UI Message Stream frames, not legacy prefix-coded data-stream frames: `data: {"type":"start"}`, `text-start`, `text-delta`, `text-end`, `tool-call-start`, `tool-call-end`, `tool-result`, `finish`, then `data: [DONE]`.
- Set `Content-Type: text/event-stream; charset=utf-8`, `Cache-Control: no-cache, no-transform`, `X-Accel-Buffering: no`, and `x-vercel-ai-ui-message-stream: v1`.
- Use `llm.Client.Stream` for web streaming turns and write token deltas as they arrive; suppress duplicate full-content deltas at the SSE sink when the agent lifecycle later emits the aggregated `EventMessageDelta`.
- Keep buffered `POST /api/chat` unchanged and backed by `DeliveryModeDeferred`.

## Rejected For US-CONS-02

- No single Hub change; that is US-CONS-05.
- No shared `agentcore.Builder`; that is US-CONS-04.
- No web SSE, voice, ask_user UI, or assistant-ui dependency; those start after the backend consolidation gates.
- No production DB mutation for planning evidence. Handler/unit tests should use isolated SQLite fixtures.

## Rejected For US-CONS-07

- Do not emit legacy `0:"text"\n` data-stream lines. Local `useDataStreamRuntime` defaults to UI Message Stream, and the local legacy decoder expects raw prefix lines rather than SSE `data:` JSON.
- Do not add assistant-ui frontend dependencies in this slice; Wave B starts after the backend stream contract is verified.

## Open Questions Carried Forward

- CONS-07 locked the Vercel AI SDK UI Message Stream schema from current package docs/source before implementation.
- CONS-08 must design durable ask_user resume for browser close/reopen; D:/tmp examples only provide partial stubs.
