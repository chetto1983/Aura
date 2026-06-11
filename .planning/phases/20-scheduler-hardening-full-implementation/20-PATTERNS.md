# Phase 20: scheduler-hardening-full-implementation - Pattern Map

**Mapped:** 2026-06-11
**Files analyzed:** 14 (5 new + 9 modified) + 6 test targets
**Analogs found:** 20 / 20 (every file has an exact or close in-repo analog — this is a wiring phase, zero greenfield)

> Source of truth for line anchors: `20-RESEARCH.md` §2 Ground-Truth Drift Table (ground-truthed 2026-06-11). This map adds the **copy-the-shape excerpts** the executor lifts. Where RESEARCH and this map disagree on a line number, RESEARCH wins (re-confirm if a parallel session touched `cron`/`channels`/`serve_adapters`/`tools/task.go` — `task.go` is flagged for concurrent edit; read its CURRENT state).

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| **NEW** `internal/channels/deliver.go` | interface (capability contract) | request-response | `internal/channels/channel.go:16-28` (`Channel` iface) | exact (sibling iface, same package) |
| **NEW** `internal/channels/telegram/deliver.go` | channel method (push) | request-response | `bot.go:285-311` (`Stop` mu discipline) + `bot.go:324-331` (`sender`) + `store.go:169-178` (`GetAccountByTelegramID`) | role-match |
| **NEW** `internal/cron/deliver.go` (or extend `dispatch.go`) | dispatch helper | event-driven (route precedence) | `notify.go:104-118` (`resolveRoute` precedence) + `dispatch.go:191-219` (`notify`) | role-match |
| **NEW** `internal/db/migrations/0014_*.up.sql` / `.down.sql` | migration | DDL | `0013_pending_notifications.up/.down.sql` | exact (same table, sibling column) |
| **MOD** `internal/channels/registry.go` (`DeliverToIdentity`) | registry fan-out | request-response (fan-out) | `registry.go:62-106` (`StartAll`/`StopAll` started-map under `mu`) | exact (same struct, same lock idiom) |
| **MOD** `internal/channels/telegram/store.go` (`GetAccountByIdentity`) | store wrapper | CRUD (read) | `store.go:169-178` (`GetAccountByTelegramID`) | exact (sibling wrapper) |
| **MOD** `internal/agent/tools/task.go` (`OriginConversationID` + ctx read) | agent tool | request-response | `shell_exec.go:328-333` (`shellSessionKey` ctx read) | exact (same ctx idiom) |
| **MOD** `internal/cron/dispatch.go` (`ChannelDeliverer` dep + routing) | dispatch deps | event-driven | `notify.go:51-72` (`SelfSendResolver`/`Notifier` consumer-declared seam) | exact (same idiom) |
| **MOD** `internal/cron/store_runs.go` (thread `identity_id`) | store projection | CRUD (insert/sweep) | `store_runs.go:106-178` (`PendingNotification`/`InsertParams`/`InsertPendingNotification`) | exact (same funcs) |
| **MOD** `internal/db/queries/pending_notifications.sql` + regen sqlc | SQL query | CRUD | `queries/telegram_accounts.sql:11-14` (sibling query already added) | exact (mechanical sqlc edit) |
| **MOD** `cmd/aura/serve_adapters.go` (`cronTaskStore` conv→identity) | composition-root adapter | request-response | `serve_adapters.go:52-80` (`selfSendResolver`) + `:107-155` (`cronTaskStore`) | exact (same adapter, extend it) |
| **MOD** `cmd/aura/serve.go` (boot reorder + late-bound Registry) | composition root | wiring | `serve.go:142-197` (`bootServe` build order) | exact (same func, reorder) |

## Pattern Assignments

---

### NEW `internal/channels/deliver.go` (interface, request-response)

**Analog:** `internal/channels/channel.go:16-28` — the `Channel` lifecycle interface. Put `Deliverer` in the SAME package as a sibling optional capability (a started `Channel` MAY also implement it; the Registry runtime-asserts).

