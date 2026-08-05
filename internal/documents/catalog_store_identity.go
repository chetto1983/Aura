// The identity leg of the catalog store: the transaction seam every statement that
// touches aura.documents runs inside, and the search-id namespace resolvers that were the
// last two entry points reaching the catalog without naming a principal.
//
// Split out of catalog_store.go on 2026-08-03, when migration 0087's fail-closed floor
// made the seam necessary and the conversion pushed that file past the 600-LOC cap
// (CLAUDE.md, no god class).

package documents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// catalogTx is the pair of handles ONE identity-scoped transaction hands out.
//
// The catalog store speaks both dialects — sqlc for the generated statements, raw DBTX for
// the three queries sqlc does not generate — and the two must be the SAME transaction or a
// statement runs outside the scope of the one it belongs to: the batched version-size read
// would miss the rows the list it decorates has just returned, and the asset soft-delete
// would land outside the document soft-delete it accompanies.
type catalogTx struct {
	q   *sqlc.Queries
	raw sqlc.DBTX
}

// scoped runs fn with app.current_identity bound to identityID, so the fail-closed floor
// migration 0087 put on aura.documents (and its version, tag, chunk and embedding
// children) admits exactly that principal's rows. Without it every catalog INSERT fails
// 42501: the floor is AS RESTRICTIVE, so an unset session variable refuses the write
// outright and hides every read.
//
// It is db.WithIdentityTxRaw rather than db.WithIdentityTx because this store needs the raw
// pgx.Tx as well as a *sqlc.Queries over it. WithIdentityTx yields only the latter, and
// sqlc.Queries keeps its DBTX unexported, so from inside that callback there is no way back
// to the transaction the hand-written queries would have to share.
//
// A pool-less store is the injected-DBTX construction the package's unit tests use
// (&PostgresCatalogStore{db: fake}): it has no transaction to open, so it runs fn on the
// handles it was given. Production always goes through NewPostgresCatalogStore with a pool.
func (s *PostgresCatalogStore) scoped(ctx context.Context, identityID string, fn func(catalogTx) error) error {
	if s.pool == nil {
		return fn(catalogTx{q: s.q, raw: s.db})
	}
	return db.WithIdentityTxRaw(ctx, s.pool, identityID, func(tx pgx.Tx) error {
		return fn(catalogTx{q: sqlc.New(tx), raw: tx})
	})
}

// scopedValue is scoped for the methods that return a value as well as an error. It exists
// because Go has no type parameters on methods, and because a caller must never receive a
// half-built row from a transaction that then rolled back: on failure it yields the zero
// value, never whatever fn had assigned before it failed.
func scopedValue[T any](
	ctx context.Context,
	s *PostgresCatalogStore,
	identityID string,
	fn func(catalogTx) (T, error),
) (T, error) {
	var out T
	err := s.scoped(ctx, identityID, func(sc catalogTx) error {
		var fnErr error
		out, fnErr = fn(sc)
		return fnErr
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

// ErrDocumentNotCatalogued reports that a search document has no row in aura.documents.
// It is the normal state of a CLI-ingested document: the catalog is written only by the
// asset upload chain, so `aura docs ingest` produces graph chunks and a job ledger row and
// nothing the cockpit's document list can see.
var ErrDocumentNotCatalogued = errors.New("documents: search document is not in the catalog")

// SetSearchDocumentStatus corrects one catalogued document's status without touching
// anything else. UpdateDocument cannot serve this — it rewrites every column, so a narrow
// correction would have to invent scope, title and tags to make it.
//
// identityID is the OWNER of the document being corrected, not whoever noticed the
// failure. It became an argument when migration 0087 made aura.documents fail-closed: the
// write needs a principal, and the one caller — documents.Service.markCatalogFailed, under
// IngestPath — is already holding req.IdentityID. Enumerating identities to find the row
// would be the wrong shape: correcting one document is an owner-scoped correction, not the
// cross-tenant sweep that conversations.ScanOrphans is.
//
// The second argument is the SEARCH document id (doc_<hex>, derived from file content),
// which is the only id an embedding worker holds. It is a different namespace from the
// catalog's uuid primary key, and passing one where the other was expected is how the "a
// dead embedding never stays ready" guarantee silently never fired: uuid.Parse rejected the
// string, the worker swallowed the error into a WARN, and the catalog kept saying ready.
// The mapping is the typed, immutable owner-scoped search_document_id column.
func (s *PostgresCatalogStore) SetSearchDocumentStatus(
	ctx context.Context,
	identityID, searchDocumentID string,
	status DocumentStatus,
	reason string,
) error {
	return s.scoped(ctx, identityID, func(sc catalogTx) error {
		pgDocumentID, err := sc.catalogIDForSearchDocument(ctx, identityID, searchDocumentID)
		if err != nil {
			return err
		}
		pgIdentityID, err := pgUUID("identity_id", identityID)
		if err != nil {
			return err
		}
		if _, err := sc.q.SetDocumentStatus(ctx, sqlc.SetDocumentStatusParams{
			ID: pgDocumentID, IdentityID: pgIdentityID, Status: string(status),
			ErrorCode: reason, ErrorMessage: reason,
		}); err != nil {
			return fmt.Errorf("set document status: %w", err)
		}
		return nil
	})
}

// CatalogIDForSearchDocument resolves the catalog uuid behind a `doc_<hex>` search id.
// Exported for OpenService, which must reach the original asset starting from the only id
// a retrieval hit carries.
//
// It takes the identity for the same reason SetSearchDocumentStatus does, and its caller
// (OpenService.catalogID, under OpenDocument) likewise already holds one. The resolution is
// therefore owner-scoped in its own right now: a search id belonging to someone else
// resolves to ErrDocumentNotCatalogued here rather than resolving and being refused one
// gate later by GetDocument.
func (s *PostgresCatalogStore) CatalogIDForSearchDocument(
	ctx context.Context,
	identityID, searchDocumentID string,
) (string, error) {
	return scopedValue(ctx, s, identityID, func(sc catalogTx) (string, error) {
		id, err := sc.catalogIDForSearchDocument(ctx, identityID, searchDocumentID)
		if err != nil {
			return "", err
		}
		return uuidString(id), nil
	})
}

// catalogIDForSearchDocument resolves the unique live owner-scoped typed search id.
func (sc catalogTx) catalogIDForSearchDocument(ctx context.Context, identityID, searchDocumentID string) (pgtype.UUID, error) {
	if strings.TrimSpace(searchDocumentID) == "" {
		return pgtype.UUID{}, fmt.Errorf("search_document_id is empty")
	}
	owner, err := pgUUID("identity_id", identityID)
	if err != nil {
		return pgtype.UUID{}, err
	}
	row, err := sc.q.GetDocumentBySearchID(ctx, sqlc.GetDocumentBySearchIDParams{
		IdentityID: owner, SearchDocumentID: searchDocumentID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, fmt.Errorf("%w: %s", ErrDocumentNotCatalogued, searchDocumentID)
	}
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("resolve catalog document for %s: %w", searchDocumentID, err)
	}
	return row.ID, nil
}
