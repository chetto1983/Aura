# Phase 20: scheduler-hardening-full-implementation - Context

**Gathered:** 2026-06-11
**Status:** Ready for planning

<domain>
## Phase Boundary

Scheduled-task notifications (reminders, agent_job summaries, failure/risk alerts) are delivered back to the channel that scheduled them — identity-keyed to the user's 1:1 chat (Telegram) — instead of always routing to whatsapp/email/stdout, across both the immediate dispatch path (Step 1) and the quiet-hours-deferred / failed-retry sweep (Step 2).

This is a HOW-only phase: requirements are locked by `20-SPEC.md`; the execution-ready design is `.planning/spikes/reminder-agnostic-channel.md`. Discussion locked four implementation forks and reconciled the SPEC to match.

</domain>

<spec_lock>
## Requirements (locked via SPEC.md)

**7 requirements are locked.** See `20-SPEC.md` for full requirements, boundaries, and acceptance criteria — it was AMENDED during this discussion (2026-06-11) to absorb Forks 1, 2, 4 (see Implementation Decisions below). Downstream agents MUST read the amended `20-SPEC.md` before planning or implementing. Requirements are not duplicated here.

**In scope (from SPEC.md):**
- `channels.Deliverer` interface + `Registry.DeliverToIdentity` fan-out (generic, identity-keyed, late-bound over started channels, deterministic order).
- Telegram `Deliver` implementation + `Store.GetAccountByIdentity` wrapper (reusing the existing SQL/sqlc query).
- Origin + snapshot-identity capture: `CreateTaskInput.OriginConversationID` + ctx sessionID read in the `task` tool; the `cronTaskStore.CreateScheduledTask` adapter resolves conversation → `identity_id` (via `conversations.GetConversation`) at schedule time and threads BOTH into `cron.Store`.
- `dispatch.deliverToOrigin` + `DispatchDeps.ChannelDeliverer` seam (identity read from `task.IdentityID`; no dispatch-time conversation lookup) + the `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` gate + serve wiring (boot reorder, late-bound Registry).
- Migration **0014** adding `identity_id text` to `pending_notifications` + Insert/Sweep/projection threading + `sweepNotifications` route-back.
- Unit tests (registry deterministic fan-out, Telegram deliver, task-tool origin+identity capture, dispatch precedence + kill-switch) + the live verification recipe (Step 1 + Step 2 via forced quiet-hours window).

**Out of scope (from SPEC.md):**
- Group-chat / exact-origin-chat delivery (a group-set reminder delivers to the user's DM, not the group).
- Channels other than Telegram (whatsapp/email stay MCP self-send routes; the seam is generic so a future channel plugs in with zero scheduler changes).
- Broader scheduler hardening (orphan-run reclaim, quiet-hours redesign, MCP reconnect, heartbeat changes).
- Telegram onboarding / account-linking changes (assumed shipped, Phase 13).
- CLI-origin channel delivery (CLI tasks are intentionally route-delivered).

</spec_lock>

<decisions>
## Implementation Decisions

These four forks were the only genuine ambiguities left by the SPEC (ambiguity 0.12). All four were locked to research-backed options after a D:/tmp + online industrial-pattern sweep. **Forks 1, 2, 4 refined the SPEC; `20-SPEC.md` was amended in the same discuss-phase commit, so SPEC and CONTEXT do not contradict.**

### Fork 1 — Origin routing key: snapshot stable identity at schedule time
- **D-01:** Capture the stable `identity_id` at SCHEDULE time (not just `origin_conversation_id`) and deliver by it. The `cronTaskStore.CreateScheduledTask` adapter (composition root) resolves `origin_conversation_id → identity_id` once via `conversations.GetConversation` and persists BOTH onto `scheduler_tasks` (both columns already exist, migration 0009). Rationale: transactional-outbox / Klaviyo pattern — the *recipient key* must be the stable identifier (survives a deleted origin conversation, which `ON DELETE SET NULL` would otherwise turn into a silent route-fallback); the *channel* is still resolved live at delivery.
- **D-02:** This SIMPLIFIES dispatch (SPEC R4): the dispatcher reads `task.IdentityID` directly. The dispatch-time `IdentityForConversation` seam from the original SPEC/spike is REMOVED — `cron` no longer needs a conversation-lookup adapter at dispatch. One snapshot, more robust, fewer moving parts.

### Fork 2 — Origin-channel preference: default-on env kill-switch
- **D-03:** Gate the origin-channel-preference behavior behind `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` (bool, **default true** via `config.envBoolDefault`). Off ⇒ scheduler delivery is byte-identical to today's route-only behavior. Rationale: Fowler ops/kill-switch — a delivery/routing regression fails *silently and user-visibly* (reminders land where the user isn't looking, no error surfaced); the operator must be able to revert to the known-good route in seconds without a recompile/unwire. Cost is near-zero (the `AURA_<DOMAIN>_<UNIT>` convention + `config.envBoolDefault` helper already exist). **Add the var to the PRD env catalog.**

