# Phase 20: scheduler-hardening-full-implementation - Research

**Researched:** 2026-06-11
**Domain:** Go scheduler dispatch + identity-keyed channel delivery (cron + channels + telegram + conversations seams)
**Confidence:** HIGH — every symbol/line in CONTEXT.md §"Existing Code" was ground-truthed against the live tree today; the design has zero unverified external dependencies (all libraries already vendored, all SQL/sqlc already generated).

> This is a **HOW-only** ground-truth research. The WHAT is locked (7 requirements R1-R7 in 20-SPEC.md, 4 forks D-01..D-06 in 20-CONTEXT.md). No architecture is re-litigated. The single highest-value output is the **Ground-Truth Drift Table** (§2) the planner's `read_first` lists depend on.

## Summary

The design in 20-SPEC.md and the spike is fully consistent with the live codebase. Every scaffolding claim holds: `scheduler_tasks.identity_id text NOT NULL DEFAULT 'local'` AND `origin_conversation_id uuid` (no FK) both exist (migration 0009); `cron.Task`/`CreateTaskParams` carry both; `GetTelegramAccountByIdentity` SQL + sqlc wrapper exist; `conversations.Store.Get` projects `IdentityID` and returns `ErrConversationNotFound`; the tool-call ctx carries the conversation id as `sessionID` (`llm_agent.go:470`); `channels.Channel` is a 4-method lifecycle interface so `Deliverer` is a clean sibling optional capability; `channels.Registry.started` is the late-bound fan-out target under `mu`; `botSender` (Send/Edit) + `tele.ChatID(int64)` are the Telegram send seam; `config.envBoolDefault` exists (unexported).

