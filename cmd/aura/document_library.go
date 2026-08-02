package main

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/jackc/pgx/v5/pgxpool"
)

// documentLibrary backs the document_search tool. It ranks one identity's
// documents by what each one IS — title, operator tags, and the description of
// the file — which is the only question the index still answers now that
// document_open hands over the file itself.
type documentLibrary struct {
	pool *pgxpool.Pool
}

func newDocumentLibrary(pool *pgxpool.Pool) *documentLibrary {
	return &documentLibrary{pool: pool}
}

func (l *documentLibrary) SearchDigests(
	ctx context.Context,
	identityID, query string,
	limit int,
) ([]documents.DigestHit, error) {
	if l == nil || l.pool == nil {
		return nil, fmt.Errorf("document library is not configured: no database pool")
	}
	return documents.NewPostgresCatalogStore(l.pool).SearchDigests(ctx, identityID, query, limit)
}

// SetDigest backs document_describe: the agent writes what it saw after opening
// a file, and that becomes what the library ranks on. Identity-scoped in SQL.
func (l *documentLibrary) SetDigest(
	ctx context.Context,
	identityID, documentID, description string,
) error {
	if l == nil || l.pool == nil {
		return fmt.Errorf("document library is not configured: no database pool")
	}
	return documents.NewPostgresCatalogStore(l.pool).
		SetDigest(ctx, identityID, documentID, description)
}