### Fork 3 — Live verification depth: live-verify BOTH steps
- **D-04:** Live-verify Step 1 (reminder → same Telegram chat) via the existing CDP harness, AND Step 2 (deferred/failed sweep route-back) by forcing the quiet-hours window to cover "now" so the deferred sweep is observable cheaply. Integration tests (DB round-trip + fake channel) remain the regression guard for both. No skip-as-green (CLAUDE.md). Step 2 is a hard gate for phase close, not advisory.

### Fork 4 — Channel fan-out ordering: deterministic now
- **D-05:** `Registry.DeliverToIdentity` fans out over started channels in a DETERMINISTIC order (stable sort by channel name, or an explicit `Priority` field), never Go map-iteration order. Rationale: Courier ("Best Of"), Novu, AWS Pinpoint all try channels in a declared order until one succeeds — map iteration is nondeterministic and untestable the moment a 2nd `Deliverer` lands. The per-identity preference engine is DEFERRED (see Deferred Ideas) — YAGNI with one `Deliverer` today.

### Process decision — SPEC reconciliation
- **D-06 [informational]:** Forks 1, 2, 4 refine locked SPEC requirements (R1/R2/R4 + Constraints + Acceptance + new env). Chose to AMEND `20-SPEC.md` in the same commit as this CONTEXT so the planner sees no contradiction (rather than leaving the planner to reconcile a stale R4 seam). *Process/meta-decision (a discuss-phase bookkeeping choice already executed in commit `2f3f25a3`) — not an implementable plan requirement, so it is not tracked against a plan.*

