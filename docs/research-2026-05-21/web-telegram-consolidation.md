# Web / Telegram Consolidation Audit — 2026-05-21

User mandate (verbatim): "we need consolidate 1 inbound and 1 outbound for all web and telegram no duplication and must have same level".

Translation: ONE inbound + ONE outbound shape, NO transport-specific code paths above the adapter boundary, and feature parity (streaming, voice, markdown render, soft-budget, context compaction, `tools_allowed`, `tools_used`) on both transports.

---

## Executive summary

Aura already has the **right primitive** (`internal/chat.Hub` + `chat.InboundAdapter` + `chat.OutboundAdapter` + `chat.AgentLoop`) and the Telegram channel has been fully refactored onto it. The web channel was bolted on as a buffered adapter on a **separate Hub** with a **separate InvocationBuilder**, so the duplication is at the *invocation construction* layer, not at the chat-event layer.

In numbers:

| Layer | Telegram impl | Web impl | Duplication |
|---|---|---|---|
| InvocationBuilder | `internal/channels/telegram/invocation_builder.go::InvocationBuilder.Build` — 688 LOC | `cmd/aura/web_chat.go::webInvocationBuilder.Build` — 612 LOC | ~70% (prompt overlay, pinned operational, memoryindex priority cache, postTurn cfg, tool executor) |
| Tool executor | `agent.ExecuteToolCalls` (shared, in internal/agent) called from telegram `executeToolCalls` | `webToolExecutor.ExecuteToolCalls` — bespoke goroutine-per-call implementation in `cmd/aura/web_chat.go` | ~100% — `webToolExecutor` re-implements what `agent.ExecuteToolCalls` already does |
| Hub | Built per-bot in `internal/channels/telegram/invocation_builder.go::NewHub` | Built per-bot in `cmd/aura/web_chat.go::newHubBackedWebChatService` | TWO Hubs in process; cron also has its own (third) Hub |
| Session store | `agent.SessionStore` (per-user, with `OnClose` GC hook) | `webChatSessions` — bespoke `map[string][]llm.Message` in `cmd/aura/web_chat.go` | ~100% duplicate behavior, incompatible types |
| Conversation state | `conversation.Context` (token/message limits, summarizer, search context, agent_note, tool-result compaction) | `webAgentState` — minimal `[]llm.Message` shim that ignores all of the above | Web is functionally degraded (`TrackTokens` is a no-op, no compaction, no agent_note) |
| Outbound adapter | `internal/channels/telegram/outbound.go::Outbound` — 438 LOC streaming + TTS + voice policy | `internal/channels/web/outbound.go::Router/Buffer` — 225 LOC deferred buffer | Different shape but both consume the same `chat.OutboundEvent` stream — boundary is correct |
| Inbound adapter | `internal/channels/telegram/inbound.go::Inbound` — 92 LOC | none — web has no `chat.InboundAdapter`; constructs `chat.InboundMessage` inline in `chat_service.go::Chat` | Asymmetric: telegram is adapter-shaped, web bypasses the adapter |
| Markdown render | `internal/telegram/entity_markdown.go` (telegramify-markdown-go → tele.Entities) | Web returns raw text in JSON; the React client renders | Pipeline-asymmetric (acceptable) but the assistant-side rendering decisions diverge |
| Voice synthesis | `Outbound.maybeSpeak()` in `internal/channels/telegram/outbound.go` | none — web ChatReply has no audio bytes | Missing feature |
| Tools used tracking | Per `Bot.StoreOrchestrationSnapshot` + EventStats | Web `Buffer.apply()` accumulates from EventToolStart | OK, both work; duplicate accumulators |
| Soft-budget notice | `b.NotifySoftBudget(c, userID)` in InvocationBuilder OnEvent | not wired | Missing feature |
| Context compaction | `convCtx.CompactCompletedToolResults` + `EnforceLimit` in InvocationBuilder OnEvent | not wired (`webAgentState` has no compaction) | Missing feature |
| Tools allowed | Telegram: `toolReg.Definitions()` (all tools) | Web: `cleanWebToolList(deps.Tools.Names())` (allowlist filter), but the filter just returns all tool names lowercased + deduped — no actual restriction | De-facto identical; the "web has narrower allowlist" assumption in the codebase is false |
| Conversation archive | Telegram InvocationBuilder OnEvent → `archiver.Append` | none — web turns are not archived | Missing feature |

**Total LOC in the duplicated layer:** ~1300 LOC (`invocation_builder.go` 688 + `web_chat.go` 612). Realistic post-consolidation footprint: ~800 LOC in a single `internal/agentcore/invocation_builder.go` shared module + ~120 LOC per-transport bind file (~50 web + ~70 telegram for tele-specific UI affordances) ≈ ~940 LOC. Delta ≈ **-360 LOC** plus the removal of two whole concept clusters (`webChatSessions`, `webAgentState`, `webToolExecutor`).

---

# PART 1 — CURRENT STATE AUDIT

## 1. Telegram inbound handlers (message in)

