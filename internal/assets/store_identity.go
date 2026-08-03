// The identity leg of the asset store: the transaction seam every statement that touches
// aura.assets runs inside, and the three folds that keep ten wrapped methods from becoming
// ten copies of the same six lines.
//
// Split out of store.go on 2026-08-03, when migration 0090's fail-closed floor made the
// seam necessary. It lives beside store.go rather than inside it for the reason
// internal/documents/catalog_store_identity.go gives: the RLS reasoning is a concern of its
// own, and a reader asking "how is this store scoped?" should not have to read ten CRUD
// methods to find out.

package assets

import (
	"context"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// withIdentity runs fn with app.current_identity bound to identityID, so the fail-closed
// floor migration 0090 put on aura.assets and aura.asset_events admits exactly that
// principal's rows. Without it every Create fails 42501: the floor is AS RESTRICTIVE, so an
// unset session variable refuses the INSERT outright and hides every read.
//
// The scope is the identity the CALL carries, never the ambient context. All ten methods
// already took an identityID before this seam existed — the asset belongs to the principal
// it was uploaded for, which on the processing path is not whoever is executing the job.
// This is the idempotency.Store.withIdentity side of the choice db.WithCallerIdentityTx
// documents, not the askuser.Store side.
//
// A pool-less Store is the injected-DBTX construction the package's unit tests use
// (NewStore over a fake sqlc.DBTX in store_unit_test.go): it has no transaction to open, so
// it runs fn on the queries it was built with. Production always goes through NewStore with
// a *pgxpool.Pool — cmd/aura/document_processor_wiring.go is the only call site.
func (s *Store) withIdentity(ctx context.Context, identityID string, fn func(*sqlc.Queries) error) error {
	if s.pool == nil {
		return fn(s.q)
	}
	return db.WithIdentityTx(ctx, s.pool, identityID, fn)
}

// scopedRow runs one statement yielding a single aura.assets row as identityID and decodes
// it. Eight of the ten Store methods are exactly that, and without this fold each would
// carry its own copy of the open-transaction / capture-row / decode dance — enough
// repetition for `dupl` to fire at its 100-token threshold, and enough for one of the ten to
// quietly lose its scope in a later edit.
//
// On failure it yields the zero Asset, never whatever fn assigned before the transaction
// rolled back: a caller must not receive a row from a write that did not commit.
func (s *Store) scopedRow(
	ctx context.Context,
	identityID string,
	fn func(*sqlc.Queries) (sqlc.AuraAssets, error),
) (Asset, error) {
	var row sqlc.AuraAssets
	err := s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var fnErr error
		row, fnErr = fn(q)
		return fnErr
	})
	if err != nil {
		return Asset{}, err
	}
	return assetFromSQL(row)
}

// scopedRows is scopedRow for the two list statements.
func (s *Store) scopedRows(
	ctx context.Context,
	identityID string,
	fn func(*sqlc.Queries) ([]sqlc.AuraAssets, error),
) ([]Asset, error) {
	var rows []sqlc.AuraAssets
	err := s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var fnErr error
		rows, fnErr = fn(q)
		return fnErr
	})
	if err != nil {
		return nil, err
	}
	out := make([]Asset, 0, len(rows))
	for _, row := range rows {
		asset, assetErr := assetFromSQL(row)
		if assetErr != nil {
			return nil, assetErr
		}
		out = append(out, asset)
	}
	return out, nil
}

// scopedTarget is scopedRow for the seven statements addressed by an (asset id, identity id)
// pair: it parses both uuids once and hands them to fn.
//
// Both uuids stay in the statement even though app.current_identity now filters the same
// rows. The policy is the backstop D-07 asks for, not a replacement for the WHERE clause —
// an asset id belonging to another identity must return "no rows", which is what the
// explicit predicate says and what every caller's error handling already expects.
func (s *Store) scopedTarget(
	ctx context.Context,
	id, identityID string,
	fn func(*sqlc.Queries, pgtype.UUID, pgtype.UUID) (sqlc.AuraAssets, error),
) (Asset, error) {
	pgID, err := pgUUID("asset id", id)
	if err != nil {
		return Asset{}, err
	}
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return Asset{}, err
	}
	return s.scopedRow(ctx, identityID, func(q *sqlc.Queries) (sqlc.AuraAssets, error) {
		return fn(q, pgID, pgIdentityID)
	})
}
