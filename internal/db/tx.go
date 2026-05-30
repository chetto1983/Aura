package db

import (
	"context"

	"github.com/chetto1983/aura/internal/db/sqlc"
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
