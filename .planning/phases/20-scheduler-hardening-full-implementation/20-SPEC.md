# Phase 20: scheduler hardening full implementation — Specification

**Created:** 2026-06-11
**Ambiguity score:** 0.12 (gate: ≤ 0.20)
**Requirements:** 7 locked

> **Discuss-phase refinements (2026-06-11 — see `20-CONTEXT.md`):** four implementation forks were locked after industrial-pattern research. (1) **Origin key** — R1 snapshots the stable `identity_id` at schedule time (transactional-outbox / Klaviyo pattern), so a deleted origin conversation still resolves the owning channel; this **simplifies R4**, which now reads `task.IdentityID` directly and drops the dispatch-time `IdentityForConversation` seam. (2) **Kill-switch** — a default-on `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` env gates the origin preference (Fowler ops/kill-switch; a misroute fails silently and user-visibly). (3) **Live verify** — both Step 1 and Step 2 are live-verified (Step 2 via a forced quiet-hours window). (4) **Channel order** — `Registry.DeliverToIdentity` fans out in a deterministic order, not map-iteration order. The requirement bodies below are updated to match.

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

1. **Origin capture (conversation + snapshot identity)**: A scheduled task persists both the conversation it was created from AND the stable owning `identity_id` resolved at schedule time.
   - Current: `CreateTaskInput` has no origin field; `actionSchedule` does not read the tool-call ctx; the adapter never sets `origin_conversation_id`, so it persists NULL even though the column + domain fields exist. `scheduler_tasks.identity_id` exists but always defaults `'local'`.
   - Target: `CreateTaskInput.OriginConversationID`; `actionSchedule` reads the conversation id from the tool-call ctx (the `sessionID` set at `llm_agent.go:470`). The composition-root adapter `cronTaskStore.CreateScheduledTask` resolves that conversation id → `identity_id` **at schedule time** via `conversations.GetConversation` (which already projects `identity_id`) and threads BOTH `origin_conversation_id` and the resolved `identity_id` into `cron.CreateTaskParams` (both columns already exist). The `task` tool stays free of any `conversations` import — the adapter owns the resolution (consumer-declared-seam idiom). **Rationale (discuss-phase Fork 1):** `identity_id` is the stable, channel-independent delivery key (outbox snapshot pattern); `origin_conversation_id` is deletable (`ON DELETE SET NULL`, R6) and is kept only for context / future group support. Snapshotting identity at enqueue means a deleted origin conversation still resolves the owning channel — and it lets R4 read identity directly at dispatch with no conversation lookup.
   - Acceptance: Scheduling a reminder during an agent turn carrying conversation id `C` (owned by identity `I`) produces a `scheduler_tasks` row with `origin_conversation_id = C` AND `identity_id = I`; scheduling with no conversation context (bare ctx) persists NULL origin + `identity_id = 'local'` (no panic, no error); deleting conversation `C` afterward (origin → NULL) leaves `identity_id = I` intact so delivery still resolves the channel.

2. **Identity-keyed delivery seam**: A generic, optional channel-delivery capability the scheduler can fan out over.
   - Current: `internal/channels` has no delivery interface; the only inbound contract is `channels.Channel` (Start/Stop/IsHealthy); the scheduler can reach a user only through the 3 MCP routes.
   - Target: `channels.Deliverer` interface `Deliver(ctx, identityID, text string) (delivered bool, err error)` + `Registry.DeliverToIdentity` that fans out over **started** channels only (late-bound) **in a deterministic order** (stable sort by channel name, or an explicit `Priority`), NOT Go map-iteration order. Tri-state contract: `(false,nil)` = not my user → try next; `(true,nil)` = delivered → stop; `(false,err)` = owns-but-failed → stop, do not try siblings. A channel that does not implement `Deliverer` is skipped. **Rationale (discuss-phase Fork 4):** every mature multi-channel router (Courier "Best Of", Novu, AWS Pinpoint) tries channels in a *declared* order until one succeeds; map iteration is nondeterministic and untestable the moment a 2nd `Deliverer` lands. The per-identity preference engine is deferred (YAGNI — Telegram is the only `Deliverer` today).
   - Acceptance: Unit tests prove first-delivers-wins **in the deterministic order**, not-my-user fall-through, owns-but-fails stops without asking siblings, and a not-started channel is never asked.