### Claude's Discretion
- Exact env var name (`AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` proposed; planner/executor may align to catalog conventions).
- Whether deterministic order is "sort by channel name" vs an explicit `Priority()` method on `Deliverer` (either satisfies D-05 — pick the lower-LOC one; with one channel it's unobservable, so the cheapest is fine as long as it is not map iteration).
- `deliverToOrigin` file placement (dispatch.go vs a new `internal/cron/deliver.go`) — executor's call under the ≤600-LOC rule.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase truth-sources (read first)
- `.planning/phases/20-scheduler-hardening-full-implementation/20-SPEC.md` — the 7 locked requirements + amended R1/R2/R4 + Constraints + Acceptance. AMENDED 2026-06-11 to match the four forks below.
- `.planning/spikes/reminder-agnostic-channel.md` — the execution-ready 8-step design (file targets, Step 1/Step 2 split, live recipe, risks). NOTE: its R4 still describes the original dispatch-time `IdentityForConversation` seam — superseded by D-01/D-02 (identity snapshotted at schedule time, dispatch reads `task.IdentityID`).

### Existing code the new work must mirror / extend (ground-truthed 2026-06-11)
- `internal/cron/dispatch.go` §`notify` (≈191-219) + §`sweepNotifications` (≈278-298) — the two delivery sites to route through `deliverToOrigin`; `DispatchDeps` (≈75-87) gains `ChannelDeliverer`.
- `internal/cron/notify.go` — the existing `compositeNotifier` route precedence (per-task route → `AURA_SCHEDULER_NOTIFY_DEFAULT` → stdout) the origin preference sits IN FRONT of; the consumer-declared `Notifier` / `SelfSendResolver` idiom to copy for the channel seam.
- `internal/cron/store.go` (≈60-143) — `Task` + `CreateTaskParams` already carry `IdentityID` + `OriginConversationID`; `CreateTask` defaults `IdentityID='local'`.
- `internal/channels/registry.go` — `Registry` with `started` map under `mu` (the late-bound fan-out target); `enabled`/`envChannelEnabled` is the env-gate pattern to mirror for the kill-switch.
- `internal/channels/telegram/store.go` — `GetAccountByTelegramID` (the sibling to add `GetAccountByIdentity` next to); `internal/db/sqlc/telegram_accounts.sql.go` + `internal/db/queries/telegram_accounts.sql` already define `GetTelegramAccountByIdentity` (reuse, no new query).
- `internal/agent/tools/task.go` §`actionSchedule` (≈180-218) + `CreateTaskInput` (≈54-66) — origin capture site; `shell_exec.go` §`shellSessionKey` (≈327-330) is the precedent for reading `sessionID` from `WithToolCallContext` (returns "" for bare ctx).
- `internal/agent/llm_agent.go:470` — `WithToolCallContext(ctx, a.sessionID, …)` carries the conversation id as `sessionID`.
- `internal/conversations/store.go` §`Get` + `store_helpers.go:30` — `GetConversation` projects `identity_id` (the schedule-time conv→identity resolver); `ErrConversationNotFound` sentinel for missing rows.

### Industrial-pattern research (decision provenance — Forks 1, 2, 4)
- Transactional outbox (immutable snapshot at enqueue): microservices.io/patterns/data/transactional-outbox.html ; AWS Prescriptive Guidance "Transactional Outbox".
- Snapshot-at-schedule vs resolve-at-send: Klaviyo "Determine recipients at send time vs schedule time" (help.klaviyo.com/hc/en-us/articles/360050216012).
- Deterministic channel priority ("try in order until one succeeds"): Courier "Channel Priority" / "Multi-Channel Routing" ; Novu "Channel Steps" ; AWS Pinpoint origination-number fallback ordering.
- Kill-switch / ops-toggle justification: Martin Fowler "Feature Toggles" (ops toggles & kill-switches) ; Unleash "software kill switches".

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`scheduler_tasks.identity_id` + `.origin_conversation_id`** (migration 0009) already exist and round-trip through `cron.Task`/`CreateTaskParams` — Step 1 needs NO migration.
- **`GetTelegramAccountByIdentity`** SQL + sqlc wrapper already exist — R3 only adds the thin `Store.GetAccountByIdentity` Go wrapper, no new query.
- **`config.envBoolDefault`** + the `AURA_CHANNEL_<NAME>_ENABLED` env-gate pattern (`registry.go`) — the template for the `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` kill-switch.
- **Durable `pending_notifications` queue + sweep** (`dispatch.go`, migration 0013) — Step 2 extends it (add `identity_id`), no new mechanism.
- **`compositeNotifier` route precedence** (`notify.go`) — the no-origin fallback chain is already correct; origin preference sits in front of it.

### Established Patterns
- **Consumer-declared interface + composition-root adapter** (`cron.Notifier`/`SelfSendResolver` adapted in `serve_adapters.go`) — copy this exactly for `ChannelDeliverer`: `cron` declares the seam, `cmd/aura` adapts the live `channels.Registry`. `cron` imports neither `channels` nor `conversations`.
- **Optional-capability via runtime interface assertion** — a started `Channel` MAY also implement `Deliverer`; the Registry checks at runtime and skips channels that don't (no change to `Channel.Start` signature).
- **Fail-soft tri-state delivery** — `(false,nil)`=not-my-user/try-next, `(true,nil)`=delivered/stop, `(false,err)`=owns-but-failed/stop-no-sibling.

### Integration Points
- Schedule time: `task.actionSchedule` → `cronTaskStore.CreateScheduledTask` adapter (resolves conv→identity, persists both).
- Dispatch time: `dispatch.notify` + `dispatch.sweepNotifications` → new `deliverToOrigin(task.IdentityID, …)` → `Registry.DeliverToIdentity` → `Telegram.Deliver`.
- Boot: `bootChannelsAndSetup` must run BEFORE `buildDispatch` so the late-bound Registry pointer is wired into `DispatchDeps` (confirmed `bootChannelsAndSetup` only needs `chat` + `override`, both available earlier).

</code_context>

<specifics>
## Specific Ideas

- The headline bug to fix (Davide, live during Phase 19): "a reminder set via the Telegram bot fired but the notice went to whatsapp/stdout, never back to Telegram — send to the channel that scheduled it; easy to add new channels without reinventing the wheel." The generic `Deliverer` seam IS the "add new channels without reinventing the wheel" requirement.
- Live ground-truth discipline: the live tests assert the DESTINATION (rendered message arrives in the SAME Telegram chat / DB `identity_id` set), never the agent's reply text.

</specifics>

<deferred>
## Deferred Ideas

- **Per-identity channel preference engine** (user-configurable channel order, always-send, per-user quiet hours) — the heavy layer Courier/Novu/Knock monetize. Defer until a real 2nd live push channel + an actual user need exist. Fork 4 ships only the deterministic static order.
- **True group-origin delivery** (a group-set reminder delivers back to the GROUP, not the user's DM) — needs a conversation→channel-address binding (`convID` is a one-way UUIDv5, non-invertible) and breaks the identity-keyed model. Out of scope per SPEC; revisit on a real requirement.
- **whatsapp/email as `Deliverer` channels** (vs MCP self-send routes) — the seam is generic so they plug in later with zero scheduler changes; not built this phase.
- **PRD env-catalog update** — add `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` to the ~60-var index in `prd.md` (housekeeping at phase close, not a blocker).

### Reviewed Todos (not folded)
None — `todo.match-phase 20` returned zero matches.

</deferred>

---

*Phase: 20-scheduler-hardening-full-implementation*
*Context gathered: 2026-06-11*
