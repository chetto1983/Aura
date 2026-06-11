# Phase 20: scheduler hardening full implementation — Specification

**Created:** 2026-06-11
**Ambiguity score:** 0.12 (gate: ≤ 0.20)
**Requirements:** 7 locked

## Goal

Scheduled-task notifications (reminders, agent_job summaries, failure/risk alerts) are delivered back to the channel that scheduled them — identity-keyed to the user's 1:1 chat — instead of always routing to whatsapp/email/stdout, across both the immediate dispatch path and the quiet-hours-deferred / failed-retry sweep.

## Background

Discovered live during Phase 19 (19-11) sign-off: a reminder set via the Telegram bot fired but the notice went to whatsapp/stdout, never back to Telegram. Davide flagged it a bug and directed a generic fix — "send to the channel that scheduled it; easy to add new channels without reinventing the wheel."

Current reality (ground-truthed 2026-06-11):
- Scheduler (CAP-06, Phase 10) ships the full cron/agent_job/reminder/backup dispatch with quiet-hours deferral, a `pending_notifications` retry queue (migration 0013), and a composite `Notifier` (`internal/cron/notify.go`) whose only routes are whatsapp/email/stdout via MCP self-send.
- Telegram (UX-02, Phase 13) ships with `telegram_accounts` keyed by `identity_id uuid` (migration 0012), a live bot held under `Telegram.mu`, and a `botSender.Send(to tele.Recipient, …)` seam.
- The scaffolding for the fix already exists: `scheduler_tasks.origin_conversation_id uuid` (migration 0009) round-trips through `cron.Task` / `CreateTaskParams`; `GetTelegramAccountByIdentity` SQL + the generated sqlc wrapper both exist; `conversations.GetConversation` projects `identity_id`; the tool-call ctx carries the conversation id as `sessionID` (`llm_agent.go:470`).

Two root causes remain (both confirmed):
1. **Origin never captured** — `tools.CreateTaskInput` has no origin field; `actionSchedule` ignores the ctx sessionID; `cronTaskStore.CreateScheduledTask` never populates `origin_conversation_id` (it persists empty/NULL).
2. **Notifier ignores origin** — `dispatch.notify()` (`dispatch.go:213`) and `sweepNotifications()` (`dispatch.go:287`) call `Notifier.Notify(ctx, route, "", text)` and never read `task.OriginConversationID` / `task.IdentityID`.

The execution-ready design is `.planning/spikes/reminder-agnostic-channel.md` (identity-keyed `channels.Deliverer` seam + `Registry.DeliverToIdentity` + Telegram impl + `dispatch.deliverToOrigin` + serve wiring; Step 1 immediate path, Step 2 migration 0014 for the deferred/failed sweep).

## Requirements

1. **Origin capture**: A scheduled task persists the conversation it was created from.
   - Current: `CreateTaskInput` has no origin field; `actionSchedule` does not read the tool-call ctx; the adapter never sets `origin_conversation_id`, so it persists NULL even though the column + domain fields exist.
   - Target: `CreateTaskInput.OriginConversationID`; `actionSchedule` reads the conversation id from the tool-call ctx (the `sessionID` set at `llm_agent.go:470`); `cronTaskStore.CreateScheduledTask` threads it into `cron.CreateTaskParams.OriginConversationID`.
   - Acceptance: Scheduling a reminder during an agent turn carrying conversation id `C` produces a `scheduler_tasks` row with `origin_conversation_id = C`; scheduling with no conversation context (bare ctx) persists NULL origin (no panic, no error).

2. **Identity-keyed delivery seam**: A generic, optional channel-delivery capability the scheduler can fan out over.
   - Current: `internal/channels` has no delivery interface; the only inbound contract is `channels.Channel` (Start/Stop/IsHealthy); the scheduler can reach a user only through the 3 MCP routes.
   - Target: `channels.Deliverer` interface `Deliver(ctx, identityID, text string) (delivered bool, err error)` + `Registry.DeliverToIdentity` that fans out over **started** channels only (late-bound). Tri-state contract: `(false,nil)` = not my user → try next; `(true,nil)` = delivered → stop; `(false,err)` = owns-but-failed → stop, do not try siblings. A channel that does not implement `Deliverer` is skipped.
   - Acceptance: Unit tests prove first-delivers-wins, not-my-user fall-through, owns-but-fails stops without asking siblings, and a not-started channel is never asked.