3. **Telegram Deliverer**: Telegram delivers to the onboarded user's 1:1 chat.
   - Current: Telegram implements only the `channels.Channel` lifecycle; `Store` has `GetAccountByTelegramID` but no `GetAccountByIdentity` Go wrapper (the `GetTelegramAccountByIdentity` SQL + sqlc wrapper already exist).
   - Target: `Store.GetAccountByIdentity` wrapper + `Telegram.Deliver(ctx, identityID, text)` resolving identity → `telegram_user_id` → `bot.Send(tele.ChatID(id), text)` under `t.mu`. Returns `(false,nil)` on `pgx.ErrNoRows` (not my user), `(false,err)` on send failure, `(true,nil)` on send.
   - Acceptance: With an Offline bot + fake Store — found identity → send recorded on the `botSender` double; `ErrNoRows` → `(false,nil)`; send error → `(false,err)`.

4. **Dispatch routes to origin**: A `deliverToOrigin` precedence in the dispatcher prefers the origin channel, gated by a default-on kill-switch.
   - Current: `notify()` and `sweepNotifications()` call `Notifier.Notify(ctx, route, "", text)`, ignoring origin.
   - Target: `DispatchDeps` gains a single `ChannelDeliverer` seam (cron does not import channels — the composition root adapts the `Registry`). **The identity is read directly from `task.IdentityID`** (the R1 schedule-time snapshot) — there is NO `IdentityForConversation` lookup at dispatch (discuss-phase Fork 1 simplification). A `deliverToOrigin` helper: when the `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` gate is on (default), `notify` is unset, and `task.IdentityID` is non-empty → try `DeliverToIdentity(task.IdentityID, text)`; delivered → done; not-my-user (e.g. `'local'` / un-onboarded → no channel owns it) → fall back to `Notifier.Notify(route)`; owns-but-failed → queue a failed `pending_notification` for same-channel retry with NO cross-channel fallback. Nil deps OR kill-switch off → today's behavior unchanged.
   - Acceptance: Unit tests — channel delivers ⇒ Notifier NOT called; no owning channel (`'local'`/un-onboarded identity) ⇒ Notifier called with route; explicit route set ⇒ Notifier called and channel skipped; owns-but-fails ⇒ failed pending row inserted + Notifier NOT called; kill-switch off ⇒ channel skipped + Notifier called (regression to today); nil deps ⇒ legacy regression guard passes.

5. **End-to-end immediate path (Step 1)**: A reminder set in Telegram fires back in that Telegram chat.
   - Current: A reminder set via the Telegram bot fires but routes to whatsapp/stdout (the live Phase-19 bug).
   - Target: Composition root captures origin + snapshot identity (R1 — the `cronTaskStore.CreateScheduledTask` adapter resolves conversation → `identity_id` via `conversations.GetConversation` **at schedule time**), wires the `Registry` as `ChannelDeliverer` into `DispatchDeps`, and reorders `bootChannelsAndSetup` before `buildDispatch` so the late-bound Registry pointer is available. (Reminders bypass quiet-hours deferral — `dispatch.go:202` `task.Kind != KindReminder` — so the immediate path fully covers the headline use case.)
   - Acceptance: Live — schedule "remind me in 1 minute" in a Telegram DM → after ~70s the reminder text arrives in the SAME Telegram chat (rendered ground truth, not stdout/whatsapp); the `scheduler_tasks` row for that task has `origin_conversation_id` AND `identity_id` set to the chat's identity.

