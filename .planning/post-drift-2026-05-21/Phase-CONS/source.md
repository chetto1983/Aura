# Phase-CONS Source Map

**Role:** source
**Status:** closed 2026-05-24 after US-CONS-13
**Current slice:** none. CONS-02..13 are implemented and verified.

## Objective

Consolidate web and Telegram into one channel-neutral agent path without losing transport-specific UX. The active backend objective is to move transport-independent `agent.Invocation` assembly into `internal/agentcore.Builder` while preserving transport hooks for Telegram rendering, web responses, ask_user, budget warnings, and finalization.

## Canonical Inputs

| Source | Evidence Used | Decision |
| --- | --- | --- |
| `PRD.md` section 7.5 | Phase-CONS is the next post-CTX phase; PRD expands the phase to CONS-02..13 and requires `internal/agentcore.Builder`, web parity, and assistant-ui later. | Repair this phase plan before code. |
| `PRD.md` section 15 | Web chat is a first-class product surface; threads stay per-channel; `/api/chat` remains a compat wrapper over Chat Hub. | Do not add web-only runtime behavior; keep channel-neutral backend contracts. |
| `.planning/post-drift-2026-05-21/INDEX.md` | Phase-CTX is closed; Phase-CONS is closed through US-CONS-13. | Require a fresh benchmark gap before reopening CONS work. |
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
| `D:/tmp/assistant-ui/apps/docs/content/docs/guides/tool-ui.mdx` | UI-only tools defined elsewhere should use `makeAssistantToolUI`; the renderer receives args, result, and status. | Register Aura's backend-owned tool UIs without defining frontend execution tools. |
| `D:/tmp/assistant-ui/templates/minimal/components/assistant-ui/thread.tsx` | Assistant messages render `part.toolUI ?? <ToolFallback {...part} />` inside `MessagePrimitive.Parts`. | Use the same generative UI pattern for Aura's assistant-ui Thread. |
| `D:/tmp/assistant-ui/templates/minimal/components/assistant-ui/tool-fallback.tsx` | Generic tool fallback is collapsible, status-aware, and shows args/result on expansion. | Adopt the shape, but keep Aura's privacy rule: argument values are redacted before display. |

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
| Assistant-ui local 2026 docs/source, checked 2026-05-24: `D:/tmp/assistant-ui/apps/docs/content/docs/guides/tools.mdx` | Pick one registration style per tool, centralize definitions, handle errors/loading states, and test tools in isolation plus the full assistant flow. | CONS-11 uses one UI-only registration layer plus helper unit tests and browser flow probes. |

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

## Adopted For US-CONS-08

- Add a disposable in-memory `AudioCache` in `internal/api` because synthesized web audio is an HTTP retrieval artifact, not canonical chat state.
- Keep the concrete `pockettts.Client` adapter in `cmd/aura/web_voice.go`; `internal/channels/web` stays provider-agnostic and only projects chat events.
- Add `audio_url` to buffered `ChatReply` only when `voice=all|on|true|1`, Pocket-TTS is configured, and the assistant reply has text.
- Serve cached audio through `GET /api/chat/audio/{cache_id}` with `Content-Type` from the synthesizer and `Cache-Control: private, max-age=3600`.
- Project `chat.EventQuestionRequested` into buffered `pending_question` and streaming `data-pending-question` UI Message Stream frames.
- Share Telegram and web ask_user answer parsing through `internal/channels/askuser` so numeric option handling, canonical approval options, and pending tool-result injection stay consistent.
- Resume web answers through `POST /api/chat/answer/{question_id}` and the shared Hub; durable ground truth remains `chat_questions` plus `question_answered` run events.
- Support browser close/reopen by looking up `Hub.PendingQuestion` and falling back to durable question options/kind even when the in-memory pending tool call is missing.

## Adopted For US-CONS-09