3. **Telegram Deliverer**: Telegram delivers to the onboarded user's 1:1 chat.
   - Current: Telegram implements only the `channels.Channel` lifecycle; `Store` has `GetAccountByTelegramID` but no `GetAccountByIdentity` Go wrapper (the `GetTelegramAccountByIdentity` SQL + sqlc wrapper already exist).
   - Target: `Store.GetAccountByIdentity` wrapper + `Telegram.Deliver(ctx, identityID, text)` resolving identity → `telegram_user_id` → `bot.Send(tele.ChatID(id), text)` under `t.mu`. Returns `(false,nil)` on `pgx.ErrNoRows` (not my user), `(false,err)` on send failure, `(true,nil)` on send.
   - Acceptance: With an Offline bot + fake Store — found identity → send recorded on the `botSender` double; `ErrNoRows` → `(false,nil)`; send error → `(false,err)`.

4. **Dispatch routes to origin**: A `deliverToOrigin` precedence in the dispatcher prefers the origin channel.
   - Current: `notify()` and `sweepNotifications()` call `Notifier.Notify(ctx, route, "", text)`, ignoring origin.
   - Target: `DispatchDeps` gains `ChannelDeliverer` + `IdentityForConversation` seams (cron imports neither channels nor conversations — the composition root adapts). A `deliverToOrigin` helper: when `notify` is unset and origin resolves to an identity → try `DeliverToIdentity`; delivered → done; not-my-user → fall back to `Notifier.Notify(route)`; owns-but-failed → queue a failed `pending_notification` for same-channel retry with NO cross-channel fallback. Nil deps → today's behavior unchanged.
   - Acceptance: Unit tests — channel delivers ⇒ Notifier NOT called; no origin / no owning channel ⇒ Notifier called with route; explicit route set ⇒ Notifier called and channel skipped; owns-but-fails ⇒ failed pending row inserted + Notifier NOT called; nil deps ⇒ legacy regression guard passes.

5. **End-to-end immediate path (Step 1)**: A reminder set in Telegram fires back in that Telegram chat.
   - Current: A reminder set via the Telegram bot fires but routes to whatsapp/stdout (the live Phase-19 bug).
   - Target: Composition root captures origin (R1), wires the `Registry` as `ChannelDeliverer` + an `identityForConversation` adapter (over `conversations.GetConversation`) into `DispatchDeps`, and reorders `bootChannelsAndSetup` before `buildDispatch` so the late-bound Registry pointer is available. (Reminders bypass quiet-hours deferral — `dispatch.go:202` `task.Kind != KindReminder` — so the immediate path fully covers the headline use case.)
   - Acceptance: Live — schedule "remind me in 1 minute" in a Telegram DM → after ~70s the reminder text arrives in the SAME Telegram chat (rendered ground truth, not stdout/whatsapp); the `scheduler_tasks.origin_conversation_id` for that task is set.

6. **Deferred + failed sweep route back (Step 2)**: Quiet-hours-deferred and failed notifications also reach the origin channel.
   - Current: `pending_notifications` (migration 0013) stores only `notify_route` + body — origin is lost, so the sweep (`dispatch.go:287`) can only route to whatsapp/email/stdout.
   - Target: Migration **0014** adds `origin_conversation_id uuid REFERENCES aura.conversations(id) ON DELETE SET NULL` to `pending_notifications`; `InsertPendingNotification` + `SweepDueNotifications` carry it; the store projection adds the field; `sweepNotifications` delivers via `deliverToOrigin`.
   - Acceptance: A quiet-hours-deferred agent_job notification whose origin is a Telegram chat, swept after the window ends, is delivered back to that Telegram chat (not the default route); a transiently-failed channel delivery is retried on the same channel via the swept pending row.