6. **Deferred + failed sweep route back (Step 2)**: Quiet-hours-deferred and failed notifications also reach the origin channel.
   - Current: `pending_notifications` (migration 0013) stores only `notify_route` + body — origin is lost, so the sweep (`dispatch.go:287`) can only route to whatsapp/email/stdout.
   - Target: Migration **0014** adds `identity_id text` (the stable snapshot delivery key, mirroring `scheduler_tasks.identity_id` — plain text, NO FK, so it survives a deleted origin conversation; consistent with Fork 1) to `pending_notifications`; `InsertPendingNotification` callers thread `task.IdentityID`; `SweepDueNotifications` + the store projection carry it; `sweepNotifications` delivers via `deliverToOrigin` keyed on the row's `identity_id` (subject to the same `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` gate).
   - Acceptance: A quiet-hours-deferred agent_job notification whose origin identity owns a Telegram chat, swept after the window ends, is delivered back to that Telegram chat (not the default route); a transiently-failed channel delivery is retried on the same channel via the swept pending row.

7. **Route precedence + no-origin fallback (observable contract)**: Explicit routes are honored; tasks with no owning channel degrade to today's behavior.
   - Current: All scheduled-task output routes via the notify route only.
   - Target: `notify` unset → origin channel preferred; `notify=whatsapp|email|stdout` set → that route is honored and the channel is skipped; no channel owns the identity (CLI-origin, un-onboarded identity, deleted conversation → NULL/unmapped origin) → fall back to the notify route, else `AURA_SCHEDULER_NOTIFY_DEFAULT`, else stdout (purely additive — no regression).
   - Acceptance: explicit `notify=whatsapp` on a Telegram-origin task → delivered via whatsapp (not Telegram); a CLI-scheduled reminder (no origin channel) → delivered via the configured route; behavior with nil channel deps is byte-identical to today.

## Boundaries

**In scope:**
- `channels.Deliverer` interface + `Registry.DeliverToIdentity` fan-out (generic, identity-keyed, late-bound over started channels, **deterministic order**).
- Telegram `Deliver` implementation + `Store.GetAccountByIdentity` wrapper (reusing the existing SQL/sqlc query).
- Origin + snapshot-identity capture: `CreateTaskInput.OriginConversationID` + ctx sessionID read in the `task` tool; the `cronTaskStore.CreateScheduledTask` adapter resolves conversation → `identity_id` (via `conversations.GetConversation`) at schedule time and threads BOTH into `cron.Store`.
- `dispatch.deliverToOrigin` + `DispatchDeps.ChannelDeliverer` seam (identity read from `task.IdentityID`; no dispatch-time conversation lookup) + the `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` gate + serve wiring (boot reorder, late-bound Registry).
- Migration **0014** adding `identity_id text` to `pending_notifications` + Insert/Sweep/projection threading + `sweepNotifications` route-back.
- Unit tests (registry deterministic fan-out, Telegram deliver, task-tool origin+identity capture, dispatch precedence + kill-switch) + the live verification recipe (Step 1 + Step 2 via forced quiet-hours window).

**Out of scope:**
- **Group-chat / exact-origin-chat delivery** — a group-set reminder delivers to the user's DM (identity-keyed), not the group. True group-origin delivery needs a conversation→channel-address binding (`convID` is a one-way UUIDv5, non-invertible) and breaks the identity-keyed model — deferred until a real requirement.
- **Channels other than Telegram** — whatsapp/email remain MCP self-send routes (the no-origin fallback), not `Deliverer` channels; no second `Channel` is built. The seam is generic so a future channel plugs in with zero scheduler changes.
- **Broader scheduler hardening** — orphan-run reclaim, quiet-hours window redesign, MCP reconnect-on-use, heartbeat changes — separate concerns, not this phase.
- **Telegram onboarding / account-linking changes** — assumed shipped (Phase 13); this phase consumes the existing `telegram_accounts ↔ identity` link.
- **CLI-origin channel delivery** — CLI tasks are intentionally route-delivered (no push channel owns the CLI identity).

