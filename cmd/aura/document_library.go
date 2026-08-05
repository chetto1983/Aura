package main

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/jackc/pgx/v5/pgxpool"
)

// documentLibrary backs every host retrieval surface. It combines identity-scoped
// PostgreSQL card routing with revalidated ArcadeDB passage candidates.
type documentLibrary struct {
	pool         *pgxpool.Pool
	retriever    *documents.HostRetriever
	retrievalErr error
}

func newDocumentLibrary(pool *pgxpool.Pool, configs ...*config.Config) *documentLibrary {
	cfg := config.LoadDB()
	if len(configs) > 0 && configs[0] != nil {
		cfg = configs[0]
	}
	retriever, err := newHostDocumentRetriever(cfg, pool)
	return &documentLibrary{pool: pool, retriever: retriever, retrievalErr: err}
}

func (l *documentLibrary) Retrieve(
	ctx context.Context,
	request documents.RetrievalRequest,
) (documents.RetrievalResponse, error) {
	if l == nil || l.pool == nil || l.retriever == nil {
		if l != nil && l.retrievalErr != nil {
			return documents.RetrievalResponse{}, fmt.Errorf(
				"document library is not configured: %w", l.retrievalErr,
			)
		}
		return documents.RetrievalResponse{}, fmt.Errorf("document library is not configured")
	}
	return l.retriever.Retrieve(ctx, request)
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