7. **Route precedence + no-origin fallback (observable contract)**: Explicit routes are honored; tasks with no owning channel degrade to today's behavior.
   - Current: All scheduled-task output routes via the notify route only.
   - Target: `notify` unset → origin channel preferred; `notify=whatsapp|email|stdout` set → that route is honored and the channel is skipped; no channel owns the identity (CLI-origin, un-onboarded identity, deleted conversation → NULL/unmapped origin) → fall back to the notify route, else `AURA_SCHEDULER_NOTIFY_DEFAULT`, else stdout (purely additive — no regression).
   - Acceptance: explicit `notify=whatsapp` on a Telegram-origin task → delivered via whatsapp (not Telegram); a CLI-scheduled reminder (no origin channel) → delivered via the configured route; behavior with nil channel deps is byte-identical to today.

## Boundaries

**In scope:**
- `channels.Deliverer` interface + `Registry.DeliverToIdentity` fan-out (generic, identity-keyed, late-bound over started channels).
- Telegram `Deliver` implementation + `Store.GetAccountByIdentity` wrapper (reusing the existing SQL/sqlc query).
- Origin capture in the `task` tool (`CreateTaskInput.OriginConversationID` + ctx sessionID read) + adapter pass-through to `cron.Store`.
- `dispatch.deliverToOrigin` + `DispatchDeps.{ChannelDeliverer, IdentityForConversation}` seams + serve wiring (boot reorder, late-bound Registry, `identityForConversation` adapter).
- Migration **0014** adding `origin_conversation_id` to `pending_notifications` + Insert/Sweep/projection threading + `sweepNotifications` route-back.
- Unit tests (registry fan-out, Telegram deliver, task-tool origin capture, dispatch precedence) + the live verification recipe.

**Out of scope:**
- **Group-chat / exact-origin-chat delivery** — a group-set reminder delivers to the user's DM (identity-keyed), not the group. True group-origin delivery needs a conversation→channel-address binding (`convID` is a one-way UUIDv5, non-invertible) and breaks the identity-keyed model — deferred until a real requirement.
- **Channels other than Telegram** — whatsapp/email remain MCP self-send routes (the no-origin fallback), not `Deliverer` channels; no second `Channel` is built. The seam is generic so a future channel plugs in with zero scheduler changes.
- **Broader scheduler hardening** — orphan-run reclaim, quiet-hours window redesign, MCP reconnect-on-use, heartbeat changes — separate concerns, not this phase.
- **Telegram onboarding / account-linking changes** — assumed shipped (Phase 13); this phase consumes the existing `telegram_accounts ↔ identity` link.
- **CLI-origin channel delivery** — CLI tasks are intentionally route-delivered (no push channel owns the CLI identity).

## Constraints

- **Identity-keyed**: delivery uses `IdentityID` as the channel-independent address key. The scheduler never imports `internal/channels` or `internal/conversations` — the composition root (`cmd/aura`) adapts both via consumer-declared interfaces (the existing `cron.Notifier` / `cron.Handler` idiom).
- **Owned-but-failed does NOT fall back to another channel** — avoids double-delivery when the same-channel retry later succeeds.
- **Migrations**: Step 1 requires NO migration (reuses `scheduler_tasks.origin_conversation_id`, migration 0009). Step 2 is migration **0014** (next free slot; 0013 created `pending_notifications`). The existing `aura_app` DML grant covers the new column.
- **Reuse**, do not duplicate: R3 uses the existing `GetTelegramAccountByIdentity` SQL + sqlc wrapper — no new query.
- **Quality gates (CLAUDE.md)**: owned-surface coverage ≥85% (hard floor); `go vet` + `go build` + `go test` + `-race` green; `golangci-lint` 0; every touched file ≤600 LOC; no-skip-as-green in CI; deferred-tool pattern unaffected (`task` stays non-deferred).
- **Boot order**: `bootChannelsAndSetup` must run before `buildDispatch` (the one place to review — confirmed it only needs `chat` + `override`, both available earlier; the Deliverer is late-bound so only the Registry pointer must exist at build time).

