package main

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/knowledge"
)

type runtimeDocumentGraphDeactivator struct {
	cfg *config.Config
}

func (d runtimeDocumentGraphDeactivator) DeactivateDocument(ctx context.Context, documentID string) error {
	if d.cfg == nil {
		return fmt.Errorf("document graph deactivator is not configured")
	}
	mcp, err := knowledge.Open(ctx, &d.cfg.Neo4j)
	if err != nil {
		return err
	}
	defer func() { _ = mcp.Close() }()
	return (&documents.Indexer{Client: mcp}).DeactivateDocument(ctx, documentID)
}