| File | Function | Role | Produces |
|---|---|---|---|
| `internal/telegram/handlers.go` | `Bot.registerHandlers` | Wires telebot `OnText`, `OnDocument`, `OnVoice`, callback router | telebot route table |
| `internal/telegram/handlers.go` | `Bot.onMessage` | Per-text-message entry point; allowlist + `routeTelegramContext` | (side-effect: dispatch to hub via UserGate) |
| `internal/telegram/handlers.go` | `Bot.onAskUserCallback` | Inline-keyboard callback for ask_user; checks pending question state, routes the choice as a normal message via `routeTelegramContext` | (side-effect: dispatch) |
| `internal/telegram/handlers.go` | `Bot.routeTelegramContext` | Acquires `UserGate` slot, calls `b.hub.Receive(ctx, ChannelTelegram, c)` inside actor goroutine | `chat.Run` (async) |
| `internal/telegram/handlers.go` | `Bot.telegramHubContext` | Stamps actor identity + authority into ctx | `context.Context` |
| `internal/telegram/setup.go` | `buildAfterTranscribeHook` | Voice memo entry point: receives transcript + tele.Context from voice handler, constructs a `chat.InboundMessage` directly and calls `b.hub.ReceiveMessage` (bypasses the `Normalize` step because the raw is no longer a tele.Update) | `chat.Run` |
| `internal/telegram/documents.go` | `docHandler.onDocument` | Document upload entry point; runs ingestion pipeline separately and emits a normal text message via a downstream code path (separate from chat hub turn). NOT a chat hub entry. | (sources persisted, no agent turn) |
| `internal/telegram/voice_handler.go` | `voiceHandler.onVoiceMessage` | Audio entry point; persists OGG, runs whisper, then calls `AfterTranscribeHook` (which is the chat hub entry above) | (sources persisted, agent turn dispatched) |
| `internal/channels/telegram/inbound.go` | `Inbound.Normalize` | Translates `tele.Context` → `chat.InboundMessage`. Handles text + ask_user callback. Stashes original `tele.Context` under `ChannelData["tele_context"]` so outbound can drive edits | `chat.InboundMessage` |

## 2. Web inbound handlers (chat API)

| File | Function | Role | Produces |
|---|---|---|---|
| `internal/api/router.go` | `NewRouter` mux | Single route: `POST /chat` → `handleChat(deps)` (bearer-gated by `auth.RequireBearer`) | `http.Handler` |
| `internal/api/chat.go` | `handleChat` | JSON-decodes `ChatRequest{user_id, message, thread_id}`, checks `identity.CapabilityAPIChat`, calls `deps.Chat.Chat(ctx, userID, threadID, message)` | `ChatReply{reply, elapsed_ms, llm_calls, tool_calls, tokens, tools_used}` |
| `internal/api/chat.go` | `ChatService` interface | The narrow contract /chat depends on — `Chat(ctx, userID, threadID, message) (ChatReply, error)` | (no SSE; one round-trip) |
| `cmd/aura/web_chat.go` | `apiChatServiceAdapter.Chat` | Translates from `api.ChatService` shape to `webadapter.ChatService.Chat`, then commits/rolls back the web session map | `api.ChatReply` |
| `internal/channels/web/chat_service.go` | `ChatService.Chat` | Constructs `chat.InboundMessage{Channel: ChannelWeb, Mode: DeliveryModeDeferred}` INLINE (no `InboundAdapter.Normalize`), then `hub.ReceiveMessage` + `router.Reserve` + `buf.Wait` | `ChatReply` (mirror shape of `api.ChatReply`) |

**Asymmetry:** the Telegram path goes through a real `chat.InboundAdapter.Normalize`; the Web path bypasses it. There is no `webadapter.Inbound` type. The "normalize" step for web is hardcoded inside `ChatService.Chat`.

## 3. Telegram outbound (LLM/tool result → Telegram)

Two cooperating writers + one adapter:

| File | Function | Role |
|---|---|---|
| `internal/channels/telegram/outbound.go` | `Outbound.Deliver` | Logs operational events only (EventError + EventUsage). Does NOT deliver content. |
| `internal/channels/telegram/outbound.go` | `Outbound.ConsumeStream` | The real delivery path. Reads tokens from `llm.Stream`, progressively edits a Telegram message, applies markdown→entities rendering, throttles edits (600ms), prepends CoT (`🧠 _reasoning_`), prepends status pane footer, dispatches TTS on the final delta (when voice_only mode is on), handles MESSAGE_TOO_LONG fallback. Called from `streamingChatClient.Chat`. |
| `internal/channels/telegram/outbound.go` | `Outbound.maybeSpeak` | Per-chat voice mode lookup + pocket-tts synthesis + `tele.Voice` send + voice_dispatches SQLite recording. Suppresses text reply when mode=voice_only. |
| `internal/channels/telegram/chat_client.go` | `streamingChatClient.Chat` | The `agent.ChatClient` that the InvocationBuilder injects into the agent loop. Each iteration calls `llmc.Stream` then `outbound.ConsumeStream`. |
| `internal/channels/telegram/status_pane.go` | `statusPane.OnToolStart/OnToolEnd/EnterContentMode/Refresh` | In-flight tool indicator (🛠 …) rendered into the placeholder during the tool loop; switches to "footer" mode once content streaming starts. Throttle: 600ms shared with Outbound via `MarkEdited` mutual notification. Italian copy locked. |
| `internal/telegram/entity_markdown.go` | `renderForTelegramEntities` | Markdown → telebot entities + 4096-UTF16 split using `eekstunt/telegramify-markdown-go`. Strips heading symbols, normalizes bullets. |
| `internal/telegram/entity_messages.go` | `sendAssistantToRecipient`, `editAssistantMessage`, `sendAssistantRemainder` | Send/edit/multi-message helpers with plain-text fallback when entity rendering errors. |
| `internal/telegram/atomic_tables.go` | `rewriteAtomicTables` + `applyAtomicTables` | GFM table preprocessing: replaces ASCII grids with our own renderer because telegramify wraps them in `<pre>` which scrolls badly on mobile. |
| `internal/channels/telegram/invocation_builder.go` | OnEvent `agent.EventFinal` branch | Non-streaming delivery path: when the run ends with `event.Delivered == false` (terminal_tool, error, finalization) edit the placeholder with the final text. Also handles `b.NotifySoftBudget`, conversation archive append, `convCtx.CompactCompletedToolResults`, `convCtx.EnforceLimit`. |
| `internal/telegram/conversation_terminal.go` | `Bot.FinalizeTerminalTool` | When terminal tool fires, calls `agent.FinalizeAfterTerminalTool` for a prose summary then edits placeholder with rendered output. |