- Add only `@assistant-ui/react` and `@assistant-ui/react-data-stream`; Tailwind already exists in Aura web, so the package install should not introduce a second styling stack.
- Use `useDataStreamRuntime` with the default current `ui-message-stream` protocol. Do not opt into legacy `data-stream`.
- Keep auth aligned with existing dashboard `api.ts`: pass `Authorization: Bearer <token>` from `web/src/lib/auth.ts` and keep the existing `/login` redirect gate.
- Store the web chat thread as a URL `thread_id` when present, otherwise generate `web_<uuid>` and write it back with `history.replaceState`.
- Send Aura's durable thread ID through `body.thread_id`; assistant-ui's internal thread list remains a UI concern.
- Mount `/chat` as a lazy dashboard route and keep the Chat chunk separate from the main dashboard bundle.
- Verify the frontend runtime with a Playwright browser probe that intercepts `/api/chat/stream` and returns the same UI Message Stream frames produced by the Go backend tests.
- Keep markdown/code/wiki-link rendering, tool-call cards, ask_user cards, audio playback, attachments, and dictation out of this slice; those are US-CONS-10..13.

## Adopted For US-CONS-10

- Reuse Aura's existing `web/src/components/Markdown.tsx` renderer instead of adding a second chat-only markdown stack.
- Add `rehype-highlight@7.0.2` because it is a small `react-markdown`-compatible highlighting plugin; reject Shiki for this slice because it would add a larger async highlighter surface before tool UI exists.
- Convert `[[slug]]` and `[[slug|label]]` in the markdown text preprocessing layer to `Link to="/wiki/:slug"`, matching the existing dashboard route in `web/src/App.tsx`.
- Keep wiki-link conversion out of fenced code blocks so code examples are not rewritten.
- Override assistant-ui `Text` message parts only; leave tool/data/audio parts for US-CONS-11..13.
- Use browser UI Message Stream fixtures as ground truth for rendered markdown, highlighted code, wiki links, external link safety, and mobile code-block width.

## Adopted For US-CONS-11

- Render assistant-ui `tool-call` parts with `part.toolUI ?? <ToolCallComponent {...part} />`, matching the local assistant-ui minimal template.
- Register backend-owned UI-only tool renderers with `makeAssistantToolUI` for `wiki_page` and `web_search`.
- Also register Aura's consolidated `web` tool to the web-search card so the current manifest still gets a rich search UI after tool consolidation.
- Keep argument values private: backend stream emits `tool-call-delta.argsText` from `arg_keys` only, with every value replaced by `[redacted]`.
- Put argument/result details behind an expandable block; browser QA expands the generic card to assert redaction and result visibility.
- Use structured browser fixtures for top-3 web search results and wiki link cards; real backend preview-only results fall back gracefully.

## Rejected For US-CONS-02

- No single Hub change; that is US-CONS-05.
- No shared `agentcore.Builder`; that is US-CONS-04.
- No web SSE, voice, ask_user UI, or assistant-ui dependency; those start after the backend consolidation gates.
- No production DB mutation for planning evidence. Handler/unit tests should use isolated SQLite fixtures.

## Rejected For US-CONS-07

- Do not emit legacy `0:"text"\n` data-stream lines. Local `useDataStreamRuntime` defaults to UI Message Stream, and the local legacy decoder expects raw prefix lines rather than SSE `data:` JSON.
- Do not add assistant-ui frontend dependencies in this slice; Wave B starts after the backend stream contract is verified.

## Rejected For US-CONS-08

- Do not put the concrete Pocket-TTS client inside `internal/channels/web`; provider wiring belongs in `cmd/aura`, and the API only depends on the narrow `ChatVoiceService` interface.
- Do not persist synthesized audio in SQLite or the wiki/source stores. Audio cache deletion must only break playback of the temporary URL, not Aura's belief about the conversation.
- Do not duplicate Telegram ask_user parsing in web; the channel-neutral helper owns option display and numeric answer parsing.
- Do not add assistant-ui UI cards in this slice; backend `pending_question`/`audio_url` contracts unblock CONS-12/13.

## Rejected For US-CONS-09

- Do not add `@assistant-ui/styles` or a prebuilt assistant-ui theme. Aura owns visual density, shell navigation, and design tokens.
- Do not pass auth through query params. The local `D:/tmp/openhuman` event-token pattern applies to EventSource-style SSE without headers, while Aura's assistant-ui runtime uses `fetch` and can send bearer headers.
- Do not start live LLM/browser smoke as the completion benchmark. The slice benchmark uses a browser UI Message Stream fixture plus Go backend stream contract tests; live provider coverage remains a later integration gate when credentials/services are available.