**Copy-the-shape** (the package's narrow-interface idiom — short doc comment, one method, no struct):
```go
// channel.go:16-28 shape to mirror:
type Channel interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsHealthy() bool
}
```
New body (tri-state contract is the load-bearing doc — copy it verbatim from RESEARCH §3 / SPEC R2):
```go
// Deliverer is an OPTIONAL channel capability: a started Channel MAY also
// implement it. The Registry runtime-asserts ch.(Deliverer) and skips a channel
// that does not — zero registry change for a future channel.
type Deliverer interface {
	// (false,nil)=not my user → try next; (true,nil)=delivered → stop;
	// (false,err)=owns-but-failed → stop, do NOT try siblings.
	Deliver(ctx context.Context, identityID, text string) (delivered bool, err error)
}
```

**Gotcha:** the tri-state semantics are the contract the dispatch precedence (R4) and the registry fan-out (R2) both depend on — `(false,err)` must NOT mean "try the next channel" (Pitfall 3, double-delivery). Spell it in the doc comment so the meaning travels with the type.

---

### MOD `internal/channels/registry.go` — `DeliverToIdentity` fan-out (registry, request-response fan-out)

**Analog:** `registry.go:62-106` — `StartAll`/`StopAll` already snapshot `r.started` under `r.mu` into a local map, release the lock, then iterate. `DeliverToIdentity` copies the snapshot-under-lock idiom but adds a **deterministic sort** (Fork 4) before iterating.

**Copy-the-shape** (the existing `StopAll` snapshot-then-release, registry.go:87-93):
```go
func (r *Registry) StopAll(ctx context.Context) error {
	r.mu.Lock()
	toStop := make(map[string]Channel, len(r.started))
	for name, ch := range r.started {
		toStop[name] = ch
	}
	r.mu.Unlock()
	// ... iterate toStop ...
}
```
New method (verbatim from RESEARCH §"Code Examples" — already validated against this struct):
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
		if !ok { continue } // channel can't push → skip
		delivered, err := d.Deliver(ctx, identityID, text)
		if err != nil { return false, err }  // owns-but-failed → stop, no siblings
		if delivered { return true, nil }     // first-delivers-wins
	}
	return false, nil // not-my-user across all → caller falls back to the route
}
```

**Gotcha:** `sort` is a NEW import in `registry.go`. The kill-switch (`AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL`) is NOT read here — that gate lives at the composition root and the cron-side `deliverToOrigin` (Pitfall 1). The `envChannelEnabled` pattern at registry.go:119-134 is the env-gate *template* (mirror it for the bool semantics) but the actual kill-switch read happens in `cmd/aura`, not here.

---

### NEW `internal/channels/telegram/deliver.go` — `Telegram.Deliver` (channel method, request-response)

**Analogs (three combined):**
- **`bot.go:285-311` / `bot.go:324-331`** — read the live `bot` under `t.mu` (lock, copy `bot := t.bot`, unlock; nil-after-Stop guard).
- **`store.go:169-178`** — `GetAccountByTelegramID` error-classification shape (the new `GetAccountByIdentity` is its sibling).
- **`bot.go:45-48`** — the `botSender` seam (`Send(to tele.Recipient, what any, ...)`); `tele.ChatID(int64)` is the 1:1 Recipient.

**Copy-the-shape** (the `Stop` nil-bot mu discipline, bot.go:285-291 — replicate this exact lock/copy/unlock/guard):
```go
func (t *Telegram) Stop(ctx context.Context) error {
	t.mu.Lock()
	bot := t.bot
	started := t.started
	t.mu.Unlock()
	if !started || bot == nil {
		return nil
	}
	// ...
}
```
New `Deliver` (assemble: lock-copy-bot → resolve identity→account → send):
```go
func (t *Telegram) Deliver(ctx context.Context, identityID, text string) (bool, error) {
	t.mu.Lock()
	bot := t.bot
	t.mu.Unlock()
	if bot == nil { // never started / already stopped → can't push, not-my-user
		return false, nil
	}
	if t.deps.Store == nil {
		return false, nil
	}
	acct, err := t.deps.Store.GetAccountByIdentity(ctx, identityID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) { // not my user → try next channel
			return false, nil
		}
		return false, fmt.Errorf("telegram deliver: resolve identity %s: %w", identityID, err)
	}
	if _, err := bot.Send(tele.ChatID(acct.TelegramUserID), text); err != nil {
		return false, fmt.Errorf("telegram deliver: send to %d: %w", acct.TelegramUserID, err) // owns-but-failed
	}
	return true, nil
}
```
Add the compile assertion next to `var _ channels.Channel = (*Telegram)(nil)` (bot.go:30):
```go
var _ channels.Deliverer = (*Telegram)(nil)
```

**Gotcha:** (1) `bot.Send` takes a `botSender` — in `Deliver` you read the concrete `*tele.Bot` (`t.bot`), which satisfies `botSender`; the test injects a `docBot`-style double via a seam. (2) `'local'` identity is a non-UUID — `GetAccountByIdentity` must map a `parseUUID` failure to not-found behavior so `Deliver` returns `(false,nil)`, NOT an error (Pitfall 6). (3) Reading `t.deps.Store` is field-stable (set at construct), but `t.bot` MUST be read under `t.mu` (race with `Stop`, Pitfall 4).

---

### MOD `internal/channels/telegram/store.go` — `GetAccountByIdentity` wrapper (store, CRUD read)

**Analog:** `store.go:169-178` — `GetAccountByTelegramID` is the exact sibling. The SQL + sqlc are already generated (`GetTelegramAccountByIdentity`, queries/telegram_accounts.sql:11-14, takes `pgtype.UUID`).

**Copy-the-shape** (store.go:169-178 verbatim — the error-classification body to mirror):
```go
func (s *Store) GetAccountByTelegramID(ctx context.Context, telegramUserID int64) (Account, error) {
	row, err := s.q.GetTelegramAccountByTelegramID(ctx, telegramUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Account{}, fmt.Errorf("get telegram account %d: %w", telegramUserID, pgx.ErrNoRows)
		}
		return Account{}, fmt.Errorf("get telegram account %d: %w", telegramUserID, err)
	}
	return accountFromRow(row), nil
}
```
New wrapper — adds the `parseUUID` boundary (store.go:231-237, already in the file) because the query takes `pgtype.UUID`:
```go
func (s *Store) GetAccountByIdentity(ctx context.Context, identityID string) (Account, error) {
	idID, err := parseUUID("identity_id", identityID)
	if err != nil {
		// non-UUID identity (e.g. 'local') can never match a real account →
		// surface as not-found so Deliver returns (false,nil), not an error.
		return Account{}, fmt.Errorf("get telegram account by identity %q: %w", identityID, pgx.ErrNoRows)
	}
	row, err := s.q.GetTelegramAccountByIdentity(ctx, idID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Account{}, fmt.Errorf("get telegram account by identity %q: %w", identityID, pgx.ErrNoRows)
		}
		return Account{}, fmt.Errorf("get telegram account by identity %q: %w", identityID, err)
	}
	return accountFromRow(row), nil
}
```

**Gotcha:** mapping the `parseUUID` failure to a wrapped `pgx.ErrNoRows` (NOT a distinct error) is what lets `Deliver` use a single `errors.Is(err, pgx.ErrNoRows)` branch to mean "not my user" for both the no-account case AND the `'local'` case (Pitfall 6). Reuse the existing `parseUUID` helper (store.go:231) — do NOT add a new one.

---

### MOD `internal/agent/tools/task.go` — `CreateTaskInput.OriginConversationID` + ctx read (agent tool, request-response)

> **CONCURRENT-EDIT FLAG:** `task.go` may be under edit by a parallel session. Read its CURRENT state before editing — the anchors below were ground-truthed 2026-06-11 but the line numbers may have drifted. The *shape* is stable; re-locate by symbol name (`CreateTaskInput`, `actionSchedule`, the `CreateScheduledTask` literal).

**Analog:** `shell_exec.go:328-333` — `shellSessionKey` reads `sessionID` from the tool-call ctx via the two-value `toolCallCtx(ctx)` form (returns "" for bare ctx). `result.go:35-38` is the `toolCallCtx` accessor; `result.go:26` is `WithToolCallContext`.

**Copy-the-shape** (shell_exec.go:328-333 verbatim — the bare-ctx-safe read idiom):
```go
func shellSessionKey(ctx context.Context) string {
	if tc, ok := toolCallCtx(ctx); ok {
		return tc.sessionID
	}
	return ""
}
```
Edit 1 — add the field to `CreateTaskInput` (task.go:54-66, after `NextRunAt`):
```go
type CreateTaskInput struct {
	// ...existing fields...
	NextRunAt            time.Time
	OriginConversationID string // forwarded from toolCallCtx(ctx).sessionID; "" for bare ctx
}
```
Edit 2 — in `actionSchedule` (task.go:179-217), read the ctx and thread it into the literal at task.go:202-214:
```go
originConvID := ""
if tc, ok := toolCallCtx(ctx); ok {
	originConvID = tc.sessionID // == conversation id (llm_agent.go:470); "" for bare ctx
}
created, err := t.Store.CreateScheduledTask(ctx, CreateTaskInput{
	// ...existing fields...
	NextRunAt:            next,
	OriginConversationID: originConvID,
})
```

**Gotcha:** (1) MUST use the two-value `if tc, ok := toolCallCtx(ctx); ok` form — a bare ctx (CLI / unit test) must yield "" with no panic (Pitfall 5). (2) `tools` stays free of any `conversations` import — the tool only forwards the raw sessionID; the conv→identity resolution happens in the `cmd/aura` adapter (consumer-declared-seam idiom). (3) The `task` Spec stays `Deferred: false` (task.go:130) — do NOT touch it.

---

### MOD `cmd/aura/serve_adapters.go` — `cronTaskStore` conv→identity resolution (composition-root adapter, request-response)

**Analog:** `serve_adapters.go:52-80` (`selfSendResolver` — the consumer-declared-seam adapter pattern) AND `serve_adapters.go:107-155` (`cronTaskStore` itself — the file you EXTEND). The adapter gains a `*conversations.Store` field and resolves `origin_conversation_id → identity_id` before threading both into `cron.CreateTaskParams`.

**Copy-the-shape** (the existing `CreateScheduledTask`, serve_adapters.go:130-144 — the literal you extend with `IdentityID`/`OriginConversationID`):
```go
created, err := s.store.CreateTask(ctx, cron.CreateTaskParams{
	Kind: cron.TaskKind(in.Kind),
	Spec: cron.ScheduleSpec{ /* ... */ },
	Payload:     in.Payload,
	StepBudget:  in.StepBudget,
	NextRunAt:   in.NextRunAt,
	NotifyRoute: in.NotifyRoute,
	Status:      status,
})
```
New (add the field at serve_adapters.go:107-110, inject at newCronTaskStore:115-117, resolve in CreateScheduledTask — verbatim from RESEARCH §"Code Examples"):
```go
type cronTaskStore struct {
	pool  *pgxpool.Pool
	store *cron.Store
	conv  *conversations.Store // NEW — schedule-time conv→identity resolver
}

