// debug_reconcile is a one-shot CLI tool that runs ReconcileWikiQdrantWithDisk
// against a live Qdrant + wiki directory and prints the result. Useful for
// operator inspection and for verifying orphan cleanup manually.
//
// Usage:
//
//	QDRANT_URL=http://localhost:6333 WIKI_PATH=/path/to/wiki \
//	  go run ./cmd/debug_reconcile [--dry-run]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/aura/aura/internal/storage/qdrant"
	"github.com/aura/aura/internal/storage/search"
)

func main() {
	dryRun := flag.Bool("dry-run", false, "print orphans without deleting them")
	flag.Parse()

	qdrantURL := os.Getenv("QDRANT_URL")
	if qdrantURL == "" {
		qdrantURL = "http://localhost:6333"
	}
	collection := os.Getenv("QDRANT_COLLECTION")
	if collection == "" {
		collection = "aura_memory_v1"
	}
	wikiPath := os.Getenv("WIKI_PATH")
	if wikiPath == "" {
		wikiPath = "./wiki"
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	client, err := qdrant.NewClient(qdrant.Config{
		BaseURL: qdrantURL,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "create qdrant client: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	if *dryRun {
		// Dry-run: scroll and diff without deleting.
		docIDs, err := client.ScrollSlugs(ctx, collection)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scroll slugs: %v\n", err)
			os.Exit(1)
		}
		logger.Info("dry-run: scrolled Qdrant", "doc_id_count", len(docIDs))
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{"doc_ids": docIDs, "count": len(docIDs)})
		return
	}

	rep := search.ReconcileWikiQdrantWithDisk(ctx, client, collection, nil, wikiPath, logger)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	out := map[string]any{
		"orphans_found":   rep.OrphansFound,
		"orphans_deleted": rep.OrphansDeleted,
		"elapsed_ms":      rep.ElapsedMs,
	}
	if rep.Err != nil {
		out["error"] = rep.Err.Error()
		_ = enc.Encode(out)
		fmt.Fprintf(os.Stderr, "reconcile error: %v\n", rep.Err)
		os.Exit(1)
	}
	_ = enc.Encode(out)
}
