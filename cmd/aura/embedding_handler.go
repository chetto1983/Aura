package main

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/jackc/pgx/v5/pgxpool"
)

type runtimeDocumentEmbeddingHandler struct {
	cfg  *config.Config
	pool *pgxpool.Pool
}

func (h runtimeDocumentEmbeddingHandler) HandleIngestionJob(ctx context.Context, job documents.IngestionJob) error {
	if h.cfg == nil || h.pool == nil {
		return fmt.Errorf("document embedding handler is not configured")
	}
	mcp, err := knowledge.Open(ctx, &h.cfg.Neo4j)
	if err != nil {
		return err
	}
	defer func() { _ = mcp.Close() }()
	model, version := runtimeEmbeddingGraphMetadata(h.cfg)
	handler := &documents.EmbeddingJobHandler{
		Worker: &documents.EmbeddingWorker{
			Jobs:      documents.NewPostgresJobStore(h.pool),
			Generator: embeddingClient(h.cfg, documentHTTPClient(h.cfg)),
			Indexer: &documents.Indexer{
				Client:           mcp,
				EmbeddingModel:   model,
				EmbeddingVersion: version,
			},
			// Without this the document keeps the "ready" it was given the moment its bytes
			// landed, even when every embedding attempt failed — which is how a 118-chunk
			// spreadsheet with zero vectors still advertised itself as searchable.
			Catalog: documents.NewPostgresCatalogStore(h.pool),
		},
	}
	return handler.HandleIngestionJob(ctx, job)
}

func runtimeEmbeddingGraphMetadata(cfg *config.Config) (model, version string) {
	if cfg == nil {
		return "default", "v1"
	}
	_, _, model = cfg.EmbedRoute()
	if model == "" {
		model = "local-sidecar"
	}
	version = fmt.Sprintf("dim:%d", cfg.Neo4j.EmbedDimensions)
	return model, version
}
