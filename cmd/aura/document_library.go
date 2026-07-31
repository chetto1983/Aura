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
//
// It replaced docsToolSearcher, which held a live Neo4j client and ran the
// two-stage retrieval pipeline (dense/sparse seed, cross-encoder rerank, 1-hop
// graph expand) to return passages. Measured: passages answer "what does it
// say" and cannot answer "how many" at any k.
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