## 4. Web outbound (chat API → web client)

| File | Function | Role |
|---|---|---|
| `internal/channels/web/outbound.go` | `Router.Deliver` | Receives every `chat.OutboundEvent` for ChannelWeb; routes by `ev.RunID` to a per-run `Buffer`. Lazy-creates buffer on first event (handles race where event arrives before `Reserve`). |
| `internal/channels/web/outbound.go` | `Buffer.apply` | Folds events into `Result{FinalContent, Delivered, LLMCalls, ToolCalls, TokensTotal/Prompt/Completion, CostUSD, ElapsedMs, TerminalTool, ToolsUsed, Error, Status}`. Only EventMessageDelta + EventMessageDone touch `FinalContent`; EventUsage carries stats; EventToolStart accumulates `ToolsUsed`. |
| `internal/channels/web/outbound.go` | `Buffer.Wait` | Blocks until EventDone arrives or ctx cancels. |
| `internal/channels/web/chat_service.go` | `ChatService.Chat` | Orchestrates: build msg → `Reserve(?)` (no run_id yet) → `hub.ReceiveMessage` → after it returns, `Reserve(run.ID)` again → `buf.Wait` → returns ChatReply. Bug-prone: events that arrive before `Reserve(run.ID)` rely on Router's lazy-create branch. |

**Format:** JSON body shape `ChatReply{reply, elapsed_ms, llm_calls, tool_calls, tokens, tools_used[]}`. No SSE. No streaming. No citations field. No tool-result preview. No CoT (reasoning is discarded).

## 5. Duplication clusters

### Cluster A — InvocationBuilder construction (~70%)
- Both call `conversation.LoadPromptOverlay(cfg.PromptOverlayPath)`. Web does it via `conversation.RenderSystemPrompt(...)` instead, missing the overlay file → **regression**.
- Both build pinned operational lessons: `b.memoryStore.FetchRecentOperational(ctx, 10)` + `memoryindex.OperationalLessonsBlock(...)` + `priorityCaches.LoadOrStore` keyed by threadID. Identical code, separately reimplemented (one in `internal/channels/telegram/invocation_builder.go`, one in `cmd/aura/web_chat.go`).
- Both call `memoryindex.RenderPrioritySection(ctx, store)` and `cache.Render(ctx, store, turnIdx)` and `cache.InvalidatePinSection()` identically.
- Both build `postTurn := agent.NewHeuristicPostTurnConfig(...)` + `agent.NewMemoryJudgeHook(...)` identically.
- Both build `agent.Invocation{...}` with overlapping but DIVERGENT field sets — web omits: TerminalToolPolicy, BeforeTool (DuplicatePolicy), PhantomToolGuard with ToolNamesFn, BeforeLLM (budget check), RecordUsage, EstimateCost, TerminalHandler, OnEvent (entire), Toolset, PromptVersion/Hash/Modules.
- Both compute `toolDefs`. Telegram uses `MakeToolsProvider` with Qdrant top-K search and `ToolSearchTopK`; Web uses static `deps.Tools.DefinitionsFor(allowlist)`. **Web cannot use tool-search retrieval today.**

### Cluster B — webChatSessions vs agent.SessionStore (~100%)
- Both serve the same purpose: per-(user, thread) message history with idle eviction.
- `webChatSessions.gcLocked` mirrors `agent.SessionStore.OnEvict` behavior.
- `webChatSessions.begin/commit/rollback` is a manual two-phase commit on top of the chat hub run lifecycle that `agent.SessionStore` already handles natively (`session.Finish()`).
- `webAgentState` reimplements `agent.State` over a flat `[]llm.Message` and has no `TrackTokens`, no `SetSystemMessage` (it overwrites in begin), no `SetSearchContext`, no `CompactCompletedToolResults`, no `EnforceLimit`, no `SetAgentNote`. **Functional gap, not just code dup.**

### Cluster C — Tool executor (~100%)
- `webToolExecutor.ExecuteToolCalls` (cmd/aura/web_chat.go:398-498) re-implements `agent.ExecuteToolCalls` with: goroutine-per-call dispatch, ctx-cancel select, attempts repo recording, error classification, fatal/non-fatal disambiguation, terminal-tool detection, ReadSkillNames accumulation, awaiting-user-input handling, token-juice compaction.
- The Telegram path delegates to `agent.ExecuteToolCalls` (called from `InvocationBuilder.executeToolCalls`). They are NOT byte-identical: webToolExecutor uses `agent.WrapUntrustedToolResult` + `limitToolContent`; telegram path uses the registry's standard wrapper. Minor divergence enough to cause subtle test/prod skew.

### Cluster D — Hub wiring (3× duplication)
- Hub #1 (Telegram): `internal/channels/telegram/invocation_builder.go::NewHub` — uses `b.AgentNoteStore()`, `b.MemoryStore()` directly off `*telegram.Bot`.
- Hub #2 (Web): `cmd/aura/web_chat.go::newHubBackedWebChatService` — uses `deps.MemoryStore` off `telegram.Deps` (which contains the same memoryStore).
- Hub #3 (Cron): `cmd/aura/app_wire.go::wireBot` lines ~358-367 — separate `chat.New(...)` for cron tasks.

Three Hubs in one process. Each has its own AgentLoopAdapter wrapping a different InvocationBuilder. The only thing they share is `chat.Hub`'s lifecycle plumbing (LifecycleStore = `deps.RunStore`).

### Cluster E — Stats/usage event accumulation (~50%)
- `internal/chat/agentloop.go::Run` emits EventUsage carrying llm_calls, tool_calls, loop_steps, tokens_*, cost_usd, terminal_tool.
- Web's `Buffer.apply` reads them via `ev.Payload["llm_calls"].(int)` — string keys with type assertion.
- Telegram's `Outbound.Deliver` reads them via `ev.Payload[k]` for the same keys — string keys with type assertion (slightly different — Telegram only logs, Web stores).
- Same keys hardcoded in 3 places (agentloop emit + 2 consumers). Future addition of a stat = touch 3 files.

