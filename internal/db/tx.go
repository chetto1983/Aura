package db

import (
	"context"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
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

// WithIdentityTx is the RLS carrier (D-07): it mirrors WithTx but sets the per-request RLS
// session var `app.current_identity` to identityID BEFORE running fn, so the owner-isolation
// policies filter every statement in the tx to the caller's rows. See SetTxIdentity for why
// the setting is transaction-local.
//
// This is the single choke point every owner-scoped read/mutate routes through, so a
// forgotten *ForIdentity WHERE clause still returns 0 foreign rows (the D-07 backstop the
// app-level filter is layered on top of, not a replacement for).
//
// An empty identityID sets the var to the empty string, which FAILS CLOSED on every
// owner-scoped table: migration 0087 covers the document plane, capability grants, content
// parts, the idempotency registry and the per-identity object-store binding; 0089 finished
// the job on aura.conversations, aura.conversation_turns, aura.paused_states and
// aura.shared_links. No row is visible and no write is accepted without a principal, which
// is the point — a caller with no identity must not reach owner-scoped data at all, and is
// never silently UPGRADED to someone else's rows.
//
// aura.shared_links is the one table whose read side is deliberately wider than "mine": an
// unrevoked link is readable with no identity, because a share link is a capability handed
// to someone who is not the owner. Its writes are still owner-only. See migration 0089.
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
	if err = SetTxIdentity(ctx, tx, identityID); err != nil {
		return err
	}
	return fn(sqlc.New(tx))
}

// WithCallerIdentityTx is WithIdentityTx with the identity read from the CONTEXT instead of
// taken as an argument. identityctx.IdentityID(ctx) is the principal every entry point
// already puts there: agui.withPrincipal for a web request, bootCLIChat for the CLI,
// telegram's scopeTurnToIdentity for an inbound message, and runner.scopeContextToConversation
// for the turn loop.
//
// It exists so a store called from all four of those paths does not have to widen every
// method signature to carry an identity the context is already holding —
// conversations.Store and askuser.Store are the two, and between them that would have been
// thirty signatures. Use the explicit WithIdentityTx wherever the identity is a genuine
// INPUT rather than ambient caller state: conversations.Store.Create scopes to the owner it
// is being asked to create, not to whoever is asking.
//
// A context with no principal sets the empty string, which is fail-closed everywhere (see
// WithIdentityTx). That is the intended failure mode, not a degradation to be papered over
// with a default identity: resolve a real owner (identityctx.OperatorIdentity) at the
// composition root instead.
func WithCallerIdentityTx(ctx context.Context, pool *pgxpool.Pool, fn func(*sqlc.Queries) error) error {
	return WithIdentityTx(ctx, pool, identityctx.IdentityID(ctx), fn)
}

// SetTxIdentity binds app.current_identity to identityID for the remainder of tx. It is the
// single statement the whole RLS carrier reduces to, exported so a composition root that has
// ALREADY begun a transaction — and only learns the identity inside it, as
// auraLegAdapter.CreateIdentityWithGrants does when it mints a new identity mid-tx — can opt
// into the same scoping without opening a second transaction or hand-rolling the SQL.
//
// is_local => true scopes the setting to THIS transaction (it resets at COMMIT/ROLLBACK), so it
// is safe under pgxpool connection reuse. A bare SET would persist on the pooled connection and
// leak the identity to the next borrower — the T-36-04-E elevation-of-privilege prohibition.
func SetTxIdentity(ctx context.Context, tx pgx.Tx, identityID string) error {
	_, err := tx.Exec(ctx, `SELECT set_config('app.current_identity', $1, true)`, identityID)
	return err
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
	if err = SetTxIdentity(ctx, tx, identityID); err != nil {
		return err
	}
	return fn(tx)
}
