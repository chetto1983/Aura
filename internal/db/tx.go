package db

import (
	"context"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WithTx runs fn inside a single Postgres transaction and is the one DRY seam
// every multi-statement write reuses (D-A2-03): conversations.Store.AppendTurn
// (atomic INSERT turn + UPDATE aggregates, SC-2), askuser.Store.MarkResumedBatch,
// and Runner.Stop auto-resolve. It Begins a tx, hands fn a *sqlc.Queries bound to
// that tx (pgx.Tx satisfies sqlc.DBTX), and on return:
//
//   - panic in fn        -> Rollback, then re-panic (the panic is not swallowed)
//   - fn returns non-nil -> Rollback, return fn's error
//   - fn returns nil     -> Commit (a Commit failure surfaces as the returned err)
//
// The named return `err` is read by the deferred closure, so a Commit error
// replaces a nil fn result.
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(*sqlc.Queries) error) (err error) {
	ctx, end := dbTransactionBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return
		}
		err = tx.Commit(ctx)
	}()
	return fn(sqlc.New(tx))
}

// WithIdentityTx is the Phase-36 RLS carrier (D-07): it mirrors WithTx but sets the
// per-request RLS session var `app.current_identity` to identityID BEFORE running fn,
// so the owner-isolation policies on aura.conversations / aura.conversation_turns /
// aura.paused_states (migration 0032) filter every statement in the tx to the caller's
// rows. `set_config(..., is_local => true)` scopes the setting to THIS transaction only
// (it resets at COMMIT/ROLLBACK), so it is safe under pgxpool connection reuse — a bare
// `SET` would leak the identity onto the pooled connection and the next borrower (the
// T-36-04-E elevation-of-privilege prohibition). An empty identityID sets the var to ”
// (which the NULLIF-guarded policy treats as "no identity context" → the legacy
// unscoped path), so a caller with no principal is never silently upgraded.
//
// This is the single choke point every owner-scoped web read/mutate routes through, so a
// forgotten *ForIdentity WHERE clause still returns 0 foreign rows (the D-07 backstop the
// app-level filter is layered on top of, not a replacement for).
func WithIdentityTx(ctx context.Context, pool *pgxpool.Pool, identityID string, fn func(*sqlc.Queries) error) (err error) {
	ctx, end := dbTransactionBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return
		}
		err = tx.Commit(ctx)
	}()
	// is_local => true scopes the setting to this tx (resets at COMMIT/ROLLBACK) — never
	// a bare SET, which would persist on the pooled connection and leak to the next borrower.
	if _, err = tx.Exec(ctx, `SELECT set_config('app.current_identity', $1, true)`, identityID); err != nil {
		return err
	}
	return fn(sqlc.New(tx))
}

// WithIdentityTxRaw is WithIdentityTx's raw-pgx sibling (Phase 37F): identical transaction
// lifecycle and identical `SET LOCAL app.current_identity` RLS carrier, but hands fn the raw
// pgx.Tx instead of a *sqlc.Queries. It exists for owner-scoped stores that are raw pgx by
// design (the PgAuditStore precedent, internal/agui/audit_store.go) and therefore have no
// sqlc.Queries wrapping their tx: sqlc.Queries.db is an unexported field with no accessor, so
// there is no way to reach a raw pgx.Tx through WithIdentityTx's *sqlc.Queries callback without
// adding a sqlc-generated query — which a raw-pgx store deliberately does not carry (see
// internal/share/store.go). This function and WithIdentityTx MUST NOT drift apart on
// commit/rollback/panic semantics; both are the DRY seam for their respective query shapes.
func WithIdentityTxRaw(ctx context.Context, pool *pgxpool.Pool, identityID string, fn func(pgx.Tx) error) (err error) {
	ctx, end := dbTransactionBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return
		}
		err = tx.Commit(ctx)
	}()
	if _, err = tx.Exec(ctx, `SELECT set_config('app.current_identity', $1, true)`, identityID); err != nil {
		return err
	}
	return fn(tx)
}