## 6. Feature drift — concrete diffs

The user memory note (project_web_telegram_parity_full_fix.md) listed 7 features deferred. Verified state on master:

| Feature | Telegram | Web | Verdict |
|---|---|---|---|
| **Streaming** | Progressive Telegram edits via `Outbound.ConsumeStream`, throttled 600ms | Buffered one-shot via `Buffer.Wait`; no SSE/WS | MISSING on web |
| **Voice (TTS)** | `Outbound.maybeSpeak` → pocket-tts synth → tele.Voice; voice mode policy per chat | none; web ChatReply has no audio | MISSING on web |
| **Markdown render** | `telegramify-markdown-go` → tele.Entities + 4096-UTF16 split + atomic-table preprocessing | none — raw markdown text in JSON; client-side render | DIVERGENT (acceptable shape, but no shared markdown contract) |
| **Soft-budget notice** | `b.NotifySoftBudget(c, userID)` fires when llm_calls > 0 && terminal_tool == "" | not wired — web silently exceeds and only reports cost_usd in stats | MISSING on web |
| **Context compaction** | `convCtx.CompactCompletedToolResults` (MaxChars=1200, KeepRecentFull=2) + `convCtx.EnforceLimit` post-turn | not wired — `webAgentState` is a flat slice with no compaction; `trimWebMessages` is a fixed-size tail cap (30) | DEGRADED on web |
| **`tools_allowed`** (registry slice) | Set via `agent.MakeToolsProvider` w/ tool-search Qdrant top-K=5 retrieval | Static `deps.Tools.Names()` allowlist, no tool-search retrieval | DEGRADED on web — large registries leak full tool list into context every turn |
| **`tools_used`** (response field) | Captured via `agent.TurnStats.ToolsCalled` from EventToolEnd; logged by InvocationBuilder OnEvent EventFinal | Captured via `Buffer.apply` EventToolStart accumulator into `ToolsUsed[]` | OK on both, redundant accumulators |

Additional drift discovered during audit (not in the memory note):
- **Voice transcript inbound** (Telegram has it via `buildAfterTranscribeHook`; web has no file/audio upload that triggers an agent turn).
- **`ask_user` UI** (Telegram renders question with inline keyboard via `askUserQuestionMarkup`; web has no question prompt — the `EventQuestionRequested` event is silently dropped by Buffer.apply).
- **Pending question resume** (Telegram has `prepareAskUserResumeInput` + `RecordQuestionAnswer`; web has nothing — a follow-up message after a question_requested would just become a fresh turn).
- **`agent_note` injection** (Telegram calls `convCtx.SetAgentNote(...)` from `AgentNoteStore.Get`; web does not).
- **Hot-reload of system prompt after `file` tool write to overlay** (Telegram detects `overlayWriteInCalls(calls)` and re-composes `promptPlan`; web does not).
- **Per-thread `priorityCaches`** exists in both but each Hub has its own — caches are per-process, not per-feature, so the "thread" cache key collides if telegram and web users share the same id-suffixed thread string (unlikely today because of `web:` prefix, but undefined).
- **Conversation archive** (Telegram appends a row per turn to `conversations` table; web turns are not archived; `cmd/quality_bench` cannot cross-reference web bench turns with their transcript).
- **Sandbox / sender (DocumentSender / TokenSender)** (`b.SendDocumentToUser`, `b.SendToUser` exist for telegram so tools like `request_dashboard_token` and `create_xlsx` can push a file; web has no equivalent and these tools either fail or rely on the Telegram bot being live).
- **Budget gate** (Telegram checks `bgt.IsHardBudgetExceeded` + `bgt.CanAfford` pre-turn; web does not).
- **`tele.Typing` indicator** (Telegram pulses Typing every 4s; web has no UX equivalent in the buffered shape, but a future SSE/WS path would need it).

---

# PART 2 — TARGET SHAPE

## Design constraints

1. `chat.Hub` + `chat.InboundAdapter` + `chat.OutboundAdapter` + `chat.AgentLoop` are KEPT — they are correct, only one Hub instance should exist per process (we have 3 today).
2. The duplication is in the **InvocationBuilder** layer, not the chat-event layer. The fix is **one shared InvocationBuilder** in a new package `internal/agentcore` that both transports use.
3. Transport-specific concerns (markdown render, TTS, throttled edits vs JSON buffer, document upload) stay in `internal/channels/<transport>` as adapters. The boundary is `chat.OutboundEvent` — already in place.
4. Streaming on web becomes a NEW `webadapter.StreamingOutbound` registered for `(ChannelWeb, DeliveryModeStreaming)` that converts the same OutboundEvent stream to SSE frames. The buffered Router stays for `(ChannelWeb, DeliveryModeDeferred)` for non-stream clients.

## Target interfaces (Go, minimal)

The `chat` package interfaces are already minimal and correct:

```go
// internal/chat/types.go — KEEP AS IS
type InboundAdapter interface {
    Channel() Channel
    Normalize(ctx context.Context, raw any) (InboundMessage, error)
}

type OutboundAdapter interface {
    Channel() Channel
    Mode() DeliveryMode
    Deliver(ctx context.Context, event OutboundEvent) error
}

type AgentLoop interface {
    Run(ctx context.Context, run *Run, msg InboundMessage, emit EmitFn) error
}
```

What's MISSING and should be added: a single shared **`agentcore.InvocationBuilder`** that today is duplicated. This is the new contract:

