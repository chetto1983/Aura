package main

import (
	"fmt"
	"log/slog"
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
	// Losing it is not fatal — the cascade degrades to document cards — but it must never be
	// silent: measured 2026-09-06, a discarded error here left every document_search answering
	// "arcadedb_unavailable" with an empty result and nothing in the log to say why.
	if strings.TrimSpace(cfg.ArcadeDB.BaseURL) == "" {
		slog.Info("documents: no ArcadeDB base URL — retrieval will answer from document cards only")
		return retriever, nil
	}
	index, err := newRuntimeDocumentIndex(cfg, nil, true)
	if err != nil {
		slog.Warn("documents: passage index unavailable — retrieval will answer from document cards only",
			"err", err)
		return retriever, nil
	}
	retriever.ControlPlane = &documents.ArcadeRetrievalControlPlane{Index: index}
	retriever.PassageIndex = index
	return retriever, nil
}
