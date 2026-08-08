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
			CandidateLimit: cfg.DocumentPipeline.RetrievalCandidates,
			FusionStrategy: arcadedb.FusionStrategy(cfg.DocumentPipeline.FusionStrategy),
		},
	}
	// ONE index serves both roles now. The control plane used to be a PostgreSQL store
	// built from the pool while the projection was ArcadeDB; cards and passages are both
	// ArcadeDB records today, so a single DocumentIndex answers the card leg and the two
	// candidate legs. Without ArcadeDB configured the retriever has NO control plane and
	// Retrieve fails loudly, which is the honest outcome: there is nothing left to search.
	if strings.TrimSpace(cfg.ArcadeDB.BaseURL) == "" {
		return retriever, nil
	}
	index, err := newRuntimeDocumentIndex(cfg, nil, false)
	if err != nil {
		return retriever, nil
	}
	retriever.ControlPlane = &documents.ArcadeRetrievalControlPlane{Index: index}
	retriever.Projection = index
	return retriever, nil
}
