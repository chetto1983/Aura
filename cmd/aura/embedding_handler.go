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
	handler := &documents.EmbeddingJobHandler{
		Worker: &documents.EmbeddingWorker{
			Jobs:      documents.NewPostgresJobStore(h.pool),
			Generator: embeddingClient(h.cfg, documentHTTPClient(h.cfg)),
			Indexer:   &documents.Indexer{Client: mcp},
		},
	}
	return handler.HandleIngestionJob(ctx, job)
}
