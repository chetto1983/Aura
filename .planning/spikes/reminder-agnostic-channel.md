# Spike / Design — Reminder agnostic-channel delivery

> Status: **DESIGNED, not implemented** (deferred 2026-06-10 to finish Phase 19 live sign-off first).
> Origin: discovered live during Phase 19 19-11 sign-off — a reminder set via the Telegram bot
> fired but the notification never reached Telegram (routed to whatsapp/stdout). User (Davide)
> flagged it as a bug and directed a generic fix: "the reminder must send to the channel that
> scheduled it — so adding new channels is easy without reinventing the wheel."
> NOT one of the 26 Phase-19 audit findings — a new finding.

## Problem

Scheduled-task / reminder notifications route only to **whatsapp / email / stdout** (MCP self-send,
`internal/cron/notify.go`). A reminder created in a Telegram chat never reaches the user there. Two
root causes:

1. **Origin never captured.** `tools.CreateTaskInput` (internal/agent/tools/task.go) has no
   identity/conversation fields; `cronTaskStore.CreateScheduledTask` (cmd/aura/serve_adapters.go)
   never populates them → tasks persist with `origin_conversation_id=""`, `identity_id="local"`.
   *Good news:* the tool-call ctx already carries the conversation id as `sessionID`
   (llm_agent.go:449; `sessionID = Event.ThreadID = conversation id`). So `actionSchedule` just reads it.
2. **Notifier ignores origin.** `cron.Notifier.Notify(ctx, route, recipient, text)` only knows the
   3 routes; the dispatched `Task` carries `OriginConversationID`/`IdentityID` but they are never used.

## Design — agnostic seam (identity-keyed)

Deliver by **IdentityID** (the universal, channel-independent address key — every channel keys its
account table by identity). The scheduler resolves `OriginConversationID → IdentityID` once
(`conversations.identity_id`); channels stay free of conversation semantics.

- **New optional capability** `internal/channels/deliver.go`:
  `type Deliverer interface { Deliver(ctx, identityID, text string) (delivered bool, err error) }`
  — `(false,nil)` = not my user (try next); `(false,err)` = my user but send failed; `(true,nil)` = sent.
  A channel that can't push simply does not implement it → registry skips it. **New channels plug in
  with zero scheduler/registry changes.**
- **`Registry.DeliverToIdentity`** (registry.go) fans out across **started** channels (late-bound — a
  not-started channel with a nil transport is never asked; resolves the bot-not-live-at-build-time order).
- **Telegram** `internal/channels/telegram/deliver.go`: `identity → GetAccountByIdentity →
  telegram_user_id → bot.Send(tele.ChatID(id), text)`. Needs Store wrapper `GetAccountByIdentity`
  (SQL `GetTelegramAccountByIdentity` already exists; only the Go wrapper is missing).
- **Scheduler** (`internal/cron/dispatch.go`): `DispatchDeps` gains `ChannelDeliverer` +
  `IdentityForConversation` seams (cron never imports channels/conversations — composition root adapts).
  One `deliverToOrigin(ctx, originConvID, route, text)` helper: prefer channel, fall back to route.
  Used by both `notify()` (line 213) and (Step 2) `sweepNotifications()` (line 287). Quiet-hours
  deferral is unchanged — channel-preferred delivery still defers, then the sweep delivers to channel.
- **Wiring** (`cmd/aura/serve.go`): reorder `bootChannelsAndSetup` before `buildDispatch`; pass the
  Registry as `ChannelDeliverer` (late-bound) + `identityForConversation(chat.conv)` adapter.

## Minimal-first plan

**Step 1 — smallest end-to-end (NO migration): "reminder set in Telegram fires back in Telegram"**
1. `internal/channels/deliver.go` — `Deliverer` interface.
2. `internal/channels/registry.go` — `DeliverToIdentity` fan-out.
3. `internal/channels/telegram/store.go` — `GetAccountByIdentity` wrapper.
4. `internal/channels/telegram/deliver.go` — `Telegram.Deliver` (reads live bot under mu).
5. `internal/agent/tools/task.go` — `CreateTaskInput.OriginConversationID` + read `toolCallCtx(ctx).sessionID` in `actionSchedule`.
6. `cmd/aura/serve_adapters.go` — pass `OriginConversationID` through; add `identityForConversation` adapter.
7. `internal/cron/dispatch.go` — `ChannelDeliverer`+`IdentityForConversation` deps, `deliverToOrigin` helper, swap `notify()` line 213.
8. `cmd/aura/serve.go` — reorder bootChannels before buildDispatch; thread `reg` + adapter into `DispatchDeps`; add `var _ cron.ChannelDeliverer = (*channels.Registry)(nil)`.

**Step 2 — quiet-hours / failed sweep (migration 0014)**
- `0014_pending_notifications_origin.up/.down.sql`: add `origin_conversation_id uuid REFERENCES aura.conversations(id) ON DELETE SET NULL` (existing aura_app DML grant covers it; COMMENT; no new index).
- `pending_notifications.sql` Insert+Sweep add the column; `sqlc generate`.
- `internal/cron/store_runs.go` PendingNotification/InsertParams/projection add `OriginConversationID`.
- `dispatch.go` `insertPendingNotification` callers thread `task.OriginConversationID`; sweep line 287 → `deliverToOrigin`.

**Step 3 — tests**
- registry: first-delivers-wins / not-my-user fall-through / owns-but-fails aggregation / not-started never asked.
- telegram deliver: Offline bot + fake Store — found→send via recording botSender; ErrNoRows→(false,nil); send err→(false,err).
- task tool: `actionSchedule` with `WithToolCallContext` captures sessionID; no ctx → empty.
- dispatch: `deliverToOrigin` precedence (channel delivers → Notifier NOT called; no-route → Notifier; channel error → failed row, Notifier NOT called; nil deps → legacy regression guard).

## Live verification recipe

1. `aura serve` (telegram enabled, account onboarded linking telegram_user_id → identity).
2. In the bot chat: "remind me in 1 minute to drink water" → agent schedules `kind=reminder at=now+60s payload={"text":"drink water"}`.
3. DB: `SELECT id,kind,origin_conversation_id,next_run_at FROM aura.scheduler_tasks WHERE kind='reminder' ORDER BY created_at DESC LIMIT 1;` → assert `origin_conversation_id` set (= UUIDv5 of the chat id).
4. Wait ~70s → "drink water" arrives in the SAME Telegram chat (rendered ground truth, not stdout/whatsapp).
5. Fallback: delete the telegram_accounts row, schedule again → falls back to `AURA_SCHEDULER_NOTIFY_DEFAULT`.

## Risks

1. **CLI-scheduled tasks have no origin** → NULL origin → falls back to route. Intended; document it.
2. **Group chats:** `telegram_user_id` is the 1:1 chat id → a group-set reminder delivers to the user's DM, not the group. Correct for 1:1 onboarding; if group origin is needed later, carry the origin chat id (breaks the identity-keyed model — defer until a real requirement).
3. **Multiple channels per identity:** `DeliverToIdentity` stops at first delivered; map iteration is nondeterministic. Add a deterministic preference order when a 2nd push channel lands.
4. **`bootServe` reorder** is the one place to review carefully (confirmed `bootChannelsAndSetup` only needs `chat`+`override`, available before `buildDispatch`).
5. **Channel-owns-but-fails does NOT fall back to route** (avoids double-delivery on retry) → transient Telegram outage defers to the failed-row retry (Step 2). Until Step 2, a hard failure on a quiet-hours/failed row falls back to route.