## Rejected For US-CONS-10

- Do not implement `/wiki/page/:slug`; the current Aura router is `/wiki/:slug`.
- Do not add copy/retry/regenerate UI controls yet. This slice localizes the strings and send button label, but action bars belong with richer message/tool UI.
- Do not render tool-call, ask_user, audio, attachment, or dictation UI in the markdown slice.

## Rejected For US-CONS-11

- Do not expose raw tool argument values in the browser to satisfy the "full args" wording; Aura's tool argument privacy rule is stronger, so only keys and redacted placeholders render.
- Do not define frontend-executed tools for `wiki_page`, `web_search`, or `web`; Aura's backend remains the execution authority.
- Do not add a second styling package or assistant-ui theme. Cards use Aura's existing Tailwind tokens and lucide icons.
- Do not make the web card depend on the deprecated `web_search` tool name only; the current Aura manifest uses the consolidated `web` tool.

## Adopted For US-CONS-12

- Use `useDataStreamRuntime`'s `onData` callback for `data-pending-question` frames. The installed assistant-ui stream decoder stores `data-*` frames as message metadata, not visible content parts, so page-level state is the reliable rendering path.
- Keep the backend `data-pending-question` UI Message Stream contract unchanged; it already exposes `id`, `question`, `options`, and `kind`.
- Render the pending card inline in `web/src/pages/Chat.tsx` below the message list and above the composer, inside the `AssistantRuntimeProvider`, so answer submission can append the final `ChatReply` as a normal assistant message through the thread runtime.
- Send `/api/chat/answer/{id}` with `answer`, current `thread_id`, and `selected_option_ids` for option clicks; this matches `internal/api/chat.go` and preserves durable same-thread resume.
- Use concrete D:/tmp browser fixtures: approval frame with `options:["approve","deny"]` and clarification frame with free-text textarea. Screenshots are stored as `D:/tmp/aura-chat-us-cons-12-approval.png` and `D:/tmp/aura-chat-us-cons-12-clarification.png`.
- Keep helper tests focused on payload normalization and answer-body construction so browser QA can focus on rendered behavior and API body assertions.

## Rejected For US-CONS-12

- Do not rely on `MessagePrimitive.Parts` alone for streamed `data-*` frames; local assistant-ui source shows those frames are emitted through `onData` and `metadata.unstable_data`, not rendered message content.
- Do not start a second chat stream after answer submission. The existing backend answer endpoint resumes the durable question and returns a `ChatReply`; the UI appends that reply to the assistant thread.
- Do not implement audio playback, file attachments, or dictation in this slice; those are the US-CONS-13 surface.

## Adopted For US-CONS-13

- Use assistant-ui attachment primitives and `AttachmentAdapter` for composer chips, but upload immediately through Aura's existing `/api/sources/upload` endpoint and send only source references in chat metadata.
- Use `WebSpeechDictationAdapter` and browser `SpeechRecognition` for chat dictation input. Aura's whisper sidecar remains separate voice-memo infrastructure, not part of this chat composer path.
- Treat backend `audio_url` and streamed `data-audio-url` frames as the same UI concept: render a bounded HTML5 audio control only for `/api/chat/audio/{id}` paths.
- Carry source references into the shared web Hub as `chat.AttachmentRef`, then format explicit `source_id` lines into the user-visible LLM request so the model can decide when to call the `source` tool.

## Rejected For US-CONS-13

- Do not embed uploaded file bytes into assistant-ui message bodies or LLM context; canonical source storage owns the bytes and chat sends `src_*` handles.
- Do not delete canonical sources when a composer chip is removed. Removal detaches the pending message reference only.
- Do not route chat dictation through server-side Whisper. That would conflate browser STT input with Aura's voice memo ingestion path and add latency without improving the CONS benchmark.

## Open Questions Carried Forward

- CONS-07 locked the Vercel AI SDK UI Message Stream schema from current package docs/source before implementation.
- CONS-08 resolved durable ask_user resume by using `chat_questions` as the browser-close source of truth and injecting either the pending tool result or an explicit "Answer to pending ..." user message when only durable state survives.
