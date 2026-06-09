# Audit: internal/askuser

**Verdict:** needs-work — one silent correctness bug in `MarkResumedBatch` (already-resumed token passes without error), one swallowed unmarshal error in `ListRecent`.

**Counts:** critical 0 / high 1 / medium 1 / low 0

---

## Findings

### [HIGH][BUG] `MarkResumedBatch` silently accepts already-resumed tokens

**Location:** `internal/askuser/store.go:267–283`

**Confidence:** high

**Detail:**

The batch re-check that is supposed to catch non-existent or already-resumed tokens is logically broken for the already-resumed case. The sequence inside the transaction is:

1. `q.MarkPausedStateResumed` runs `UPDATE … WHERE token = $1 AND resumed_at IS NULL`. If the token was already resumed before this batch call, the UPDATE touches 0 rows (the WHERE guard rejects it) and returns no error (it is `:exec`).
2. `q.GetPausedStateByToken` fetches the row. Because `GetPausedStateByToken` has **no** `resumed_at IS NULL` filter (SQL: `WHERE token = $1`), it returns the already-resumed row successfully.
3. The guard `if !row.ResumedAt.Valid` is FALSE because `ResumedAt.Valid` is TRUE on an already-resumed row.
4. No error is returned. The already-resumed token is silently treated as a successful batch member and the transaction commits.

This contradicts the documented contract ("if any token is unknown/already-resumed the whole batch rolls back with ErrPauseNotFound") and the analogous `MarkResumed` single-token behaviour (which correctly catches already-resumed via `RowsAffected() == 0` from `pool.Exec`). The test suite covers the *unknown UUID* case (`TestMarkResumedBatch_UnknownTokenRollsBack`) but has no test for an *already-resumed* token passed to `MarkResumedBatch`.

**Suggested fix:**

Switch from the misleading post-update re-read approach to the same `RowsAffected` pattern that `MarkResumed` uses, but inside the transaction via a raw `pgx.Tx.Exec`:

```go
// inside db.WithTx callback, using the underlying pgx.Tx:
tag, err := tx.Exec(ctx, markResumedSQL, id, answer)
if err != nil {
    return fmt.Errorf("mark resumed batch %s: %w", token, err)
}
if tag.RowsAffected() == 0 {
    return fmt.Errorf("mark resumed batch %s: %w", token, ErrPauseNotFound)
}
```

Because `db.WithTx` only exposes a `*sqlc.Queries` (not the raw `pgx.Tx`), the cleanest fix is to add an overloaded `markResumedSQL` path via `WithTxRaw` that passes the `pgx.Tx` directly, or to replace `GetPausedStateByToken` with a targeted query `GetPendingByToken` (adding `AND resumed_at IS NULL`) so that an already-resumed row yields `pgx.ErrNoRows`, matching the intended semantics.

---

### [MEDIUM][BUG] `ListRecent`: `json.Unmarshal` error on `resumed_answer` is silently swallowed

**Location:** `internal/askuser/store.go:212`

**Confidence:** high

**Detail:**

```go
if json.Unmarshal(r.ResumedAnswer, &ans) == nil {
    rec.ResumedAnswer = ans.Content
}
```

If the persisted `resumed_answer` column contains unexpected or corrupted JSON (e.g., written by a future schema change, a migration rollback, or a bug that bypassed `encodeAnswer`), the unmarshal silently fails and `rec.ResumedAnswer` is left as an empty string. The caller and the operator receive no indication that the data is corrupt. There is no logging anywhere in the package.

This is intentional data hiding rather than a load-path crash, which keeps the list command operational, but it masks data integrity issues with no observable signal.

**Suggested fix:**

Log a warning (via `slog.Warn`) on unmarshal failure — or at minimum populate `rec.ResumedAnswer` with a sentinel like `"<malformed>"` so the operator-facing list output does not silently lie. If a logger is available in the `Store` struct, use it; otherwise add `slog` as the canonical structured logger already used elsewhere in the project.

---

## What was checked and found clean

- **Nil-pointer dereference**: `New(nil)` is intentionally used in unit tests to exercise parse/encode guards that short-circuit before any pool call. All production call sites pass a real pool. No nil-deref risk.
- **Unchecked errors**: All DB-layer errors are checked and wrapped. `_ = tx.Rollback` is the canonical pgx pattern (rollback on a failed tx is best-effort). `_ = w.Flush()` in `paused_states.go` (outside scope but adjacent) is low-risk tabwriter.
- **Context propagation**: Every method accepts and forwards `ctx` to the underlying pgx/sqlc call.
- **Resource leaks**: `ListPendingPausedStates` and `ListRecentPausedStates` (generated) close `rows` via `defer rows.Close()`. No raw SQL is executed in a way that leaks a cursor.
- **Goroutine leaks**: The package spawns no goroutines. The goleak TestMain on the integration tier catches any pgx pool leaks.
- **Integer overflow**: `int32(limit)` in `ListRecent` (line 196) could overflow if a caller passes a value > 2147483647, but the only production caller passes 50. The guard `limit <= 0 → 50` does not clamp the upper bound; acceptable given the operator-facing scope.
- **Race conditions**: No shared mutable state. `Store` is immutable after construction.
- **Dead code**: `parseUUID`, `encodeAnswer`, `fromRow`, and `autoTerminatedContent` are all referenced internally. `Record`, `ResumeAnswer`, `InsertParams`, `Pending`, `ErrPauseNotFound`, `ErrInvalidAnswer`, `ActionAccept/Decline/Cancel` are all referenced from multiple production callers outside the package. No dead exports.
- **Not-wired code**: `ListRecent` is wired to `cmd/aura/paused_states.go:59`. `CleanupResumedOlderThan` is wired to `cmd/aura/paused_states.go:98`. All Store methods are referenced via the `PauseStore` interface (`internal/runner/interfaces.go`) and consumed by the runner and the CLI. No orphaned code paths found.
- **markResumedSQL constant**: Correctly mirrors the sqlc-generated query with the `AND resumed_at IS NULL` idempotency guard. The single-token `MarkResumed` correctly detects 0 rows affected.