## Acceptance Criteria

- [ ] Scheduling a reminder from a conversation persists `scheduler_tasks.origin_conversation_id` equal to that conversation id; bare-ctx scheduling persists NULL (no panic).
- [ ] `channels.Deliverer` + `Registry.DeliverToIdentity` exist; unit tests cover first-delivers-wins, not-my-user fall-through, owns-but-fails (no sibling attempt), not-started-never-asked.
- [ ] `Telegram.Deliver` sends to the resolved DM via `bot.Send`; `ErrNoRows` → `(false,nil)`, send error → `(false,err)`, found → `(true,nil)` (Offline-bot unit test).
- [ ] `dispatch.deliverToOrigin`: channel delivers ⇒ Notifier not called; explicit route ⇒ channel skipped + Notifier called; no owning channel ⇒ Notifier called with route; owns-but-fails ⇒ failed pending row + Notifier not called; nil deps ⇒ legacy behavior preserved.
- [ ] LIVE: a "remind me in 1 minute" set in a Telegram DM arrives in the SAME Telegram chat after ~70s (not stdout/whatsapp).
- [ ] Migration 0014 applies (up) and reverts (down) cleanly; `pending_notifications` carries `origin_conversation_id`; a quiet-hours-deferred agent_job notification swept after the window routes back to its origin Telegram chat.
- [ ] An explicit `notify=whatsapp` on a Telegram-origin task is delivered via whatsapp (route honored, channel skipped).
- [ ] A CLI-scheduled reminder (no origin channel) is delivered via the configured notify route — no regression vs current behavior.
- [ ] Owned-surface coverage ≥85%; `go vet`/`build`/`test`/`-race` green; `golangci-lint` 0; all touched files ≤600 LOC.

## Ambiguity Report

| Dimension          | Score | Min  | Status | Notes                                                        |
|--------------------|-------|------|--------|--------------------------------------------------------------|
| Goal Clarity       | 0.92  | 0.75 | ✓      | Identity-keyed origin delivery, both immediate + deferred    |
| Boundary Clarity   | 0.90  | 0.70 | ✓      | Groups OUT, Telegram-only, broader hardening OUT             |
| Constraint Clarity | 0.80  | 0.65 | ✓      | Step-1 no-migration; Step-2 = migration 0014; identity-keyed |
| Acceptance Criteria| 0.85  | 0.70 | ✓      | Live recipe + route/retry/fallback unit cases               |
| **Ambiguity**      | 0.12  | ≤0.20| ✓      |                                                              |

Status: ✓ = met minimum, ⚠ = below minimum (planner treats as assumption)

## Interview Log

| Round | Perspective     | Question summary                                  | Decision locked                                                                 |
|-------|-----------------|---------------------------------------------------|---------------------------------------------------------------------------------|
| 1     | Researcher      | Scope of "full implementation"?                   | Full agnostic delivery — Step 1 (immediate) + Step 2 (deferred/failed sweep)     |
| 1     | Researcher      | Route precedence — explicit notify vs origin?     | Origin is the default; an explicit `notify` route overrides it                   |
| 1     | Failure Analyst | Transient channel-send failure behavior?          | Retry on the same channel (pending queue); no cross-channel fallback             |
| 2     | Boundary Keeper | Group-chat origin in or out?                      | (Initially "exact chat" — reversed in round 3 after the data-model cost surfaced)|
| 2     | Boundary Keeper | No resolvable origin channel — what happens?      | Fall back to the configured notify route (today's behavior); CLI is route-delivered |
| 3     | Seed Closer     | Accept binding-table schema for exact group chat? | NO — scope groups OUT; DM-only identity-keyed (group reminder → user's DM)        |
| 3     | Boundary Keeper | Which channels deliver this phase?                | Telegram only; the Deliverer seam stays generic for future channels             |

---

*Phase: 20-scheduler-hardening-full-implementation*
*Spec created: 2026-06-11*
*Next step: /gsd-discuss-phase 20 — implementation decisions (identity-resolution seam R1-vs-R2, deliverToOrigin placement, test seams)*
