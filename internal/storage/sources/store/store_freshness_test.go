package source

import (
	"context"
	"fmt"
	"testing"

	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/storage/freshness"
	"github.com/aura/aura/internal/testutil"
)

func TestStorePut_BumpsFreshnessPending(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	ctx := context.Background()

	fs := freshness.NewStore(db)
	_, err := fs.Upsert(ctx, freshness.Row{
		ProjectionID:  "compact_memory_sources",
		Kind:          "sqlite",
		Status:        "fresh",
		SchemaVersion: 1,
		Version:       1,
	})
	if err != nil {
		t.Fatalf("seed projection row: %v", err)
	}

	s, err := NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	s.SetFreshnessStore(fs)

	_, dup, err := s.Put(ctx, PutInput{
		Kind:     KindPDF,
		Filename: "doc.pdf",
		MimeType: "application/pdf",
		Bytes:    []byte("%PDF-1.4 freshness test"),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if dup {
		t.Fatal("expected new source, got duplicate")
	}

	row, found, err := fs.Get(ctx, "compact_memory_sources")
	if err != nil || !found {
		t.Fatalf("Get projection: err=%v found=%v", err, found)
	}
	fmt.Printf("compact_memory_sources: PendingCount=%d Status=%s\n", row.PendingCount, row.Status)
	if row.PendingCount != 1 {
		t.Errorf("PendingCount = %d, want 1", row.PendingCount)
	}
}

func TestStorePut_DupDoesNotBumpFreshness(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	ctx := context.Background()

	fs := freshness.NewStore(db)
	_, err := fs.Upsert(ctx, freshness.Row{
		ProjectionID:  "compact_memory_sources",
		Kind:          "sqlite",
		Status:        "fresh",
		SchemaVersion: 1,
		Version:       1,
	})
	if err != nil {
		t.Fatalf("seed projection row: %v", err)
	}

	s, err := NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	s.SetFreshnessStore(fs)

	in := PutInput{
		Kind:     KindPDF,
		Filename: "doc.pdf",
		MimeType: "application/pdf",
		Bytes:    []byte("%PDF-1.4 dup test"),
	}
	// First Put: new source → bump.
	if _, _, err := s.Put(ctx, in); err != nil {
		t.Fatalf("first Put: %v", err)
	}
	// Second Put with same bytes: duplicate → no bump.
	_, dup, err := s.Put(ctx, in)
	if err != nil {
		t.Fatalf("second Put: %v", err)
	}
	if !dup {
		t.Fatal("expected duplicate on second Put")
	}

	row, found, _ := fs.Get(ctx, "compact_memory_sources")
	if !found {
		t.Fatal("projection row not found")
	}
	fmt.Printf("compact_memory_sources after dup: PendingCount=%d\n", row.PendingCount)
	if row.PendingCount != 1 {
		t.Errorf("PendingCount after dup = %d, want 1 (dup must not bump)", row.PendingCount)
	}
}

func TestStorePut_FreshnessNilSafe(t *testing.T) {
	s, err := NewStore(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	// No SetFreshnessStore — nil-safe path.
	_, _, err = s.Put(context.Background(), PutInput{
		Kind:     KindText,
		Filename: "note.txt",
		MimeType: "text/plain",
		Bytes:    []byte("nil-safe check"),
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
}
