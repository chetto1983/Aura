# Channel Surface — 2026-05-18 — git_head: 3652fdf2

This document maps all four inbound/outbound channels in Aura: their entry points, delivery mechanisms, identity authorization, error paths, and user-visible failure UX.

---

## Telegram

- **Inbound entry**: `internal/telegram/handlers.go:34` — `onMessage()` called by telebot`s `tele.OnText` handler. Validates allowlist, calls `routeTelegramContext()` which acquires a per-user actor via `UserGate.Acquire()` and enqueues a process closure invoking `hub.Receive(chat.ChannelTelegram, tele.Context)`.

- **Outbound delivery**: `internal/channels/telegram/outbound.go:113` — `ConsumeStream()` reads tokens and progressively edits a Telegram message using `bot.Edit()` with entity-rendered markdown. Progressive edits respect 600ms throttle (`streamingEditThrottle`). Fallback: if entity edit fails, retry with plain text. Final edit strips CoT and status footer. Remainder pages sent via `bot.Send()`.

- **Joins chat.Hub at**: `internal/telegram/handlers.go:116` — `hub.Receive(b.telegramHubContext(ctx, userID), chat.ChannelTelegram, c)` inside process closure. Context enriched with actor ID via `identity.TelegramSessionActorID(userID)` and authority via auth database.

- **Authorizer**: `internal/telegram/handlers.go:37` — `isAllowlisted(userID)` checks allowlist before routing. Identity authorization via `hub.telegramHubContext()` → `identity.WithAuthority()`, binding bot`s auth DB.

- **Actor type**: Telegram session actor — `identity.TelegramSessionActorID(userID)`.

- **Error path**:
  - (a) Hub failure: `internal/telegram/handlers.go:129` — logs "hub receive failed" at error level; handler returns nil.
  - (b) LLM client failure: `internal/channels/telegram/chat_client.go:59,63` — returns fallback content "Sorry, I couldn`t process your message. Please try again." with error logged.
  - (c) Tool execution failure: Propagates as `EventError` through Hub to outbound adapter, logged at warn level.

- **Channel-layer retry/backoff**: None; retry delegation to `RetryClient` (5 transient retries with exponential backoff, 3 content retries). Message delivery failures trigger fallback-to-plain-text.

- **User-visible failure UX**: LLM/streaming failure → user sees "Sorry, I couldn`t process your message. Please try again." (internal/channels/telegram/chat_client.go:59,63). Non-allowlisted user → silent drop. Entity render failure → graceful fallback to plain markdown.

---

## Web

- **Inbound entry**: `internal/api/chat.go:50` — `handleChat()` HTTP `POST /chat` handler. Parses JSON `{user_id, message}`, validates message, authorizes via `deps.Auth.Authorize()` with capability `CapabilityAPIChat`, calls `deps.Chat.Chat(chatCtx, userID, message)`.

- **Outbound delivery**: `internal/channels/web/outbound.go:193` — Single `Router` adapter (registered once at boot) routes each `OutboundEvent` to per-RunID `Buffer`. Buffer accumulates events into `Result` struct. `ChatService.Chat()` blocks on `buf.Wait(ctx)` until `EventDone`, returns `ChatReply{Reply, ElapsedMs, LLMCalls, ToolCalls, Tokens}`.

- **Joins chat.Hub at**: `internal/channels/web/chat_service.go:69` — `s.hub.ReceiveMessage(ctx, msg)` with synthetic `InboundMessage` (channel=`ChannelWeb`, mode=`DeliveryModeDeferred`).

- **Authorizer**: `internal/api/chat.go:74` — `deps.Auth.Authorize()` checks capability `CapabilityAPIChat` on resource type "api". Caller context enriched with `identity.WithAuthority()`.

- **Actor type**: Web bearer actor — derived from authenticated user ID or request body user_id.

- **Error path**:
  - (a) Hub failure: `internal/channels/web/chat_service.go:70` — error propagates to handler, returns HTTP 500.
  - (b) LLM client failure: Agent error surfaces as `EventError`, buffered, returned in `ChatReply`.
  - (c) Tool execution failure: Same as LLM — buffered and returned in reply.

- **Channel-layer retry/backoff**: None at adapter layer; LLM retry via `RetryClient`.

- **User-visible failure UX**: Success → HTTP 200 with `ChatReply`. Authorization failure → HTTP 403 "missing api.chat grant". Bad request → HTTP 400. Chat failure → HTTP 200 with `ChatReply` containing error text (caller must inspect to detect failure). Timeout → HTTP 408.

---

## Cron