func (s *cronTaskStore) CreateScheduledTask(ctx context.Context, in tools.CreateTaskInput) (tools.ScheduledTask, error) {
	identityID := "" // empty → cron.Store defaults to 'local' (store.go:115-118)
	if in.OriginConversationID != "" && s.conv != nil {
		conv, err := s.conv.Get(ctx, in.OriginConversationID)
		if err == nil {
			identityID = conv.IdentityID
		} else if !errors.Is(err, conversations.ErrConversationNotFound) {
			return tools.ScheduledTask{}, fmt.Errorf("resolve origin identity: %w", err)
		}
		// ErrConversationNotFound → leave identityID="" → 'local'; no hard fail.
	}
	created, err := s.store.CreateTask(ctx, cron.CreateTaskParams{
		// ...existing fields...
		Status:               status,
		IdentityID:           identityID,
		OriginConversationID: in.OriginConversationID,
	})
	// ...
}
```

**Gotcha:** (1) `cron.CreateTaskParams.IdentityID` + `.OriginConversationID` ALREADY exist (store.go:105-106) — `cron.Store.CreateTask` already defaults empty `IdentityID`→`'local'` (store.go:115-118) and `uuidOrNull(OriginConversationID)` empty→NULL (store.go:137). You only set the fields. (2) `conversations.Store.Get` is the GO method (store.go:157) — `GetConversation` is the *sqlc* query name; do not call a non-existent `GetConversation` Go method. (3) `s.conv` must be injected at `newCronTaskStore` from `chat.conv` (chatEnv field, available at the wiring site). (4) A `conversations` import is NOW legitimate in `cmd/aura` (it is the composition root) — this is the ONE place that import is allowed.

---

### MOD `internal/cron/dispatch.go` — `DispatchDeps.ChannelDeliverer` + route through `deliverToOrigin` (dispatch deps, event-driven)

**Analog:** `notify.go:51-72` — the `SelfSendResolver` + `Notifier` consumer-declared interfaces are the EXACT template for the new cron-local `ChannelDeliverer` seam (cron declares it, `cmd/aura` adapts `*channels.Registry`; cron imports neither `channels` nor `conversations`). The two call sites to route are `notify` (dispatch.go:213) and `sweepNotifications` (dispatch.go:287).

**Copy-the-shape** (notify.go:65-72 — the consumer-declared interface idiom to mirror):
```go
type Notifier interface {
	Notify(ctx context.Context, route NotifyRoute, recipient, text string) error
}
```
New cron-local seam (declare in dispatch.go or the new deliver.go) + add to `DispatchDeps` (dispatch.go:75-87):
```go
type ChannelDeliverer interface {
	DeliverToIdentity(ctx context.Context, identityID, text string) (delivered bool, err error)
}

