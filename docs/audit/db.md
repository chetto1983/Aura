# Audit: internal/db

**Verdict:** needs-work — two real bugs (context not wired to migration runner; search silently drops spilled turns), one dead helper.

**Counts:** critical 0 / high 2 / medium 1 / low 0

---

## Findings

### [HIGH][BUG] Context cancellation ignored by migration runner — db-1

**Location:** `internal/db/migrate.go:41-58`, `internal/db/reset.go:16-36`, `internal/db/migrate_steps.go:18-35`

**Confidence:** high

**Detail:**
`Migrate`, `Reset`, and `MigrateSteps` all accept a `context.Context` parameter but the context is never wired to the golang-migrate runner. `migrate.NewWithSourceInstance` (v4.19.1) does not accept a context; `m.Steps`, `m.Up`, and `m.Down` likewise have no context overload — the library uses a `GracefulStop chan bool` for cooperative interruption. As a result, a cancelled or timed-out context (e.g. the CLI's 30-second command timeout, or a test deadline) has no effect on an in-progress migration run. A stuck migration — e.g. waiting for an advisory lock held by another session — cannot be aborted by the caller's context.

`EnsureRoles` is not affected: it passes `ctx` to every `pool.Exec` call.

**Suggested fix:**
Spawn a goroutine that selects on `ctx.Done()` and sends to `m.GracefulStop` when the context is cancelled:

```go
stopCh := make(chan struct{})
go func() {
    select {
    case <-ctx.Done():
        m.GracefulStop <- true
    case <-stopCh:
    }
}()
defer close(stopCh)
```

Insert this block after `migrate.NewWithSourceInstance` succeeds in each of the three functions, before the `m.Up` / `m.Down` / `m.Steps` call.

---

### [HIGH][BUG] `SearchConversationTurns` silently excludes spilled turns — db-2

**Location:** `internal/db/queries/conversation_turns.sql:31-37`, `internal/db/sqlc/conversation_turns.sql.go:130-135`, `internal/db/migrations/0006_conversation_turns_fts.up.sql`

**Confidence:** high

**Detail:**
When a turn's content exceeds `AURA_CONVERSATION_TURN_CAP_BYTES`, `conversations.Store.maybeSpill` stores the content in a sidecar file and sets `content = NULL` in the DB (confirmed by migration 0005 comment and `store.go:275`). The FTS query uses `WHERE content % $1` — the pg_trgm `%` operator evaluates to NULL when `content IS NULL`, which PostgreSQL treats as false, so all spilled turns are silently excluded from every search result. The GIN index (`0006_conversation_turns_fts.up.sql`) is built on `content` without a partial predicate so it also wastes index entries for these NULL rows.

The issue is marked a LOCKED cross-slice contract (D-A5-03 / Req#13), which makes a silent regression here particularly harmful: Telegram `/search` and CLI `/search` both hit this path and will never surface large assistant turns.

**Suggested fix:**
Add `AND content IS NOT NULL` to the WHERE clause (the `%` guard already implies it, but being explicit prevents confusion):

```sql
WHERE content IS NOT NULL
  AND content % $1
```

Optionally convert the GIN index to a partial index:

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS conversation_turns_content_trgm
    ON aura.conversation_turns USING GIN (content gin_trgm_ops)
    WHERE content IS NOT NULL;
```

After the SQL change, regenerate the sqlc bindings (`sqlc generate`).

---

### [MEDIUM][DEAD-CODE] `redactDSNUsername` is package-private and only called within `internal/db` — db-3

**Location:** `internal/db/db.go:101-109`

**Confidence:** medium

**Detail:**
`redactDSNUsername` is an unexported helper. Its only call site is `redactDSN` at `db.go:97`. It is not referenced anywhere else in the repo (confirmed by grep across `D:/Aura`). This is not dead code per se — it is legitimately called by `redactDSN` — but it is a micro-function that does a redundant `url.Parse` (the same DSN was already parsed at the start of `redactDSN`). The double-parse is O(1) and not a performance issue, but the split is purely cosmetic: folding `redactDSNUsername` inline into `redactDSN` would remove the second parse and the helper entirely, reducing the surface the test suite has to cover.

Classified as dead-code-adjacent (the function exists solely to avoid a local variable in the caller) rather than a true unused symbol.

**Suggested fix:**
Inline the username extraction into `redactDSN`:

```go
// replace:
username := url.PathEscape(redactDSNUsername(s))
// with:
username := url.PathEscape(u.User.Username())  // u already parsed above
```

Then delete `redactDSNUsername`. The separate test `TestRedactDSNUsername_ParseErrorReturnsEmpty` in `db_unit_test.go` becomes redundant and should be removed with it.

---

## What was checked and found clean

- **Resource leaks**: All `rows.Close()` calls are deferred immediately after `pool.Query` succeeds. `WithTx` correctly rolls back on both error and panic paths. `migrationsFS` is embed-only, no file handles to close. Migration runners are `defer m.Close()`-d.
- **Races**: No shared mutable state in the package. `migrateAllCountingSteps` is single-goroutine. `EnsureRoles` serialises all DDL over the same pool. No goroutines spawned.
- **Error wrapping**: All errors use `%w` where needed. `redactErrorPassword` correctly handles nil and empty-password inputs. `redactDSN` correctly handles the no-password, no-userinfo, empty, and malformed DSN cases.
- **SQL mishandling**: sqlc-generated code is correct for its queries. The `AggregateCacheMetricsSinceRow` `interface{}` fields are handled defensively in the `cachemetrics` package via `anyInt64`/`anyNumericFloat`.
- **Integer overflow**: `InsertConversationTurnParams` uses `int32` for token counts; the `int32(p.InputTokens)` conversions in `conversations/store.go` truncate silently for values > 2^31, but this is a caller concern, not a defect in the sqlc layer itself.
- **WithTx correctness**: Named-return pattern is correct. Rollback-on-error and commit paths are mutually exclusive. Panic re-panic does not swallow the panic value.
- **Context propagation in EnsureRoles**: `ctx` is threaded through every `pool.Exec` and `pool.QueryRow` call.
- **Migration counting**: `migrateAllCountingSteps` correctly treats both `ErrNoChange` and `os.ErrNotExist` as "no more migrations" (the latter is returned by golang-migrate's iofs source when the sequence is exhausted).
- **DueTasks FOR UPDATE SKIP LOCKED**: Correct pattern for a multi-instance scheduler; holds the row lock only within the query result set, not a full-table lock.