```go
// internal/agentcore/builder.go (NEW)
package agentcore

// Builder constructs an agent.Invocation for one chat hub turn. There is
// exactly ONE production implementation; transports compose it with their
// per-channel hooks via Options.
type Builder struct {
    cfg            *config.Config
    llm            llm.Client
    tools          *toolregistry.Registry
    skills         *skills.Loader
    wiki           wiki.Repository
    sessions       *agent.SessionStore
    memoryStore    *memoryindex.Store
    agentNoteStore *agentnote.Store
    attemptsRepo   attempts.Repo
    budget         budget.Runtime
    archive        conversation.ArchiveRepository
    archiver       conversation.TurnAppender
    logger         *slog.Logger
    loc            *time.Location
}

// PerTurnHooks lets a transport plug in surface-specific behavior without
// owning invocation construction. Each hook is optional (nil = no-op).
type PerTurnHooks struct {
    // OnPlaceholderReady fires after the initial reply placeholder is created
    // (Telegram: send "⏳" message; Web SSE: open the stream + emit first frame).
    OnPlaceholderReady func(ctx context.Context, run *chat.Run) (placeholder any, err error)

    // OnStreamChunk fires for every llm.Token in the streaming path. Telegram
    // uses it to drive ConsumeStream + statusPane; Web SSE flushes a frame.
    // Return io.EOF to stop streaming early; any other error propagates.
    OnStreamChunk func(ctx context.Context, run *chat.Run, tok llm.Token) error

    // OnFinal fires when the run reaches EventFinal. Use it to commit the final
    // surface (Telegram: edit placeholder if !Delivered; Web buffered: write
    // ChatReply). Return value is the agent.ChatResponse Delivered flag.
    OnFinal func(ctx context.Context, run *chat.Run, event agent.Event) (delivered bool)

    // OnAskUser fires when the agent emits EventQuestionRequested. Telegram
    // sends inline keyboard; web SSE emits a question frame; web buffered
    // returns the question as the reply body and stores pending question state.
    OnAskUser func(ctx context.Context, run *chat.Run, payload map[string]any) error

    // OnSoftBudget fires when soft budget is reached.
    OnSoftBudget func(ctx context.Context, run *chat.Run)

    // ChatClient is the transport's choice of LLM driver. Telegram passes
    // streamingChatClient (drives ConsumeStream); Web passes either
    // NewNoStreamClient (buffered) or a new NewSSEStreamingClient (SSE).
    // REQUIRED — Builder errors out if nil.
    ChatClient agent.ChatClient
}

// Build is the single InvocationBuilder. Transports register a thin chat.AgentLoop
// that wraps Build(...) with their PerTurnHooks.
func (b *Builder) Build(ctx context.Context, run *chat.Run, msg chat.InboundMessage, hooks PerTurnHooks) (agent.Invocation, error)
```

The `chat.InvocationBuilder` typedef (`func(ctx, run, msg) (agent.Invocation, error)`) becomes a closure each transport composes from its own hooks:

```go
// transport-side wiring (example for web)
loop, _ := chat.NewAgentLoopAdapter(func(ctx context.Context, run *chat.Run, msg chat.InboundMessage) (agent.Invocation, error) {
    hooks := webHooks(msg) // builds OnStreamChunk → SSE, OnFinal → buffer commit, etc.
    return agentcore.Build(ctx, run, msg, hooks)
})
```

## Why this shape

- `agentcore.Builder` owns the 70% that's identical: prompt overlay, pinned operational, skills block, tool manifest, wiki TOC, conversation context, budget gate, post-turn config, archive, agent_note, compaction. Both transports get all of these for free.
- `PerTurnHooks` is the **only** transport-shaped surface — small enough to inspect, every hook has a clear single responsibility, all optional except `ChatClient`.
- No more `*tgtelegram.Bot` reaching into `internal/channels/telegram` (the current cyclic-dependency-via-`SetHub` workaround in `NewInvocationBuilder` goes away — `agentcore.Builder` is constructed from `cmd/aura/app.go` and passed to both transports as a value).
- The `Outbound`/`Inbound` adapter pair stay where they are; their contract with `agentcore` is via `PerTurnHooks` not the adapter interface, so the existing `chat.OutboundAdapter` lifecycle is untouched.
- Streaming on Web requires NO additional interface — it's a different `ChatClient` impl (one that flushes SSE frames as each `llm.Token` arrives) wired into the same hooks.

## What goes away

- `cmd/aura/web_chat.go::webInvocationBuilder` (612 LOC) — replaced by ~80 LOC of hook wiring.
- `cmd/aura/web_chat.go::webChatSessions` / `webAgentState` (~150 LOC) — replaced by `agent.SessionStore` + `conversation.Context`.
- `cmd/aura/web_chat.go::webToolExecutor` (~120 LOC) — replaced by direct call to `agent.ExecuteToolCalls`.
- `internal/channels/telegram/invocation_builder.go::InvocationBuilder.Build` (688 LOC) — replaced by ~120 LOC of hook wiring (telegram-specific UI: typing indicator, statusPane, ask_user keyboard, placeholder).
- Two of three Hubs collapse into one (the cron Hub stays separate — its lifecycle is genuinely different: triggered by scheduler tick, no user surface, no streaming).

---

# PART 3 — MIGRATION PLAN (4–8 atomic Ralph stories)

Stories are sequenced so each one ships independently with green tests, and the consolidation only finalises at the last story. Each story = 1 commit per the user's master-direct workflow.