Three drifts/clarifications the planner MUST absorb (none invalidate the design):
1. **Line-number drift** (all small, +1 to +3 lines from CONTEXT's estimates) — corrected in §2.
2. **`PendingNotification` + `InsertPendingNotificationParams` live in `store_runs.go`** (lines 106-129), NOT `store.go`. The Step-2 migration-0014 threading site is `store_runs.go`.
3. **The Go method is `conversations.Store.Get`** (not `GetConversation` — that is the *sqlc* query name). CONTEXT/SPEC say "GetConversation" loosely; the adapter calls `chat.conv.Get(ctx, convID)`.

**Primary recommendation:** Plan two waves mirroring the SPEC Step 1 / Step 2 split. Wave A (no migration): `Deliverer` interface + `Registry.DeliverToIdentity` + Telegram `Deliver` + `Store.GetAccountByIdentity` + `CreateTaskInput.OriginConversationID` + ctx capture in `actionSchedule` + the `cronTaskStore.CreateScheduledTask` schedule-time identity resolution + `DispatchDeps.ChannelDeliverer` + `deliverToOrigin` (in `notify`) + kill-switch + serve boot-reorder + Step-1 live gate. Wave B (migration 0014): `ALTER TABLE pending_notifications ADD COLUMN identity_id text` + sqlc Insert/Sweep threading + `store_runs.go` projection + `sweepNotifications` route-back + Step-2 live gate. Both waves carry unit + integration tests; both live steps are HARD gates (D-04).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Origin + identity capture at schedule time | API/Backend (`cmd/aura` composition root adapter) | Agent tool (`tools/task.go` reads ctx) | The `cronTaskStore.CreateScheduledTask` adapter owns the conv→identity resolution (consumer-declared-seam idiom); the tool only forwards the ctx sessionID. Keeps `tools` free of a `conversations` import. |
| Identity→channel delivery seam | `internal/channels` (Registry fan-out) | — | The Registry is the late-bound fan-out point over started channels; the scheduler reaches it only through a `cron`-declared interface adapted at the composition root. |
| Telegram push to 1:1 chat | `internal/channels/telegram` | DB (`telegram_accounts`) | Telegram resolves identity→telegram_user_id→`bot.Send(tele.ChatID(id))` under `t.mu`. |
| Dispatch route precedence + kill-switch | `internal/cron` (dispatch) | Composition root (gate + Registry adapter) | `cron` owns the precedence logic but imports neither `channels` nor `conversations`; the gate bool + the `ChannelDeliverer` adapter are injected by `cmd/aura`. |
| Durable deferred/failed retry queue | DB (`pending_notifications` + migration 0014) | `cron` sweep | Step-2 extends the existing durable queue with `identity_id`; no new mechanism. |

## Phase Requirements

(No REQ-IDs in ROADMAP — the authoritative set is the 7 SPEC requirements R1-R7. The planner must map every Rn to a plan/task.)

| ID | Description | Research Support (live symbols enabling it) |
|----|-------------|----------------------------------------------|
| R1 | Origin + snapshot-identity capture at schedule time | `CreateTaskInput` (task.go:54-66) gains `OriginConversationID`; `actionSchedule` (task.go:180-218) reads `toolCallCtx(ctx).sessionID` (result.go:35); `cronTaskStore.CreateScheduledTask` (serve_adapters.go:125-155) calls `chat.conv.Get(ctx, convID)` → `Conversation.IdentityID`, threads both into `cron.CreateTaskParams` (store.go:97-107, both fields already there). |
| R2 | Generic identity-keyed `Deliverer` seam + deterministic fan-out | New `channels.Deliverer` (sibling to `channels.Channel`, channel.go:16-28); `Registry.DeliverToIdentity` over `r.started` under `r.mu` (registry.go:32-34), deterministic order (sort started names). |
| R3 | Telegram `Deliver` + `Store.GetAccountByIdentity` | `GetTelegramAccountByIdentity` sqlc wrapper exists (telegram_accounts.sql.go:31); new thin `Store.GetAccountByIdentity` sibling to `GetAccountByTelegramID` (store.go:169-178); `Deliver` uses `botSender.Send(tele.ChatID(id), text)` (bot.go:45-48) under `t.mu` (bot.go:121, t.bot held). |
| R4 | Dispatch routes to origin (gated kill-switch), identity read from `task.IdentityID` | `DispatchDeps` (dispatch.go:75-87) gains `ChannelDeliverer`; new `deliverToOrigin` helper called from `notify` (dispatch.go:213) reads `task.IdentityID` directly (no dispatch-time conv lookup — Fork 1). |
| R5 | E2E immediate path (Step 1) | Composition-root wiring in `bootServe` (serve.go:136-197) — boot reorder + late-bound Registry; reminders bypass quiet-hours (dispatch.go:202 `task.Kind != KindReminder`). |
| R6 | Deferred + failed sweep route-back (Step 2) | Migration 0014 adds `identity_id text` to `pending_notifications`; `store_runs.go` (106-178) Insert/projection threading; `sweepNotifications` (dispatch.go:278-298) route-back via `deliverToOrigin`. |
| R7 | Route precedence + no-origin fallback (observable contract) | `compositeNotifier.resolveRoute` (notify.go:107-118) is the unchanged fallback chain; `deliverToOrigin` sits in front, falls back to `Notifier.Notify` when no channel owns the identity. |

## Ground-Truth Drift Table (§2 — HIGHEST VALUE)

> Claim from CONTEXT.md §"Existing Code Insights" / spike 8-step → live status as of 2026-06-11. **Use the "Confirmed location" column for `read_first` line anchors.**

| # | Claim (CONTEXT/spike) | Status | Confirmed location (live) |
|---|------------------------|--------|----------------------------|
| 1 | `dispatch.go` §`notify` ≈191-219 | ✅ DRIFT (+0) | `notify` func at **dispatch.go:191**; `Notifier.Notify` call at **dispatch.go:213**; quiet-hours reminder guard at **dispatch.go:202** (`task.Kind != KindReminder`). |
| 2 | `dispatch.go` §`sweepNotifications` ≈278-298 | ✅ CONFIRMED | `sweepNotifications` at **dispatch.go:278**; the `Notifier.Notify` call at **dispatch.go:287**. |
| 3 | `DispatchDeps` ≈75-87 gains `ChannelDeliverer` | ✅ CONFIRMED | struct at **dispatch.go:75-87** (Store/NotificationStore/Notifier/AlertThreshold/QuietHours/QuietHoursEnd). |
| 4 | `insertPendingNotification` callers thread identity | ✅ CONFIRMED | helper at **dispatch.go:237-259**; called from `notify` (207, 215) + must thread `task.IdentityID` for Step 2. |
| 5 | `compositeNotifier` route precedence (per-task→default→stdout) | ✅ CONFIRMED | `resolveRoute` at **notify.go:107-118**; precedence is exactly per-task → `AURA_SCHEDULER_NOTIFY_DEFAULT` → stdout. |
| 6 | `cron.Notifier` / `SelfSendResolver` consumer-declared idiom to copy | ✅ CONFIRMED | `Notifier` iface **notify.go:65-72**; `SelfSendResolver` **notify.go:51-53**; both adapted in `serve_adapters.go` (selfSendResolver:52-80). This is the exact template for `ChannelDeliverer`. |
| 7 | `cron.Task` + `CreateTaskParams` carry `IdentityID` + `OriginConversationID` | ✅ CONFIRMED | `Task` **store.go:57-74** (IdentityID:70, OriginConversationID:71); `CreateTaskParams` **store.go:97-107** (IdentityID:105, OriginConversationID:106). |
| 8 | `CreateTask` defaults `IdentityID='local'` | ✅ CONFIRMED | **store.go:115-118** (`if identityID == "" { identityID = "local" }`); `uuidOrNull(p.OriginConversationID)` at store.go:137 (empty → NULL). |
| 9 | `Registry.started` map under `mu` = late-bound fan-out target | ✅ CONFIRMED | **registry.go:32-34** (`mu sync.Mutex` + `started map[string]Channel`); written under lock at StartAll:76-78. |
| 10 | `enabled`/`envChannelEnabled` env-gate pattern to mirror | ✅ CONFIRMED | **registry.go:110-134** — `AURA_CHANNEL_<upper>_ENABLED`, unset/malformed → true. Pattern for the kill-switch (but see §"Kill-switch placement" — read at the composition root, not in `cron`). |
| 11 | `telegram.Store.GetAccountByTelegramID` = sibling to add `GetAccountByIdentity` next to | ✅ CONFIRMED | **store.go:169-178**; the new `GetAccountByIdentity` wraps the existing `q.GetTelegramAccountByIdentity` (takes `pgtype.UUID`, returns `AuraTelegramAccounts`). |
| 12 | `GetTelegramAccountByIdentity` SQL + sqlc wrapper exist (reuse, NO new query) | ✅ CONFIRMED | SQL **queries/telegram_accounts.sql:11-14**; generated **sqlc/telegram_accounts.sql.go:25-43** (`func (q *Queries) GetTelegramAccountByIdentity(ctx, identityID pgtype.UUID)`). |
| 13 | `tools.CreateTaskInput` has NO origin field (must add) | ✅ CONFIRMED (gap) | **task.go:54-66** — fields end at `NextRunAt`; NO `OriginConversationID`. R1 adds it. |
| 14 | `actionSchedule` ≈180-218 = origin capture site | ✅ DRIFT (+0) | **task.go:180-218**; the `CreateScheduledTask` call is at **task.go:203-215** (the `CreateTaskInput` literal to extend with `OriginConversationID`). |
| 15 | `shell_exec.go` §`shellSessionKey` ≈327-330 = ctx-sessionID precedent | ✅ DRIFT (+1) | **shell_exec.go:328-333** (`shellSessionKey`); reads `toolCallCtx(ctx).sessionID`, returns "" for bare ctx. Same idiom in `web_fetch.go:98` and `todo.go:94`. |
| 16 | `llm_agent.go:470` carries conv id as sessionID | ✅ DRIFT (was :449 in spike, :470 in CONTEXT) | **llm_agent.go:470** — `tools.WithToolCallContext(ctx, a.sessionID, call.ID, a.runDir, a.previewCap)`. `a.sessionID` == conversation id. |
| 17 | `conversations.GetConversation` projects identity_id; `ErrConversationNotFound` sentinel | ✅ CONFIRMED (method name = `Get`) | Go method is **`conversations.Store.Get`** (store.go:157-170), maps `pgx.ErrNoRows`→`ErrConversationNotFound` (store.go:47). Returns `Conversation{IdentityID}` (store.go:104, projected at store_helpers.go:30). **The sqlc query is `GetConversation`; the Go method is `Get`.** |
| 18 | `cronTaskStore.CreateScheduledTask` adapter = composition-root resolution site | ✅ CONFIRMED (gap) | **serve_adapters.go:125-155** — currently does NOT thread `IdentityID`/`OriginConversationID` into `cron.CreateTaskParams` (they're omitted from the literal at 130-144). R1 adds the `chat.conv.Get` resolution + both fields. Adapter needs a `*conversations.Store` injected (currently only `pool` + `*cron.Store`). |
| 19 | `serve.go` bootChannelsAndSetup BEFORE buildDispatch (boot reorder) | ⚠️ **REORDER REQUIRED** (currently reversed) | `buildDispatch` at **serve.go:143**; `bootChannelsAndSetup` at **serve.go:187** — TODAY buildDispatch runs FIRST. Plan must move `bootChannelsAndSetup` above `buildDispatch` (line 142→187 block) and pass `reg` into `buildDispatch`. Confirmed safe: `bootChannelsAndSetup` needs only `chat` (run/pool/conv/cfg/identity) + `override`, all available at serve.go:137. |
| 20 | Migration 0013 = `pending_notifications`; 0014 is next free slot | ✅ CONFIRMED | latest is **0013_pending_notifications.up/.down.sql**; **0014 is free**. |
| 21 | `PendingNotification`/`InsertParams`/projection = Step-2 threading site | ✅ DRIFT (file) | These live in **`store_runs.go`** (PendingNotification:106-118, InsertPendingNotificationParams:120-129, `pendingNotificationFromRow`:248-261), **NOT** `store.go`. Plan's read_first must list `store_runs.go`. |
| 22 | spike Step-2 schema = `origin_conversation_id uuid REFERENCES…ON DELETE SET NULL` | ❌ **STALE — SPEC SUPERSEDES** | SPEC R6 + Fork 1 lock the column to **`identity_id text` (NO FK)**, mirroring `scheduler_tasks.identity_id`. **Do NOT follow the spike's `origin_conversation_id uuid REFERENCES` line** — it predates Fork 1. See §"Migration 0014 mechanics". |

**Net:** all 22 claims resolve to CONFIRMED or small line-drifts EXCEPT #19 (a required boot reorder, already anticipated by the SPEC) and #22 (a stale spike schema the SPEC explicitly supersedes). Both are called out in the SPEC; no surprises.

## Exact Signatures the New Code Must Match (§3)

```go
// channels.Channel — the existing lifecycle contract (channel.go:16-28).
// Deliverer is a NEW SIBLING optional capability — a started Channel MAY also
// implement it; the Registry runtime-asserts and skips channels that don't.
type Channel interface {
    Name() string
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    IsHealthy() bool
}

// NEW (internal/channels/deliver.go) — the optional capability + tri-state contract.
type Deliverer interface {
    // (false,nil)=not my user → try next; (true,nil)=delivered → stop;
    // (false,err)=owns-but-failed → stop, do NOT try siblings.
    Deliver(ctx context.Context, identityID, text string) (delivered bool, err error)
}

// Registry late-bound fan-out target (registry.go:23-34) — read started under mu:
type Registry struct {
    channels        map[string]Channel
    enabledOverride func(name string) (enabled, ok bool)
    mu              sync.Mutex
    started         map[string]Channel // ← DeliverToIdentity fans out over THIS, sorted
}
// NEW method on *Registry:
//   func (r *Registry) DeliverToIdentity(ctx context.Context, identityID, text string) (bool, error)
// Snapshot r.started under r.mu into a slice, sort by channel name (deterministic),
// release the lock, then for each: if d,ok := ch.(Deliverer); ok { call Deliver }.

// botSender — the Telegram send seam (bot.go:45-48). *tele.Bot satisfies it.
type botSender interface {
    Send(to tele.Recipient, what any, opts ...any) (*tele.Message, error)
    Edit(msg tele.Editable, what any, opts ...any) (*tele.Message, error)
}
// tele.ChatID(int64) is the Recipient for a 1:1 chat (precedent: agui_subscriber.go:88,
// hitl.go:84, bot_dispatch.go:412). Telegram.Deliver reads the live bot under t.mu
// (bot.go:121 `mu sync.Mutex` + `bot *tele.Bot`) — guard with t.mu.Lock(); the bot may
// be nil if the channel never started → return (false,nil) (not my user, can't push).

// telegram.Store.GetAccountByIdentity (NEW, sibling to GetAccountByTelegramID:169-178):
//   func (s *Store) GetAccountByIdentity(ctx context.Context, identityID string) (Account, error)
// parseUUID("identity_id", identityID) → q.GetTelegramAccountByIdentity(ctx, pgtype.UUID).
// Map pgx.ErrNoRows → wrapped pgx.ErrNoRows (mirror GetAccountByTelegramID:172-174) so
// Deliver can errors.Is(err, pgx.ErrNoRows) → return (false,nil).
// Account.IdentityID is a UUID string; Account.TelegramUserID is int64 → tele.ChatID(that).

// cron.Notifier (notify.go:65-72) — the EXISTING fallback seam deliverToOrigin sits in front of:
type Notifier interface {
    Notify(ctx context.Context, route NotifyRoute, recipient, text string) error
}

// NEW cron-local consumer-declared seam (mirror SelfSendResolver:51-53 EXACTLY):
type ChannelDeliverer interface {
    DeliverToIdentity(ctx context.Context, identityID, text string) (delivered bool, err error)
}
// var _ cron.ChannelDeliverer = (*channels.Registry)(nil)  // assert at the composition root.
// cron imports NEITHER channels NOR conversations.

// cron.CreateTaskParams (store.go:97-107) — already has both fields; the adapter just sets them:
type CreateTaskParams struct {
    Kind TaskKind; Spec ScheduleSpec; Payload []byte; StepBudget int
    NextRunAt time.Time; NotifyRoute string; Status string
    IdentityID           string  // ← R1: set from resolved conv→identity
    OriginConversationID string  // ← R1: set from ctx sessionID
}

// tools.CreateTaskInput (task.go:54-66) — R1 ADDS one field:
//   OriginConversationID string  // forwarded from toolCallCtx(ctx).sessionID

// WithToolCallContext (result.go:26) + read pattern (shell_exec.go:328-333):
func WithToolCallContext(ctx context.Context, sessionID, toolCallID, runDir string, previewCap int) context.Context
// In actionSchedule: read via the same idiom shellSessionKey uses —
//   if tc, ok := toolCallCtx(ctx); ok { originConvID = tc.sessionID }  // "" for bare ctx.

// conversations.Store.Get (store.go:157-170) — the schedule-time resolver:
//   func (s *Store) Get(ctx context.Context, conversationID string) (Conversation, error)
// returns ErrConversationNotFound on miss; Conversation.IdentityID is the resolved key.
```

### Migration up/down + sqlc header style (mirror 0013)

```sql
-- 0014_pending_notifications_identity.up.sql  (next free slot; 0013 created the table)
-- Source: Phase 20 R6/Fork 1. Snapshot the stable identity_id (NO FK, plain text,
-- mirrors scheduler_tasks.identity_id) so a quiet-hours-deferred / failed notification
-- routes back to its origin channel after a sweep. Survives a deleted origin conversation.
ALTER TABLE aura.pending_notifications ADD COLUMN identity_id text;
COMMENT ON COLUMN aura.pending_notifications.identity_id IS
    'Stable owning identity snapshot (Phase 20, Fork 1): the channel-independent delivery key for the deferred/failed sweep route-back. Plain text, no FK — survives a deleted origin conversation. NULL for legacy/CLI rows → falls back to notify_route.';
-- The existing aura_app DML grant (0013:26) already covers the new column — no new GRANT.

-- 0014_pending_notifications_identity.down.sql
ALTER TABLE aura.pending_notifications DROP COLUMN IF EXISTS identity_id;
```

Then extend the two sqlc queries (`queries/pending_notifications.sql`) + `sqlc generate`:
- `InsertPendingNotification`: add `identity_id` to the column list + `$9` value + RETURNING list.
- `SweepDueNotifications`: add `identity_id` to both SELECT lists.
- `MarkNotification*`: unchanged.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| identity→telegram chat resolution | a new SQL query | existing `GetTelegramAccountByIdentity` sqlc wrapper | SPEC constraint "reuse, do not duplicate"; the query + generated Go already exist. |
| conv→identity resolution | a dispatch-time lookup seam | schedule-time `chat.conv.Get(convID).IdentityID` snapshot (Fork 1) | the snapshot survives a deleted conversation; dispatch then reads `task.IdentityID` with zero lookup. |
| kill-switch env parse | a bespoke parser | read `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` at the composition root via the `envBoolDefault` semantics (unset/malformed→true), pass as a `DispatchDeps.PreferOriginChannel bool` | `cron` must not import `config`; the gate is resolved once at boot. |
| started-channel fan-out | map iteration | snapshot `r.started` under `r.mu`, sort by name, iterate the slice (Fork 4) | Go map order is nondeterministic → untestable the moment a 2nd Deliverer lands. |
| fail-soft channel skip | a registry of capabilities | runtime `ch.(Deliverer)` assertion | a Channel that can't push simply doesn't implement Deliverer → zero registry change for a new channel. |

**Key insight:** Every "deceptively complex" piece (identity key resolution, durable retry, route precedence, MCP self-send fallback) already ships. This phase is wiring + one column + one interface + one method per package — no new mechanism.

## Common Pitfalls

### Pitfall 1: Kill-switch read inside `cron` would force a `config` import
**What goes wrong:** Naively calling `config.envBoolDefault` from `cron/dispatch.go` couples the scheduler to `config` (and `envBoolDefault` is unexported anyway).
**How to avoid:** Resolve the bool once at the composition root (`cmd/aura`, which has `chat.cfg`/env) and inject it as a `DispatchDeps.PreferOriginChannel bool`. Default-on semantics (unset/malformed → true) are applied at the read site. `cron` stays import-clean.
**Warning sign:** a new `internal/config` import in `internal/cron`.

### Pitfall 2: `deliverToOrigin` must honor BOTH the gate AND `notify` precedence order
**What goes wrong:** Preferring the channel even when the task has an explicit `notify=whatsapp` route → R7 regression (explicit route must win, channel skipped).
**How to avoid:** Gate condition is precisely: `PreferOriginChannel && task.NotifyRoute == "" && task.IdentityID != "" && task.IdentityID != "local"` → try `DeliverToIdentity`. Any explicit `NotifyRoute` skips the channel entirely. `'local'`/un-onboarded → not-my-user → fall back to `Notifier.Notify`.
**Warning sign:** a unit test where `notify=whatsapp` on a Telegram-origin task delivers via Telegram.

### Pitfall 3: owns-but-failed must NOT fall back to a sibling route (double-delivery)
**What goes wrong:** On `(false, err)` from `DeliverToIdentity`, falling back to `Notifier.Notify` → the same-channel retry (Step 2) later succeeds → the user gets the message twice.
**How to avoid:** `(false, err)` → insert a `failed` pending row (threaded with `task.IdentityID`) and return; do NOT call `Notifier.Notify`. Only `(false, nil)` (not-my-user) falls back to the route. (SPEC R4 + Constraint "owned-but-failed does NOT fall back".)
**Warning sign:** the owns-but-fails unit test sees `Notifier.Notify` called.

### Pitfall 4: `Deliver` reading a nil bot after Stop
**What goes wrong:** A delivery racing channel shutdown dereferences `t.bot == nil` → panic.
**How to avoid:** Lock `t.mu`, read `bot := t.bot`, unlock; if `bot == nil` return `(false, nil)` (can't push → not-my-user, let the route fall-back handle it). Mirror the `t.sender`/`Stop` mu discipline (bot.go:285-311).
**Warning sign:** a goroutine-race or nil-deref in the Offline-bot Deliver test under `-race`.

### Pitfall 5: bare-ctx scheduling must not panic (CLI / unit tests)
**What goes wrong:** Reading `toolCallCtx(ctx).sessionID` when no tool-call ctx is set.
**How to avoid:** Use the `if tc, ok := toolCallCtx(ctx); ok` two-value form (shell_exec.go:329) — "" origin for bare ctx. The adapter then persists NULL origin + `identity_id='local'` (R1 acceptance: "no panic, no error").
**Warning sign:** the bare-ctx schedule test panics or sets a non-empty origin.

### Pitfall 6: pgtype boundary — identity_id is `text` in pending_notifications but `uuid` in telegram_accounts
**What goes wrong:** `pending_notifications.identity_id` is plain `text` (Fork 1, survives a deleted conv), but `telegram_accounts.identity_id` is `uuid`. The `Deliver` path parses the text identity into a `pgtype.UUID` for the account lookup — a malformed identity (e.g. `'local'`) must surface as not-my-user, not an error.
**How to avoid:** In `Store.GetAccountByIdentity`, a `parseUUID` failure on a non-UUID identity (`'local'`) should map to not-found behavior; `Deliver` returns `(false, nil)`. Test with `identityID="local"`.
**Warning sign:** a `'local'`-identity task errors instead of falling back to the route.

## Runtime State Inventory

> This is a code/schema phase, not a rename/migration of stored keys — but Step 2 adds a column, so the inventory is relevant.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | `pending_notifications` rows existing at upgrade time will have `identity_id = NULL` (new column, no backfill). NULL → falls back to `notify_route` (R7) — byte-identical to today. | None — NULL is the correct legacy default; no data migration. |
| Live service config | None. The Telegram bot config (`TELEGRAM_BOT_TOKEN`) + `telegram_accounts` link are unchanged (consumed, not modified). | None. |
| OS-registered state | None. | None — verified: no scheduler tasks registered outside the DB; the daemon polls `scheduler_tasks`. |
| Secrets/env vars | One NEW env var `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` (bool, default true). No secret. | Add to PRD env catalog (housekeeping, not a blocker — Deferred per CONTEXT). |
| Build artifacts | `sqlc generate` must re-run after the `pending_notifications.sql` query edits (Step 2) → regenerates `pending_notifications.sql.go`. | Run `sqlc generate`; commit the regenerated file. |

## Code Examples (verified patterns from the live tree)

### Reading the conversation id from the tool-call ctx (mirror shell_exec.go:328-333)
```go
// In tools/task.go actionSchedule — capture the origin conversation id:
var originConvID string
if tc, ok := toolCallCtx(ctx); ok {
    originConvID = tc.sessionID // == conversation id (llm_agent.go:470); "" for bare ctx
}
// thread into CreateTaskInput{ ..., OriginConversationID: originConvID }
```

### Schedule-time conv→identity resolution (in the cronTaskStore adapter, serve_adapters.go:125)
```go
// cronTaskStore gains a *conversations.Store field (injected at newCronTaskStore).
func (s *cronTaskStore) CreateScheduledTask(ctx context.Context, in tools.CreateTaskInput) (tools.ScheduledTask, error) {
    identityID := ""               // empty → cron.Store defaults to 'local' (store.go:115-118)
    if in.OriginConversationID != "" && s.conv != nil {
        conv, err := s.conv.Get(ctx, in.OriginConversationID)
        if err == nil {
            identityID = conv.IdentityID
        } else if !errors.Is(err, conversations.ErrConversationNotFound) {
            return tools.ScheduledTask{}, fmt.Errorf("resolve origin identity: %w", err)
        }
        // ErrConversationNotFound → leave identityID="" (defaults to 'local'); no hard fail.
    }
    created, err := s.store.CreateTask(ctx, cron.CreateTaskParams{
        // ...existing fields...
        IdentityID:           identityID,
        OriginConversationID: in.OriginConversationID,
    })
    // ...
}
```

### Deterministic fan-out (Registry.DeliverToIdentity)
```go
func (r *Registry) DeliverToIdentity(ctx context.Context, identityID, text string) (bool, error) {
    r.mu.Lock()
    names := make([]string, 0, len(r.started))
    snap := make(map[string]Channel, len(r.started))
    for n, ch := range r.started { names = append(names, n); snap[n] = ch }
    r.mu.Unlock()
    sort.Strings(names) // deterministic (Fork 4) — never map order
    for _, n := range names {
        d, ok := snap[n].(Deliverer)
        if !ok { continue } // channel can't push → skip (zero change for a new channel)
        delivered, err := d.Deliver(ctx, identityID, text)
        if err != nil { return false, err }   // owns-but-failed → stop, no siblings
        if delivered { return true, nil }      // first-delivers-wins
    }
    return false, nil // not-my-user across all → caller falls back to the route
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| spike: dispatch-time `IdentityForConversation` seam | Fork 1: snapshot identity at schedule time, dispatch reads `task.IdentityID` | discuss-phase 2026-06-11 (D-01/D-02) | `cron` needs NO conversation-lookup adapter at dispatch — one snapshot, fewer moving parts. **The planner must NOT add an `IdentityForConversation` dep.** |
| spike: `pending_notifications.origin_conversation_id uuid REFERENCES…ON DELETE SET NULL` | SPEC R6: `identity_id text` (no FK) | 2026-06-11 (Fork 1) | survives a deleted conversation; mirrors `scheduler_tasks.identity_id`. **Use the SPEC schema, not the spike's.** |
| map-iteration fan-out (spike risk #3) | deterministic sort-by-name (Fork 4) | 2026-06-11 (D-05) | testable; per-identity preference engine DEFERRED. |

**Deprecated/outdated:** the spike's Step-2 §"add `origin_conversation_id uuid REFERENCES`" line and its R4 §"`IdentityForConversation` seam" — both superseded by the SPEC. Everything else in the spike is current.

## Project Constraints (from CLAUDE.md)

| Directive | Application to Phase 20 |
|-----------|------------------------|
| Owned-surface coverage ≥85% hard floor (overrides PRD 75/60) | New files (`channels/deliver.go`, `telegram/deliver.go`, `cron/deliver.go` or the dispatch.go additions, the adapter changes) must carry unit coverage; report the combined tag-matrix figure. |
| `go vet` + `go build` + `go test` + `-race` green; `golangci-lint` 0 | Post-edit gate after every Go file; run `golangci-lint run ./...` before any dead-code claim. |
| Every touched file ≤600 LOC (NO GOD CLASS) | `dispatch.go` is 298 LOC — adding `deliverToOrigin` may push it; Claude's Discretion (CONTEXT) allows a new `internal/cron/deliver.go`. `serve_adapters.go` is 343 LOC — adding the conv field + resolution stays under 600 but watch it. |
| No-skip-as-green in CI | The `db_integration` migration-0014 round-trip + the two LIVE gates must actually run; skip-helpers `t.Fatal` under `$CI`. |
| `AURA_<DOMAIN>_<UNIT>` env convention | `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` complies (DOMAIN=SCHEDULER). |
| Consumer-declared-interface + composition-root-adapter idiom | `ChannelDeliverer` declared in `cron`, adapted from `*channels.Registry` in `cmd/aura`; mirrors `SelfSendResolver`. |
| Deferred-tool pattern unaffected (`task` stays non-deferred) | `task.go` Spec keeps `Deferred: false` (task.go:131) — do not touch. |
| Read before edit / never suppose | Every symbol in this doc is line-anchored; the executor reads the anchored file before editing. |

## Validation Architecture

> Nyquist Dimension 8 is enabled. This section defines the strategy the planner lifts into VALIDATION.md.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + table-driven; goleak TestMain in `channels`/`telegram`/`cron`; `-race` mandatory. |
| Config file | none (go test) — tiers gated by build tags `db_integration`, and the live recipe is manual+CDP. |
| Quick run command | `go test ./internal/channels/... ./internal/cron/... ./internal/agent/tools/...` |
| Full suite command | `go test -race -tags db_integration ./internal/...` (+ the two manual LIVE gates) |
| Integration invocation | `go test -tags db_integration -race -run TestDispatch ./internal/cron -count=1` (+ derive `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` from `POSTGRES_PASSWORD`, per MEMORY). |

### Phase Requirements → Test Map
| Req | Behavior | Tier | Automated Command / Gate | Analog test file to mirror | File exists? |
|-----|----------|------|--------------------------|----------------------------|--------------|
| R1 | origin+identity capture: convC/identityI persisted; bare ctx → NULL+'local'; deleted-conv leaves identity | unit (tool) + **db_integration** (round-trip) | `go test ./internal/agent/tools/ -run TestActionSchedule...`; `go test -tags db_integration ./internal/cron -run TestCreateTask...` | tools: `result_test.go` (`WithToolCallContext` helper, ctxWith); cron: `store_test.go` + `dispatch_integration_test.go` (`migratedPool`) | ❌ new tests (Wave 0) |
| R2 | registry fan-out: first-delivers-wins (deterministic order), not-my-user fall-through, owns-but-fails stops, not-started never asked | unit | `go test ./internal/channels/ -run TestRegistryDeliver...` | `registry_test.go` (`fakeChannel` → add `fakeDeliverer` implementing both Channel+Deliverer) | ❌ new tests |
| R3 | Telegram Deliver: found→send recorded; ErrNoRows→(false,nil); send err→(false,err); 'local'→not-my-user | unit (Offline bot + fake Store + recording botSender) | `go test ./internal/channels/telegram/ -run TestDeliver...` | `artifact_test.go` / `renderer_test.go` (recording botSender double + `tele.ChatID`); `store_integration_test.go` for the real Store path | ❌ new tests |
| R4 | deliverToOrigin precedence + kill-switch: channel delivers⇒Notifier NOT called; explicit route⇒channel skipped; no owner⇒route; owns-fails⇒failed pending+no Notifier; gate off⇒route; nil deps⇒legacy | unit | `go test ./internal/cron/ -run TestDeliverToOrigin...` | `dispatch_test.go` (`captureNotifier`, `fakeNotificationStore`, `fakeCompleter`; add a `fakeChannelDeliverer`) | ❌ new tests |
| R5 | Step 1 immediate path | **LIVE (hard gate, D-04)** | manual: schedule "remind me in 1 min" in a Telegram DM → reminder text arrives in the SAME chat after ~70s; assert `scheduler_tasks.origin_conversation_id` AND `identity_id` set | CDP Telegram live-test harness (MEMORY `reference_cdp_telegram_live_test_harness`); spike §"Live verification recipe" | n/a (live) |
| R6 | Step 2 deferred/failed sweep route-back | **db_integration** (regression) + **LIVE (hard gate, D-04)** | integration: insert pending row w/ identity → sweep → fake Deliverer receives it; LIVE: force `AURA_SCHEDULER_QUIET_HOURS` to cover now, schedule an agent_job, sweep after window → arrives in origin Telegram chat | cron: `dispatch_test.go` `TestDispatchSweepNotifications...` + `dispatch_integration_test.go`; live recipe from 19-11-PLAN.md:178-191 (the proven quiet-hours forcing pattern) | ❌ new + live |
| R6 | migration 0014 up/down clean | **db_integration** | `migratedPool(t)` applies 0014; a down test reverts | `scheduler_integration_test.go` migration harness | ❌ new |
| R7 | route precedence: explicit notify=whatsapp on TG-origin → whatsapp; CLI reminder → route; nil deps byte-identical | unit | covered in the R4 dispatch precedence table tests | `dispatch_test.go` | ❌ new |

### Sampling Rate
- **Per task commit:** `go test -race ./internal/<touched-package>/` (the touched package's unit tier).
- **Per wave merge:** `go test -race -tags db_integration ./internal/cron/... ./internal/channels/...` + `golangci-lint run ./...` + coverage gate.
- **Phase gate:** full tag matrix green + BOTH live gates (R5 Step 1, R6 Step 2) signed off + owned-surface ≥85% before `/gsd-verify-work`.

### Ground-Truth Assertions for the LIVE steps (assert the DESTINATION, never the agent reply)
- **Step 1 (R5):** the rendered reminder message appears in the SAME Telegram chat (CDP-observed via web.telegram.org, the proven harness) — NOT stdout/whatsapp. DB ground truth: `SELECT origin_conversation_id, identity_id FROM aura.scheduler_tasks WHERE kind='reminder' ORDER BY created_at DESC LIMIT 1` → both non-NULL, identity_id = the chat's identity. NEVER assert on `r.Reply`/the agent's confirmation text.
- **Step 2 (R6):** with `AURA_SCHEDULER_QUIET_HOURS` forced to cover "now", a deferred agent_job notification is swept after the window and lands in the origin Telegram chat (CDP-observed). DB ground truth: the `pending_notifications` row's `identity_id` is set and `status` transitions to `delivered`.

### Regression Guards
- **Integration DB round-trip** (both steps) — origin+identity persisted and read back; migration 0014 up/down clean.
- **Fake channel** (both steps) — `fakeDeliverer`/`fakeChannelDeliverer` proves fan-out + precedence with zero live bot.
- **Kill-switch-off byte-identical** — `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL=false` (or `PreferOriginChannel:false`) → channel skipped, `Notifier.Notify` called with the route (the existing `dispatch_test.go` assertions still hold unchanged).
- **Nil-deps legacy guard** — `DispatchDeps{ChannelDeliverer: nil}` → today's behavior; mirror `TestDispatchDefaultsAlertThreshold` / the nil-NotificationStore guards.

### Coverage target
Owned-surface ≥85% hard floor. New files carrying it: `internal/channels/deliver.go` (interface — trivial), `internal/channels/registry.go` (DeliverToIdentity), `internal/channels/telegram/deliver.go` + `store.go` (GetAccountByIdentity), `internal/cron/deliver.go` (or dispatch.go additions), `serve_adapters.go` (conv resolution — covered behaviourally; `cmd/aura` glue is excluded from the floor per CLAUDE.md but the adapter logic should still have a unit test where feasible).

### Wave 0 Gaps
- [ ] `internal/channels/registry_test.go` — add `fakeDeliverer` (Channel+Deliverer) + `TestRegistryDeliverToIdentity*` (4 cases).
- [ ] `internal/channels/telegram/deliver_test.go` — Offline bot + fake Store + recording botSender (3 cases + 'local' not-my-user).
- [ ] `internal/cron/dispatch_test.go` (or `deliver_test.go`) — add `fakeChannelDeliverer` + `TestDeliverToOrigin*` (6 cases incl. kill-switch + nil deps).
- [ ] `internal/agent/tools/task_test.go` — `actionSchedule` ctx-sessionID capture + bare-ctx "" (reuse `ctxWith`/`WithToolCallContext` from result_test.go).
- [ ] `internal/cron/` migration-0014 + identity round-trip `db_integration` test (mirror `dispatch_integration_test.go` `migratedPool`).
- [ ] Live gate scripts/recipe for Step 1 + Step 2 (reuse the CDP harness + the 19-11 quiet-hours forcing pattern).

### Nyquist sampling rationale (minimum set that fully observes the new behavior)
The new behavior decomposes into four orthogonal seams, each observable by one focused tier:
1. **Fan-out ordering + tri-state** — observable ONLY at the `Registry.DeliverToIdentity` unit level (4 cases cover first-wins/fall-through/owns-fails/not-started). No higher tier adds information about ordering.
2. **Telegram identity→chat resolution + send** — observable at the `telegram.Deliver` unit level with the Offline bot (the only place the `ErrNoRows`/send-error/'local' branches are distinguishable). The live gate does NOT need to re-test these branches.
3. **Dispatch precedence + kill-switch + pending-on-fail** — observable at the `deliverToOrigin` unit level (6 cases). This is where R4+R7 fully collapse; the live gate adds nothing here.
4. **End-to-end identity snapshot + route-back** — observable ONLY live (R5/R6 hard gates) because it crosses the schedule-time adapter → DB → dispatch → real bot boundary that no fake spans. db_integration covers the DB round-trip + migration; the two live gates cover the real-bot destination.

The minimum non-redundant set is therefore: **17 unit cases (4+4+6+3) + 2 db_integration tests (round-trip + migration up/down) + 2 live gates**. Adding e2e coverage of the unit-observable branches (1-3) would be redundant; omitting either live gate would leave the cross-boundary snapshot/route-back unobserved (Nyquist under-sampling — the exact class of bug that produced the Phase-19 headline).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Postgres (aura.* schema) | R1/R6 db_integration + migration 0014 | ✓ (Docker stack) | PG (compose.yaml :5432) | — (required) |
| golang-migrate (0014 apply) | migration up/down | ✓ | shipped in `internal/db/migrations` harness | — |
| sqlc | regenerate `pending_notifications.sql.go` | ✓ | v1.31.1 (per generated header) | — |
| Live Telegram bot + onboarded account | R5/R6 LIVE gates | ✓ (TELEGRAM_BOT_TOKEN + onboarded identity, Phase 13) | telebot.v4 | — (live gate is a hard requirement, D-04) |
| CDP harness (Chrome :9222 + playwright) | R5/R6 live destination assertion | ✓ | per MEMORY `reference_cdp_telegram_live_test_harness` | manual visual confirmation in the Telegram client |
| WSL toolchain (`go test -race`, golangci-lint, coverage gate) | quality gates | ✓ | golangci-lint v2.12.2, go race native in WSL | Windows w64devkit w/ BASH_ENV (slower) |

**Missing dependencies with no fallback:** none — all required tooling is already in the dev environment.

## Package Legitimacy Audit

> No external packages are installed by this phase. All dependencies (`gopkg.in/telebot.v4`, `github.com/jackc/pgx/v5`, `github.com/google/uuid`) are already vendored and in active production use. **No slopcheck / registry verification needed — zero new installs.**

| Package | Registry | Disposition |
|---------|----------|-------------|
| (none — no new dependencies) | — | n/a |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Kill-switch is cleanest as a `DispatchDeps.PreferOriginChannel bool` resolved at the composition root (vs. an inline `os.Getenv` in `cron`) | Pitfall 1 / Don't-Hand-Roll | LOW — either satisfies the SPEC; the bool-field keeps `cron` import-clean. Executor's call (matches the SelfSendResolver precedent of resolving env at the root). |
| A2 | `cronTaskStore` gains a `*conversations.Store` field (injected at `newCronTaskStore`) | Code Examples | LOW — `chat.conv` is available at the wiring site (serve.go/chat boot); this is the only way the adapter can call `.Get`. Confirmed `chatEnv.conv` exists (chat.go:51). |
| A3 | `'local'` identity (a non-UUID) parses-fail in `GetAccountByIdentity` → not-my-user | Pitfall 6 | LOW — `telegram_accounts.identity_id` is `uuid`; `'local'` is not a UUID, so the lookup can't match a real account anyway. Test pins it. |

**These three are HOW-level executor choices within Claude's Discretion (CONTEXT §"Claude's Discretion"), not user-facing decisions.** No assumption touches a locked fork.

## Open Questions

1. **`deliverToOrigin` file placement (dispatch.go vs new `internal/cron/deliver.go`)**
   - What we know: `dispatch.go` is 298 LOC; adding the helper + the gate logic likely keeps it <600 but tightens it.
   - Recommendation: per CONTEXT §"Claude's Discretion", a new `internal/cron/deliver.go` is the cleaner home (co-locates the channel-preference concern, keeps dispatch.go focused on the run lifecycle). Executor's call under the ≤600-LOC rule.

2. **Deterministic order: sort-by-name vs explicit `Priority()`**
   - What we know: with one Deliverer (Telegram) the order is unobservable; D-05 only forbids map iteration.
   - Recommendation: sort-by-name (lower LOC, zero new interface method). Pick `Priority()` only if a 2nd channel lands this phase (it won't — out of scope).

## Sources

### Primary (HIGH confidence)
- Live codebase (ground-truthed 2026-06-11): `internal/cron/{dispatch,notify,store,store_runs}.go`, `internal/channels/{channel,registry}.go` + `registry_test.go`, `internal/channels/telegram/{bot,store}.go`, `internal/db/queries/{telegram_accounts,pending_notifications}.sql` + generated sqlc, `internal/db/migrations/{0009,0013}_*.sql`, `internal/agent/tools/{task,shell_exec,result}.go`, `internal/agent/llm_agent.go`, `internal/conversations/{store,store_helpers}.go`, `cmd/aura/{serve,serve_channels,serve_adapters,chat}.go`, `internal/cron/{dispatch_test,dispatch_integration_test}.go`, `internal/config/config.go`.
- `20-SPEC.md` (7 locked requirements + amended R1/R2/R4 + Constraints + Acceptance) — the WHAT contract.
- `20-CONTEXT.md` (4 forks D-01..D-06 + canonical refs + code insights).

### Secondary (MEDIUM confidence)
- `.planning/spikes/reminder-agnostic-channel.md` (execution-ready 8-step design — current EXCEPT the two SPEC-superseded items flagged in §State of the Art).
- MEMORY: `reference_cdp_telegram_live_test_harness`, `reference_db_knowledge_integration_test_invocation`, `19-11-PLAN.md` quiet-hours live recipe.

### Tertiary (LOW confidence)
- none — no claim in this research rests on unverified web/training sources.

## Metadata

**Confidence breakdown:**
- Ground-truth drift: HIGH — every symbol opened and line-anchored today.
- Signatures: HIGH — copied verbatim from the live tree.
- Seam wiring + boot order: HIGH — `chatEnv` fields + `bootServe`/`bootChannelsAndSetup` read in full; the one reorder confirmed safe.
- Migration 0014: HIGH — 0013 pattern + 0009 schema read; the SPEC-vs-spike schema conflict resolved explicitly.
- Validation: HIGH — analog test files named, fakes identified, Nyquist set derived from the seam decomposition.

**Research date:** 2026-06-11
**Valid until:** ~2026-07-11 (stable — internal codebase; the only invalidator is a concurrent edit to the named files. Re-confirm the line anchors if another session touches `cron`/`channels`/`serve_adapters` before planning.)