- **Inbound entry**: `internal/channels/cron/dispatcher.go:23` — `NewHubDispatcher()` wraps `cron.Dispatcher`. On task with `task.Kind == cron.KindAgentJob`, calls `hub.Receive()` which invokes `cronadapter.InboundAdapter.Normalize()` to convert `*cron.Task` to `InboundMessage` with channel=`ChannelCron`, mode=`DeliveryModeSilent`.

- **Outbound delivery**: `internal/channels/silent/outbound.go:66` — `Deliver()` consumes `OutboundEvent`. `EventError` → logs at warn with run_id, thread_id, error text. `EventDone` → logs at info. `EventUsage` → logs at info with stats. No user notification; silent-mode tasks log to structured logs only.

- **Joins chat.Hub at**: `internal/channels/cron/dispatcher.go:25` — `hub.Receive(ctx, chat.ChannelCron, task)`.

- **Authorizer**: `internal/channels/cron/loop.go:52–112` — `CronAgentLoop.delegateCronActor()` calls `DelegateActor()` to create delegated actor with type `ActorTypeCron`. Parent actor required; tool allowlist enforced via delegated capabilities and constraints.

- **Actor type**: Cron delegated actor — `identity.DelegatedActorID(identity.ActorTypeCron, parentActorID, scopeID)`.

- **Error path**:
  - (a) Hub failure: Error returned to cron scheduler; retry logic outside Hub.
  - (b) LLM client failure: Handled inside `CronAgentLoop.Run()`, propagates as error from `runner.RunJob()`.
  - (c) Tool execution failure: Error from `RunJob()`, emitted as `EventError`.

- **Channel-layer retry/backoff**: None; cron scheduler owns retry. LLM-level retry via `RetryClient`.

- **User-visible failure UX**: Success → silent, no message to user, logged at info. Failure → silent, logged at warn. No user notification; operator discovers via log monitoring.

---

## Swarm

- **Inbound entry**: `internal/swarm/hub_bridge.go:38–52` — `HubBridge.Run()` constructs synthetic `InboundMessage` with channel=`ChannelSwarm`, mode=`DeliveryModeSilent`, ParentRunID set to parent run ID, calls `hub.ReceiveMessage(ctx, msg)`. Alternatively, `HubBridge.Dispatch()` (line 59–87) constructs message with ChannelData carrying NodeSpec fields for one-way dispatch.

- **Outbound delivery**: `internal/channels/silent/outbound.go` — Swarm uses silent adapter. Logs errors at warn, completion at info. Result captured in `run.Metadata["final_text"]` for parent to read.

- **Joins chat.Hub at**: `internal/swarm/hub_bridge.go:46` — `hub.ReceiveMessage(ctx, msg)`. Hub records parent→child lineage via `run.Metadata["parent_run_id"]`.

- **Authorizer**: `internal/chat/hub.go:340–351` — Before dispatch, `h.authorizeSwarmDispatch(ctx, msg, actorID)` checks authorization. Failure → `EventError` with payload {"error": "swarm_dispatch_denied"}, status `RunStatusFailed`.

- **Actor type**: Swarm child actor — spawned from parent`s actor context via delegation. Child inherits parent`s authority model.

- **Error path**:
  - (a) Hub failure (authorization denied): `internal/chat/hub.go:341–350` — emits `EventError`, sets status `RunStatusFailed`, returns error to caller.
  - (b) LLM client failure: Propagates as error from `hub.ReceiveMessage()` to `HubBridge.Run()` caller.
  - (c) Tool execution failure: Same as LLM.

- **Channel-layer retry/backoff**: None; swarm manager owns child-run retry policy. LLM-level retry via `RetryClient`.

- **User-visible failure UX**: Success → result in `run.Metadata["final_text"]`, returned as `agent.Result.Content`. Authorization denied → parent receives error, must handle rendering. LLM/tool failure → parent receives error or partial result, must decide retry/escalation; no automatic user notification from Hub.

---

## Cross-Channel Patterns

**Authorization boundary**: All channels invoke authorization at distinct layers — Telegram allowlist + session actor, Web bearer token + capability check, Cron delegated actor with constraints, Swarm pre-dispatch authz + parent→child delegation.

**Error handling strategy**: Hub collects errors into `run.Status` and `run.LastError` (durable), emits `EventError` opaquely, sets terminal status. LLM retry centralized in `RetryClient` (5 transient, 3 content). Channel-layer retry is nil; adapters log and let caller decide escalation.

**Silent vs. streaming**: Telegram streams; web defers; cron and swarm are silent (logs only). This governs which adapters register for each channel`s delivery mode.