## US-CONS-01 — extract `internal/agentcore` package skeleton + share prompt overlay path
- **Scope**: New package `internal/agentcore` with `Builder` struct + `PerTurnHooks` + `Build` signature. Move the prompt-overlay + pinned-operational + skills + tool-manifest + wikiTOC composition out of `internal/channels/telegram/invocation_builder.go` into `agentcore.composePrompt`. Both existing builders call it.
- **Files touched**: NEW `internal/agentcore/builder.go`, `internal/agentcore/prompt.go`; MODIFY `internal/channels/telegram/invocation_builder.go` (delete duplicated composition, call agentcore), MODIFY `cmd/aura/web_chat.go` (call agentcore.composePrompt instead of `conversation.RenderSystemPrompt`).
- **LOC delta**: +180 / -240 = **-60 LOC**.
- **Acceptance**:
  - `go build ./...` clean.
  - `go test ./internal/agent/... ./internal/channels/... ./internal/api/...` green.
  - Probe: `go run ./cmd/probe_chat -case wiki_subgraph` returns identical reply on both telegram (live bot) and web (via /api/chat) — verify by capturing both transcripts and diffing.
  - Web `/api/chat` reply length grows by ≥30% on a benchmark turn (because overlay file is now actually loaded — current web has BUG: system prompt is `conversation.RenderSystemPrompt` only, no overlay).
- **Dependency**: none, first story.

## US-CONS-02 — collapse webChatSessions + webAgentState onto `agent.SessionStore` + `conversation.Context`
- **Scope**: Replace `cmd/aura/web_chat.go::webChatSessions` with calls to the shared `agent.SessionStore` (using a "web:<userID>:<threadID>" key). Replace `webAgentState` with `conversation.Context` so web gets `TrackTokens`, `CompactCompletedToolResults`, `EnforceLimit` for free. Maintain backwards compat: existing sessions don't survive the migration (eviction is OK).
- **Files touched**: MODIFY `cmd/aura/web_chat.go` (remove webChatSessions, webAgentState, trimWebMessages; rewire `webInvocationBuilder.Build` to begin a `*agent.Session` and use its `Conversation()`); MODIFY `internal/channels/web/chat_service_test.go` (update fakes).
- **LOC delta**: +60 / -240 = **-180 LOC**.
- **Acceptance**:
  - `go test ./internal/channels/web/... ./cmd/aura/...` green.
  - Probe: send 3 turns to `/api/chat` with same thread_id; verify turn 3's prompt includes context from turns 1+2 (today it does via `webChatSessions`, but the message count + token tracking must now match Telegram's `convCtx.TotalTokensUsed()` accounting).
  - New probe: send a turn that produces a huge tool result; verify `CompactCompletedToolResults` fires (assert: prompt for round N+1 has tool_result message length ≤ 1200 chars).
- **Dependency**: US-CONS-01.

## US-CONS-03 — collapse `webToolExecutor` onto `agent.ExecuteToolCalls`
- **Scope**: Delete `webToolExecutor`, replace its only call site with `agent.ExecuteToolCalls(ctx, deps.Tools, convCtx, userID, conversationID, calls, terminalPolicyEnabled, logger, opts...)`. Add the missing `WithToolAttemptRecording` + `WithTokenJuice` options. Verify identical behavior on `awaiting_user_input`, `terminal_tool` detection, fatal-error wrapping.
- **Files touched**: MODIFY `cmd/aura/web_chat.go` (delete webToolExecutor + helpers); MODIFY `cmd/aura/web_chat_test.go`.
- **LOC delta**: +30 / -180 = **-150 LOC**.
- **Acceptance**:
  - `go test ./internal/agent/... ./cmd/aura/...` green.
  - Probe parity: run the QA-phase probe suite (`cmd/probe_chat/qa_phase_cases.go`) end-to-end on both transports; failure classes must be identical (fatal vs retriable) and `tools_used[]` must match.
- **Dependency**: US-CONS-02.