## Constraints

- **Identity-keyed, snapshot-at-schedule**: delivery uses `IdentityID` as the channel-independent address key, **captured at schedule time** (Fork 1). The scheduler never imports `internal/channels` or `internal/conversations` — the composition root (`cmd/aura`) adapts both via consumer-declared interfaces (the existing `cron.Notifier` / `cron.Handler` idiom). The conversation → identity resolution lives in the schedule-time `cronTaskStore.CreateScheduledTask` adapter (composition root), NOT in a dispatch-time seam.
- **Kill-switch**: `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` (bool, **default true** via `config.envBoolDefault` — unset/malformed → on). Off ⇒ scheduler delivery is byte-identical to today's route-only behavior (Fork 2; Fowler ops/kill-switch for a silent-misroute blast radius). Add it to the PRD env catalog (`AURA_<DOMAIN>_<UNIT>` convention).
- **Owned-but-failed does NOT fall back to another channel** — avoids double-delivery when the same-channel retry later succeeds.
- **Deterministic fan-out**: `Registry.DeliverToIdentity` iterates started channels in a stable order (sort by name or explicit `Priority`), never Go map order (Fork 4).
- **Migrations**: Step 1 requires NO migration (reuses `scheduler_tasks.origin_conversation_id` AND `scheduler_tasks.identity_id`, both migration 0009). Step 2 is migration **0014** (next free slot; 0013 created `pending_notifications`) adding `identity_id text` (no FK). The existing `aura_app` DML grant covers the new column.
- **Reuse**, do not duplicate: R3 uses the existing `GetTelegramAccountByIdentity` SQL + sqlc wrapper — no new query.
- **Quality gates (CLAUDE.md)**: owned-surface coverage ≥85% (hard floor); `go vet` + `go build` + `go test` + `-race` green; `golangci-lint` 0; every touched file ≤600 LOC; no-skip-as-green in CI; deferred-tool pattern unaffected (`task` stays non-deferred).
- **Boot order**: `bootChannelsAndSetup` must run before `buildDispatch` (the one place to review — confirmed it only needs `chat` + `override`, both available earlier; the Deliverer is late-bound so only the Registry pointer must exist at build time).

## Acceptance Criteria

- [ ] Scheduling a reminder from a conversation `C` (identity `I`) persists `scheduler_tasks.origin_conversation_id = C` AND `identity_id = I`; bare-ctx scheduling persists NULL origin + `identity_id='local'` (no panic); deleting `C` afterward leaves `identity_id` intact.
- [ ] `channels.Deliverer` + `Registry.DeliverToIdentity` exist; unit tests cover first-delivers-wins **in deterministic order**, not-my-user fall-through, owns-but-fails (no sibling attempt), not-started-never-asked.
- [ ] `Telegram.Deliver` sends to the resolved DM via `bot.Send`; `ErrNoRows` → `(false,nil)`, send error → `(false,err)`, found → `(true,nil)` (Offline-bot unit test).
- [ ] `dispatch.deliverToOrigin` (identity read from `task.IdentityID`): channel delivers ⇒ Notifier not called; explicit route ⇒ channel skipped + Notifier called; no owning channel (`'local'`/un-onboarded) ⇒ Notifier called with route; owns-but-fails ⇒ failed pending row + Notifier not called; `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL=false` ⇒ channel skipped + Notifier called; nil deps ⇒ legacy behavior preserved.
- [ ] LIVE Step 1: a "remind me in 1 minute" set in a Telegram DM arrives in the SAME Telegram chat after ~70s (not stdout/whatsapp).
- [ ] LIVE Step 2: with the quiet-hours window forced to cover "now", a deferred agent_job notification is swept after the window and arrives back in the origin Telegram chat (not the default route).
- [ ] Migration 0014 applies (up) and reverts (down) cleanly; `pending_notifications` carries `identity_id`; a quiet-hours-deferred agent_job notification swept after the window routes back to its origin Telegram chat.
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
