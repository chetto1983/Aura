package main

import (
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newHostDocumentRetriever(cfg *config.Config, pool *pgxpool.Pool) (*documents.HostRetriever, error) {
	if cfg == nil || pool == nil {
		return nil, fmt.Errorf("document retriever requires configuration and database pool")
	}
	retriever := &documents.HostRetriever{
		Embedder: embeddingClient(cfg, documentHTTPClient(cfg)),
		Config: documents.RetrievalConfig{
			CandidateLimit: cfg.DocumentRetrieval.RetrievalCandidates,
			FusionStrategy: arcadedb.FusionStrategy(cfg.DocumentRetrieval.FusionStrategy),
		},
	}
	// One per-identity ArcadeDB index supplies document metadata and fused passage candidates.
	if strings.TrimSpace(cfg.ArcadeDB.BaseURL) == "" {
		return retriever, nil
	}
	index, err := newRuntimeDocumentIndex(cfg, nil, true)
	if err != nil {
		return retriever, nil
	}
	retriever.ControlPlane = &documents.ArcadeRetrievalControlPlane{Index: index}
	retriever.PassageIndex = index
	return retriever, nil
}
