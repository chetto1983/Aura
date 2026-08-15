package main

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/jackc/pgx/v5/pgxpool"
)

// documentLibrary backs every host retrieval surface. Card routing and passage candidates
// both come from ArcadeDB; the pool is still required because a nil one is how "not
// configured" is detected, and document_search must fail loudly rather than be absent.
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
