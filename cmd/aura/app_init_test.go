package main

import (
	"context"
	"testing"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/storage/freshness"
)

func TestSeedProjectionStateCreates5Rows(t *testing.T) {
	db, err := auradb.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrations.Run: %v", err)
	}

	fs := freshness.NewStore(db)
	if err := seedProjectionState(context.Background(), fs, "embeddinggemma-300m@256-mrl", 256); err != nil {
		t.Fatalf("seedProjectionState: %v", err)
	}

	rows, err := fs.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("want 5 projection rows, got %d: %v", len(rows), rows)
	}

	wantIDs := map[string]bool{
		"wiki_documents_fts5":      true,
		"aura_memory_v1":           true,
		"compact_memory_documents": true,
		"aura_memory_v1_compact":   true,
		"embedding_cache":          true,
	}
	for _, r := range rows {
		if !wantIDs[r.ProjectionID] {
			t.Errorf("unexpected projection_id %q", r.ProjectionID)
		}
		delete(wantIDs, r.ProjectionID)
	}
	for id := range wantIDs {
		t.Errorf("missing projection_id %q", id)
	}
}

func TestSeedProjectionStateIsIdempotent(t *testing.T) {
	db, err := auradb.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrations.Run: %v", err)
	}

	fs := freshness.NewStore(db)
	// Seed twice; second call should be a no-op.
	for i := 0; i < 2; i++ {
		if err := seedProjectionState(context.Background(), fs, "embeddinggemma-300m@256-mrl", 256); err != nil {
			t.Fatalf("seedProjectionState call %d: %v", i+1, err)
		}
	}

	rows, err := fs.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("idempotent: want 5 rows, got %d", len(rows))
	}
}