## US-CONS-04 — promote `agentcore.Builder` to own the full agent.Invocation construction
- **Scope**: Move the agent.Invocation construction (Options, ToolsProvider, BeforeTool/BeforeLLM/RecordUsage/EstimateCost/TerminalHandler, PhantomToolGuard, PostTurn, MaxIterations) out of both InvocationBuilders into `agentcore.Builder.Build`. Both transports now only supply `PerTurnHooks{ChatClient, OnPlaceholderReady, OnStreamChunk, OnFinal, OnAskUser, OnSoftBudget}`.
- **Files touched**: NEW `internal/agentcore/invocation.go`; MODIFY `internal/channels/telegram/invocation_builder.go` (shrink to hooks); MODIFY `cmd/aura/web_chat.go` (shrink to hooks); MODIFY `internal/channels/telegram/chat_client.go` (export streamingChatClient as the telegram hook's `ChatClient`); NEW `cmd/aura/web_chat_hooks.go` for the web-side hooks.
- **LOC delta**: +420 / -780 = **-360 LOC**.
- **Acceptance**:
  - `go test ./...` green including the fixture byte-parity test (`internal/channels/telegram/fixture/byte_parity_test.go`).
  - `golangci-lint run ./...` shows no new findings on touched files.
  - `dupl -t 60 internal/agentcore/ internal/channels/ cmd/aura/web_chat*.go` reports no duplicate cluster ≥60 tokens.
  - `wc -l` of each touched file ≤600 LOC (current god-classes: `internal/channels/telegram/invocation_builder.go` 688 LOC, `cmd/aura/web_chat.go` 612 LOC — BOTH violate CLAUDE.md's 600-LOC rule today).
- **Dependency**: US-CONS-03.

## US-CONS-05 — single Hub: collapse web + telegram Hubs into one
- **Scope**: Move Hub construction out of `internal/channels/telegram/invocation_builder.go::NewHub` AND `cmd/aura/web_chat.go::newHubBackedWebChatService` into `cmd/aura/app.go` (composition root). Register both inbound adapters (`telegramadapter.New()` + new `webadapter.New()`) and both outbound adapters (`telegramadapter.Outbound` + `webadapter.Router`) on the SAME Hub. Cron Hub stays separate.
- **Files touched**: MODIFY `cmd/aura/app.go` (add Hub construction); MODIFY `cmd/aura/app_wire.go` (route the existing wireBot calls to share the new Hub); MODIFY `internal/channels/telegram/invocation_builder.go` (delete `NewHub`, keep only the Build function); MODIFY `cmd/aura/web_chat.go` (delete Hub construction, take an injected `*chat.Hub`); NEW `internal/channels/web/inbound.go` (currently absent — webadapter has no InboundAdapter).
- **LOC delta**: +120 / -180 = **-60 LOC**.
- **Acceptance**:
  - `go test ./... -run TestHub` green.
  - Hub round-trip: send a Telegram message; then send a web message with the same thread_id (now: "web:<uid>:default"); both must increment the same `LifecycleStore` run counter (one run per dispatch).
  - `ChannelTelegram` + `ChannelWeb` + `ChannelCron` (and ChannelHeartbeat, ChannelSwarm) all visible in `chat.Hub.outbound` map.
- **Dependency**: US-CONS-04.

## US-CONS-06 — feature parity wave 1: soft-budget + context compaction + archive on web
- **Scope**: Wire the missing telegram-side post-turn features onto web via the shared `agentcore.Build` (they were already inside Build after US-CONS-04). Verify: web turns now end with `convCtx.EnforceLimit`, `CompactCompletedToolResults`, soft-budget notice (via `OnSoftBudget` hook that writes a `budget_warning` field into the web ChatReply for the client to render), conversation archive append.
- **Files touched**: MODIFY `internal/api/chat.go` + `internal/api/types.go` (add `BudgetWarning string` to ChatReply); MODIFY `internal/channels/web/chat_service.go` + `internal/channels/web/outbound.go` (capture EventUsage → BudgetWarning); MODIFY `cmd/aura/web_chat_hooks.go` (wire OnSoftBudget); MODIFY `web/src/api/chat.ts` (read budget_warning) — TS change is a follow-up if web dashboard is non-trivial; for this story emit the field and stop.
- **LOC delta**: +120 / -0 = **+120 LOC** (net additive; this is a missing-feature story, not a dedup).
- **Acceptance**:
  - `go test ./internal/channels/web/... ./internal/api/... ./internal/agent/...` green.
  - Probe: drive web until `soft_budget` triggers (mock budget runtime); verify `ChatReply.budget_warning` is non-empty.
  - Probe: verify `conversations` SQLite table has rows tagged `channel=web` after web turns (today it has only telegram).
- **Dependency**: US-CONS-05.

## US-CONS-07 — feature parity wave 2: streaming on web (SSE)
- **Scope**: Add `internal/channels/web/streaming_outbound.go` registering `(ChannelWeb, DeliveryModeStreaming)`. Add `POST /chat/stream` (SSE) endpoint that opens a long-lived connection and flushes one SSE frame per `chat.OutboundEvent`. Web ChatClient becomes a new `NewStreamingChatClient(deps.LLM)` that flushes `llm.Token` chunks into the OutboundEvent stream via `OnStreamChunk`. Keep `POST /chat` (buffered) operational.
- **Files touched**: NEW `internal/channels/web/streaming_outbound.go`; NEW `internal/channels/web/sse_chat_client.go`; MODIFY `internal/api/router.go` + `internal/api/chat.go` (add POST /chat/stream handler); MODIFY `cmd/aura/web_chat_hooks.go` (branch on `msg.Mode` to select streaming vs buffered ChatClient).
- **LOC delta**: +280 / -0 = **+280 LOC** (additive feature).
- **Acceptance**:
  - `go test ./internal/channels/web/...` green incl. SSE-frame assertion test (assert: at least 1 EventMessageDelta frame arrives before EventDone for a multi-token reply).
  - Probe: `curl -N -H "Authorization: Bearer $TOK" -d '{"message": "what is in my wiki"}' http://localhost:18080/api/chat/stream` produces a stream of `data: {...}\n\n` frames, each a `chat.OutboundEvent` JSON.
  - Throughput: first byte arrives in <500ms; final byte within p95 ≤8s on the cached `wiki_subgraph` query.
- **Dependency**: US-CONS-06.

## US-CONS-08 — feature parity wave 3: voice + ask_user on web (deferred / optional)
- **Scope**: Wire TTS synthesis on web as an optional response field (`audio_url` pointing to an in-memory cache the client can fetch). Wire `EventQuestionRequested` to a `pending_question` ChatReply field for the buffered path + a typed SSE frame for the streaming path. Wire follow-up POST `/chat/answer/{question_id}` for sending an ask_user reply on the buffered path; SSE path uses the same `pending_question` -> follow-up `chat/stream` flow.
- **Files touched**: NEW `internal/channels/web/voice_outbound.go` (calls existing pocket-tts client + serves via a TTL-bounded in-memory cache); MODIFY `internal/api/chat.go` + `internal/channels/web/outbound.go` + `cmd/aura/web_chat_hooks.go` (OnAskUser hook + audio dispatch); NEW endpoint `GET /chat/audio/{cache_id}`.
- **LOC delta**: +320 / -0 = **+320 LOC** (additive).
- **Acceptance**:
  - Probe: web request with `?voice=all` query param returns ChatReply with `audio_url`; subsequent GET fetches OGG audio bytes; `bytes > 1024 && content-type=audio/ogg`.
  - Probe: agent calls `ask_user`; ChatReply contains `pending_question{id, question, options[], kind}`; follow-up POST `/chat/answer/{id}` resumes the same run; final ChatReply contains the post-answer reply (assert: agent received the option as a tool_result).
- **Dependency**: US-CONS-07.

## Story totals

| Story | LOC delta |
|---|---|
| US-CONS-01 | -60 |
| US-CONS-02 | -180 |
| US-CONS-03 | -150 |
| US-CONS-04 | -360 |
| US-CONS-05 | -60 |
| **DEDUP TOTAL** | **-810 LOC** |
| US-CONS-06 (feature parity wave 1) | +120 |
| US-CONS-07 (streaming on web) | +280 |
| US-CONS-08 (voice + ask_user on web) | +320 |
| **PARITY TOTAL** | **+720 LOC** |
| **NET** | **-90 LOC** with ALL 7 deferred features shipped |

---

# PART 4 — RISKS

## R1 — Telegram session/state migration during US-CONS-02
Telegram already uses `agent.SessionStore`; the risk is only on the **web** side. Live in-flight web sessions (from `webChatSessions`) will not survive the deploy — users mid-conversation lose the context window. Mitigation: deploy off-hours; the existing `webChatIdleTTL = 30 * time.Minute` means most sessions self-evict within 30 min anyway. NOT a backward-compat issue for any persistent SQLite row (web session state is in-process only).

## R2 — Telegram rate-limit semantics during US-CONS-04
Telegram's 600ms throttle is enforced inside `Outbound.ConsumeStream` + `statusPane.flushLocked`. The shared `agentcore.Builder` must NOT introduce a parallel write path. The PerTurnHooks `OnStreamChunk` for Telegram MUST be the existing `Outbound.ConsumeStream` driver, not a re-implementation. Mitigation: keep the byte-parity fixture test (`internal/channels/telegram/fixture/byte_parity_test.go`) GREEN throughout — it captures the exact wire sequence of tele.Bot calls for representative scenarios.

## R3 — Web SSE/WebSocket choice (US-CONS-07)
SSE is the right call (simpler than WS for one-way server→client streaming, plays nicely with bearer auth via query string fallback for the EventSource API). WebSocket would need a separate auth handshake. Risk: SSE is sensitive to proxy buffering (nginx default `proxy_buffering on`). Mitigation: emit `X-Accel-Buffering: no` header + flush after every frame; document the nginx requirement in `docs/deploy.md`. The current Aura Docker stack does NOT have nginx; this only matters for reverse-proxy deploys.

## R4 — Voice synth timing on web (US-CONS-08)
Pocket-tts is the same client Telegram uses — italian_24l INT8 RTF 0.61, first-chunk ~200ms. Web's buffered shape will block on full synthesis (~3-5s for a typical reply). Mitigation: serve the OGG via a separate GET endpoint (TTL cache, 60s) so the JSON ChatReply returns immediately with `audio_url` and the client fetches async. Streaming SSE path can flush an `audio_ready` frame when synth completes.

## R5 — User-visible regression during US-CONS-04
The biggest commit (-360 LOC, two builders collapse). Mitigation:
- Ship behind a feature flag `AURA_AGENTCORE_BUILDER=true` for 1 week of live traffic before deleting the legacy code path.
- Add a transcript comparison probe: same query through both old and new builders must produce identical tool-call sequence (same names, same arg_keys) — the response text may vary by ≤5% (LLM nondeterminism).
- Keep `byte_parity_test.go` GREEN every commit.

## R6 — Hub merging introduces cross-channel event leakage (US-CONS-05)
Today, Telegram's Hub has only Telegram outbound adapters; merging means EventDone for a web run could route to a Telegram outbound if `outboundKey{Channel, Mode}` lookup mismatches. Mitigation: `Hub.makeEmit` already uses `outboundKey{Channel: run.Channel, Mode: mode}` — fan-out is channel-scoped. Add a test that explicitly creates a Hub with both Telegram + Web outbounds and verifies a ChannelWeb run dispatches only to the Web outbound (zero deliveries to Telegram outbound).

## R7 — Three identical `priorityCaches sync.Map` survive consolidation
Today each InvocationBuilder has its own `priorityCaches` (Telegram, Web). After consolidation they're one cache in `agentcore.Builder`. Thread keys must include channel prefix to prevent collision (already done: Telegram uses raw `<chat_id>`, Web uses `web:<userID>:<threadID>`). Mitigation: add a test that exercises both transports with overlapping threadIDs and asserts cache isolation.

## R8 — Test fixture redundancy
`internal/channels/telegram/fixture/byte_parity_test.go` + `internal/channels/web/chat_service_test.go` + `internal/chat/hub_test.go` have overlapping coverage. Risk: refactor breaks one but not the others, and CI passes. Mitigation: after US-CONS-04, audit for redundant cases and delete obsoleted ones in the SAME commit (CLAUDE.md deep-refactor-on-touch rule).

## R9 — `ChannelData` privacy invariant
`InboundMessage.ChannelData` is a `map[string]any` that today carries `tele.Context` for telegram. Web has none. After consolidation, web will need to carry an `http.ResponseWriter` + `*http.Request` for the SSE path — both are channel-private. The invariant ("agent runtime MUST NOT read ChannelData") must continue to hold; only the per-transport hooks read it. Mitigation: add an assertion test that calls `agentcore.Build` with a poisoned ChannelData and verifies `agent.Invocation` doesn't carry any of its values into `agent.Run` outputs.

## R10 — Soft-budget noise on web (US-CONS-06)
Web clients don't have the "single placeholder you keep editing" UX, so a soft-budget warning will show up as a separate visible field in JSON. Risk: client devs render it as a banner that flashes on every cached reply. Mitigation: only emit `budget_warning` when `llm_calls > 0` (matching telegram's `NotifySoftBudget` condition); the React client should treat it as a passive metadata field, not a modal.

---

## Closing notes

The plan above is the minimum needed to satisfy the user's mandate. The substrate (`internal/chat`) is correct and stays. The cleanup is concentrated in 5 stories (-810 LOC) and feature parity is 3 additive stories (+720 LOC). The biggest single-commit risk is US-CONS-04 (the big collapse) — gate it behind a feature flag for 1 week. The smallest first win is US-CONS-01 — it ships in ≤200 LOC of touched code and immediately fixes the BUG that web doesn't load the AGENT.md/SOUL.md prompt overlays.
