package main

import (
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
)

// buildFileNamer backs the real names in the file manager's listing.
//
// One index, built here and held, exactly as the retriever's wiring does: the listing runs
// on every folder the operator opens, so rebuilding a tenant client per request would pay
// that cost for a lookup whose whole point is to be cheaper than a HEAD per row.
//
// Returns nil when ArcadeDB is not configured, and nil is a supported wiring: the file
// manager then labels rows with the tail of the key, which is what it did before names
// existed. A dead file browser would be the worse failure.
//
// Supported, but never silent: this is the same shape as newHostDocumentRetriever's own
// index wiring, and there the discarded error was what made a broken document_search
// unexplainable for a whole session (2026-09-06). Degrading is the decision; hiding why is
// not part of it.
func buildFileNamer(cfg *config.Config) *documents.ObjectNames {
	if cfg == nil || strings.TrimSpace(cfg.ArcadeDB.BaseURL) == "" {
		return nil
	}
	index, err := newRuntimeDocumentIndex(cfg, nil, false)
	if err != nil {
		slog.Warn("aura files: no document index — the file manager will label rows by key tail, not by name",
			"err", err)
		return nil
	}
	return &documents.ObjectNames{Index: index}
}