type DispatchDeps struct {
	// ...existing fields: Store/NotificationStore/Notifier/AlertThreshold/QuietHours/QuietHoursEnd...
	ChannelDeliverer    ChannelDeliverer // nil → legacy route-only (regression guard)
	PreferOriginChannel bool             // AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL, resolved at the root
}
```
**Copy-the-shape for `deliverToOrigin`** — it sits IN FRONT of the existing `Notifier.Notify` at dispatch.go:213. The gate condition is precise (Pitfall 2). The current notify tail (dispatch.go:213-218) becomes the fallback:
```go
// the precedence helper (new internal/cron/deliver.go per Claude's Discretion):
func (d *Dispatch) deliverToOrigin(ctx context.Context, task Task, runID, text string) (handled bool) {
	if !d.deps.PreferOriginChannel || d.deps.ChannelDeliverer == nil {
		return false // gate off / nil dep → caller falls through to Notifier.Notify
	}
	if task.NotifyRoute != "" || task.IdentityID == "" || task.IdentityID == "local" {
		return false // explicit route wins; un-owned identity → route fallback (R7)
	}
	delivered, err := d.deps.ChannelDeliverer.DeliverToIdentity(ctx, task.IdentityID, text)
	if err != nil {
		// owns-but-failed → queue a failed pending row (same-channel retry, threaded
		// with task.IdentityID); do NOT fall back to Notifier (Pitfall 3 / R4).
		if perr := d.insertPendingNotification(ctx, task, runID, text, time.Now().UTC(), "failed", 0, err.Error()); perr != nil {
			slog.Warn("persist failed origin-channel notification", "task", task.ID, "err", perr)
		}
		return true
	}
	return delivered // true → done; false → not-my-user → caller falls back to route
}
```

**Gotcha:** (1) `cron` must NOT import `config` or `channels` — `PreferOriginChannel bool` is resolved once at the composition root (`config.envBoolDefault` semantics, default-true) and injected (Pitfall 1). (2) Gate order is load-bearing: explicit `NotifyRoute` ALWAYS wins (channel skipped) — R7 regression if you check the channel first (Pitfall 2). (3) Both `notify` AND `sweepNotifications` route through this helper; for Step 2 the sweep keys on the pending row's `identity_id` (threaded by the 0014 work), not `task.IdentityID`. (4) `dispatch.go` is 298 LOC — adding the helper here risks the ≤600 ceiling on touch; CONTEXT §Claude's Discretion sanctions a new `internal/cron/deliver.go` (cleaner home; keeps dispatch focused on run lifecycle).

---

### MOD `internal/cron/store_runs.go` — thread `identity_id` through `PendingNotification` (store projection, CRUD)

**Analog:** `store_runs.go:106-178` — `PendingNotification` (106-118), `InsertPendingNotificationParams` (120-129), `InsertPendingNotification` (149-178), `pendingNotificationFromRow` (248-261) are ALL in THIS file (not `store.go` — RESEARCH drift #21). Mirror the existing `RunID`/`NotifyRoute` field threading for the new `IdentityID`.

**Copy-the-shape** (the existing projection + insert, store_runs.go:106-129 + 164-173 — add `IdentityID` alongside `NotifyRoute`):
```go
type PendingNotification struct {
	ID          string
	RunID       string
	NotifyRoute string
	Body        string
	// ... add: IdentityID string
}
type InsertPendingNotificationParams struct {
	RunID       string
	NotifyRoute string
	Body        string
	// ... add: IdentityID string
}
// in InsertPendingNotification, alongside NotifyRoute: text(p.NotifyRoute):
row, err := s.q.InsertPendingNotification(ctx, sqlc.InsertPendingNotificationParams{
	ID:          newUUID(),
	RunID:       runID,
	NotifyRoute: text(p.NotifyRoute),
	// ... add: IdentityID: text(p.IdentityID)  (or textOrNull — text() already empty→NULL via pgtype)
	Body:        p.Body,
	// ...
})
// in pendingNotificationFromRow (248-261), alongside NotifyRoute: r.NotifyRoute.String:
// ... add: IdentityID: r.IdentityID.String
```
And `insertPendingNotification` (dispatch.go:237-259) threads `task.IdentityID` into `InsertPendingNotificationParams`:
```go
_, err := d.deps.NotificationStore.InsertPendingNotification(ctx, InsertPendingNotificationParams{
	RunID:       runID,
	NotifyRoute: task.NotifyRoute,
	IdentityID:  task.IdentityID, // NEW — the Step-2 route-back key
	Body:        body,
	// ...
})
```

**Gotcha:** (1) `pending_notifications.identity_id` is `text` (NO FK, Fork 1) so the sqlc-generated field is a nullable `pgtype.Text` — use `text(...)`/`.String` boundary helpers (already in the package), NOT a `pgtype.UUID` conversion (Pitfall 6: this column is text, unlike `telegram_accounts.identity_id` which is uuid). (2) Legacy rows at upgrade have `identity_id = NULL` → falls back to `notify_route` (byte-identical to today) — correct, no backfill. (3) The `PendingNotificationStore` interface (dispatch.go:64-69) signature is unchanged (the params struct gains a field, not the method).

---

### MOD `internal/db/queries/pending_notifications.sql` + `sqlc generate` (SQL, CRUD)

**Analog:** `queries/telegram_accounts.sql:11-14` (`GetTelegramAccountByIdentity` — the sibling column-list edit already shipped there shows the mechanical shape). Add `identity_id` to `InsertPendingNotification` (column list + `$9` value + RETURNING) and to both SELECT lists in `SweepDueNotifications`; `MarkNotification*` unchanged. Then run `sqlc generate` and commit `pending_notifications.sql.go`.

**Gotcha:** the `$N` placeholder renumbering on Insert must be exact — read the CURRENT `pending_notifications.sql` before editing (it was not opened in this map; RESEARCH §"Migration" names the three edits). The generated `AuraPendingNotifications` row struct gains `IdentityID pgtype.Text` — that is what `pendingNotificationFromRow` reads.

---

### NEW migration `0014_pending_notifications_identity.up.sql` / `.down.sql` (migration, DDL)

**Analog:** `0013_pending_notifications.up.sql` / `.down.sql` — the up adds a COMMENT + relies on the existing 0013 `aura_app` DML grant (no new GRANT); the down is a single `DROP ... IF EXISTS`.

**Copy-the-shape** (RESEARCH §"Migration up/down" — verbatim, SPEC-superseded schema is `text` NO FK, NOT the spike's `uuid REFERENCES`):
```sql
-- 0014_pending_notifications_identity.up.sql
-- Source: Phase 20 R6/Fork 1. Snapshot the stable identity_id (NO FK, plain text,
-- mirrors scheduler_tasks.identity_id) so a quiet-hours-deferred / failed notification
-- routes back to its origin channel after a sweep. Survives a deleted origin conversation.
ALTER TABLE aura.pending_notifications ADD COLUMN identity_id text;
COMMENT ON COLUMN aura.pending_notifications.identity_id IS
    'Stable owning identity snapshot (Phase 20, Fork 1): the channel-independent delivery key for the deferred/failed sweep route-back. Plain text, no FK — survives a deleted origin conversation. NULL for legacy/CLI rows → falls back to notify_route.';
