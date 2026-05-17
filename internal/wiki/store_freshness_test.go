package wiki

import (
	"context"
	"fmt"
	"testing"

	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/storage/freshness"
	"github.com/aura/aura/internal/testutil"
)

func TestWritePage_BumpsFreshnessPending(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	ctx := context.Background()

	fs := freshness.NewStore(db)
	_, err := fs.Upsert(ctx, freshness.Row{
		ProjectionID:  "compact_memory_wiki",
		Kind:          "sqlite",
		Status:        "fresh",
		SchemaVersion: 1,
		Version:       1,
	})
	if err != nil {
		t.Fatalf("seed projection row: %v", err)
	}

	s := newWritesTestStore(t)
	s.SetFreshnessStore(fs)

	writeFixturePage(t, s, "Test Article", "body content here", "2026-01-01T00:00:00Z")

	row, found, err := fs.Get(ctx, "compact_memory_wiki")
	if err != nil || !found {
		t.Fatalf("Get projection: err=%v found=%v", err, found)
	}
	fmt.Printf("compact_memory_wiki: PendingCount=%d Status=%s\n", row.PendingCount, row.Status)
	if row.PendingCount != 1 {
		t.Errorf("PendingCount = %d, want 1", row.PendingCount)
	}
}

func TestWritePage_MultipleBumpsAccumulate(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	ctx := context.Background()

	fs := freshness.NewStore(db)
	_, err := fs.Upsert(ctx, freshness.Row{
		ProjectionID:  "compact_memory_wiki",
		Kind:          "sqlite",
		Status:        "fresh",
		SchemaVersion: 1,
		Version:       1,
	})
	if err != nil {
		t.Fatalf("seed projection row: %v", err)
	}

	s := newWritesTestStore(t)
	s.SetFreshnessStore(fs)

	writeFixturePage(t, s, "Page One", "first page", "2026-01-01T00:00:00Z")
	writeFixturePage(t, s, "Page Two", "second page", "2026-01-01T00:00:00Z")
	writeFixturePage(t, s, "Page Three", "third page", "2026-01-01T00:00:00Z")

	row, found, _ := fs.Get(ctx, "compact_memory_wiki")
	if !found {
		t.Fatal("projection row not found")
	}
	fmt.Printf("compact_memory_wiki after 3 writes: PendingCount=%d\n", row.PendingCount)
	if row.PendingCount != 3 {
		t.Errorf("PendingCount = %d, want 3", row.PendingCount)
	}
}

func TestWritePage_FreshnessNilSafe(t *testing.T) {
	s := newWritesTestStore(t)
	// No SetFreshnessStore — nil-safe path.
	writeFixturePage(t, s, "Nil Safe Page", "body", "2026-01-01T00:00:00Z")
}