-- The existing aura_app DML grant (0013:26) already covers the new column — no new GRANT.
```
```sql
-- 0014_pending_notifications_identity.down.sql
ALTER TABLE aura.pending_notifications DROP COLUMN IF EXISTS identity_id;
```

**Gotcha:** **DO NOT** follow the spike's `origin_conversation_id uuid REFERENCES … ON DELETE SET NULL` line — it predates Fork 1 and is explicitly SPEC-superseded (RESEARCH drift #22 / §State of the Art). Column is `identity_id text`, no FK. 0014 is the next free slot (0013 is the latest, confirmed).

---

### MOD `cmd/aura/serve.go` — boot reorder + late-bound Registry as `ChannelDeliverer` (composition root, wiring)

**Analog:** `serve.go:142-197` (`bootServe`) — TODAY `buildDispatch` (143) runs BEFORE `bootChannelsAndSetup` (187). RESEARCH drift #19 confirms the reorder is REQUIRED and safe (`bootChannelsAndSetup` needs only `chat` + `override`, both available at :137).

**Copy-the-shape** (the current order, serve.go:142-143 then :187 — invert so the Registry exists before `buildDispatch` reads it):
```go
// TODAY (must invert):
store := cron.New(chat.pool)
dispatch := buildDispatch(chat, store)        // :143 — reads deps
// ... 40 lines ...
reg, setupSrv := bootChannelsAndSetup(...)    // :187 — builds the Registry
```
New order — build the Registry first, pass it (+ the resolved kill-switch bool) into `buildDispatch`:
```go
store := cron.New(chat.pool)
reg, setupSrv := bootChannelsAndSetup(ctx, chat, channelOverride) // MOVED UP
dispatch := buildDispatch(chat, store, reg)                       // reg → ChannelDeliverer
```
And in `buildDispatch` (wherever `DispatchDeps` is assembled), wire the Registry + the gate resolved via `config.envBoolDefault` semantics at the root:
```go
deps.ChannelDeliverer = reg // *channels.Registry satisfies cron.ChannelDeliverer
deps.PreferOriginChannel = chat.cfg.SchedulerPreferOriginChannel // or envBoolDefault at the read site, default true
// assert at the root: var _ cron.ChannelDeliverer = (*channels.Registry)(nil)
```

**Gotcha:** (1) The Registry pointer is *late-bound* — only the `*channels.Registry` value must exist at `buildDispatch` time; the per-channel `Deliverer` capability is resolved at delivery, not at build. (2) The compile-time assertion `var _ cron.ChannelDeliverer = (*channels.Registry)(nil)` belongs at the composition root (cmd/aura), NOT in cron (cron must not import channels). (3) Confirm `buildDispatch`'s signature change ripples to its other call site if any (`bootChatEnv`/CLI). (4) Resolve `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` once here via the `config.envBoolDefault(key, true)` pattern (config.go:405-415) — default TRUE.

---

## Shared Patterns

### Consumer-declared interface + composition-root adapter
**Source:** `internal/cron/notify.go:51-72` (`SelfSendResolver`/`Notifier`) + `cmd/aura/serve_adapters.go:52-80` (`selfSendResolver` adapter, with `var _ cron.SelfSendResolver = (*selfSendResolver)(nil)`).
**Apply to:** the new `cron.ChannelDeliverer` seam (declared in `cron`, adapted from `*channels.Registry` in `cmd/aura`) AND the `tools.taskStore` → `cronTaskStore` chain. `cron` imports neither `channels` nor `conversations`; `tools` imports neither `cron` nor `conversations`. Every cross-package reach is a consumer-side interface adapted at the root.
```go
// the idiom (notify.go:51-53 + serve_adapters.go:56):
type SelfSendResolver interface { Resolve(bareName string) (SelfSendTool, bool) }
var _ cron.SelfSendResolver = (*selfSendResolver)(nil)
```

### Snapshot-under-lock then iterate (no work under the mutex)
**Source:** `internal/channels/registry.go:87-93` (`StopAll`).
**Apply to:** `Registry.DeliverToIdentity` — copy `r.started` under `r.mu`, release, then (deterministic sort +) iterate. Never hold `r.mu` across a `Deliver` call (a channel push can block/network).

### SQLSTATE / pgx.ErrNoRows classification, never string match
**Source:** `internal/channels/telegram/store.go:169-178` (`errors.Is(err, pgx.ErrNoRows)`) + `store.go:239-244` (`isUniqueViolation` via `errors.As` + `pgErr.Code`).
**Apply to:** `GetAccountByIdentity` (map both no-row AND non-UUID `'local'` to wrapped `pgx.ErrNoRows`); `Telegram.Deliver` (`errors.Is` → not-my-user); the `cronTaskStore` adapter (`errors.Is(err, conversations.ErrConversationNotFound)` → soft fallback).

### Bare-ctx-safe tool-call context read
**Source:** `internal/agent/tools/shell_exec.go:328-333` (`shellSessionKey`) + `result.go:35-38` (`toolCallCtx`).
**Apply to:** `actionSchedule` origin capture — two-value `if tc, ok := toolCallCtx(ctx); ok` form, "" for bare ctx, no panic (Pitfall 5).

### Default-on kill-switch via envBoolDefault
**Source:** `internal/config/config.go:405-415` (`envBoolDefault`) + `internal/channels/registry.go:119-134` (`envChannelEnabled` — unset/malformed → true).
**Apply to:** `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` — resolve at the composition root (default true), inject as `DispatchDeps.PreferOriginChannel bool`. `cron` must NOT read env (Pitfall 1).

---

## Test Analog Map (Wave 0 gaps — every new test has a named in-repo double to copy)

| New test | Analog double / helper (path:line) | Copy-the-shape |
|----------|------------------------------------|----------------|
| `internal/channels/registry_test.go` → `TestRegistryDeliverToIdentity*` (4 cases) | `registry_test.go:14-53` `fakeChannel` (records Start/Stop under `mu`) | add a `fakeDeliverer` embedding/extending `fakeChannel` that ALSO implements `Deliver(ctx, id, text) (bool, error)` with a configurable tri-state return; register 2+ to assert deterministic (sorted) order + first-delivers-wins + owns-fails-stops + not-started-never-asked. |
| `internal/channels/telegram/deliver_test.go` → `TestDeliver*` (3 + 'local') | `artifact_test.go:18-48` `docBot` (recording `botSender`: `Send`/`Edit` + `recorded()` accessor under `mu`); Offline bot via `Deps{Offline:true}` (bot.go:227-233) | inject a `docBot`-style recorder + a fake `*Store` (or stub `GetAccountByIdentity`); assert found→`Send(tele.ChatID(id), text)` recorded + `(true,nil)`; `ErrNoRows`→`(false,nil)`; send-err→`(false,err)`; `identityID="local"`→`(false,nil)`. |
| `internal/cron/dispatch_test.go` (or `deliver_test.go`) → `TestDeliverToOrigin*` (6 cases) | `dispatch_test.go:84-100` `captureNotifier` (records routes/texts/errs) + `:38-82` `fakeNotificationStore` (records `inserted` InsertParams) + `:28-36` `fakeCompleter` | add a `fakeChannelDeliverer` (configurable `(bool,error)` return + call recorder); assert: channel-delivers⇒`captureNotifier` NOT called; explicit-route⇒channel skipped + Notifier called; `'local'`⇒Notifier called w/ route; owns-fails⇒`fakeNotificationStore.inserted` has a `failed` row w/ `IdentityID` + Notifier NOT called; `PreferOriginChannel:false`⇒channel skipped + Notifier called; nil `ChannelDeliverer`⇒legacy. |
| `internal/agent/tools/task_test.go` → `TestActionScheduleCapturesOrigin*` | `result.go:26` `WithToolCallContext` (see usage `swarm/runner_adapter_test.go:17`); `swarm/runner_adapter_test.go:16-18` `withToolCtx` helper shape | drive `actionSchedule` with `tools.WithToolCallContext(ctx, "conv-C", "call-1", t.TempDir(), cap)` → assert the captured `CreateTaskInput.OriginConversationID == "conv-C"` (via a fake `taskStore` recorder); bare ctx → `""`, no panic. |
| `internal/cron/` migration-0014 + identity round-trip (`db_integration`) | `internal/conversations/store_test.go:49` `migratedPool(t)` (applies migrations via `envOrSkip` DSNs) — sibling copies in `agui`/`askuser`/`toolinvocations` | `migratedPool(t)` applies 0014; insert a pending row WITH `identity_id`; `SweepDueNotifications` → assert `identity_id` round-trips on the projection; a down-migration test reverts cleanly. (Derive `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` from `POSTGRES_PASSWORD` per MEMORY; `db_integration` tag.) |
| LIVE Step 1 / Step 2 (R5/R6 hard gates) | CDP harness (MEMORY `reference_cdp_telegram_live_test_harness`, `D:/tmp/tg_cdp.py`); quiet-hours forcing from `19-11-PLAN.md:178-191` | manual + CDP: assert the DESTINATION (rendered msg in the SAME Telegram chat) + DB ground truth (`scheduler_tasks.origin_conversation_id` AND `identity_id` set; `pending_notifications.identity_id` set, `status='delivered'`). NEVER assert on `r.Reply`. |

**Test discipline (CLAUDE.md):** `goleak` TestMain already present in `channels`/`telegram`/`cron`; run `-race` on every touched package; no-skip-as-green (the `db_integration` tier `t.Fatal`s under `$CI` when env is unset). Owned-surface coverage ≥85% hard floor across the full tag matrix.

## No Analog Found

None. Every file maps to an exact or close in-repo analog — this is a wiring + one-column + one-interface + one-method-per-package phase with zero new mechanism (RESEARCH §"Don't Hand-Roll" key insight). `cmd/aura` glue (`serve.go`/`serve_adapters.go`) is excluded from the coverage floor per CLAUDE.md but the adapter logic still carries a unit test where feasible.

## Metadata

**Analog search scope:** `internal/channels` (+ `/telegram`), `internal/cron`, `internal/agent/tools`, `internal/conversations`, `internal/config`, `internal/db/{queries,migrations}`, `cmd/aura`, plus the named `*_test.go` doubles.
**Files scanned:** 18 source + 6 test analogs (channel.go, registry.go, registry_test.go, telegram/{bot,store}.go, artifact_test.go, cron/{dispatch,notify,store,store_runs}.go, dispatch_test.go, agent/tools/{task,result,shell_exec}.go, conversations/store.go, config/config.go, serve.go, serve_adapters.go, 0013 up/down, telegram_accounts.sql, swarm/runner_adapter_test.go, conversations/store_test.go).
**Pattern extraction date:** 2026-06-11
