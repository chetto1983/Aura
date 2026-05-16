package freshness_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/storage/freshness"
	"github.com/aura/aura/internal/testutil"
)

func newStore(t *testing.T) *freshness.Store {
	t.Helper()
	db := testutil.OpenTestDB(t, migrations.Run)
	return freshness.NewStore(db)
}

func baseRow(id string) freshness.Row {
	return freshness.Row{
		ProjectionID:      id,
		Kind:              "qdrant",
		EmbeddingModelID:  "embeddinggemma-300m@256-mrl",
		EmbeddingDim:      256,
		IndexBuildID:      "build-001",
		SchemaVersion:     1,
		LastFullRebuildAt: 1000000,
		PendingCount:      0,
		CompletedCount:    0,
		FailedCount:       0,
		Status:            "fresh",
		HealthReason:      "",
		Version:           1,
	}
}

func TestUpsertGetRoundtrip(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	in := baseRow("aura_memory_v1")
	out, err := s.Upsert(ctx, in)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if out.UpdatedAt == 0 {
		t.Fatal("Upsert: UpdatedAt not set")
	}

	got, found, err := s.Get(ctx, "aura_memory_v1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !found {
		t.Fatal("Get: row not found after Upsert")
	}
	if got.ProjectionID != "aura_memory_v1" {
		t.Fatalf("ProjectionID = %q, want aura_memory_v1", got.ProjectionID)
	}
	if got.Kind != "qdrant" {
		t.Fatalf("Kind = %q, want qdrant", got.Kind)
	}
	if got.EmbeddingModelID != "embeddinggemma-300m@256-mrl" {
		t.Fatalf("EmbeddingModelID = %q", got.EmbeddingModelID)
	}
	if got.EmbeddingDim != 256 {
		t.Fatalf("EmbeddingDim = %d, want 256", got.EmbeddingDim)
	}
	if got.Status != "fresh" {
		t.Fatalf("Status = %q, want fresh", got.Status)
	}
	if got.Version != 1 {
		t.Fatalf("Version = %d, want 1", got.Version)
	}
	if got.UpdatedAt == 0 {
		t.Fatal("UpdatedAt not persisted")
	}
}

func TestUpdateWithVersionHappyPathIncrementsVersion(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	in := baseRow("compact_memory_documents")
	if _, err := s.Upsert(ctx, in); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	updated, err := s.UpdateWithVersion(ctx, "compact_memory_documents", func(r *freshness.Row) error {
		r.Status = "rebuilding"
		r.PendingCount = 5
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateWithVersion: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("Version = %d, want 2", updated.Version)
	}
	if updated.Status != "rebuilding" {
		t.Fatalf("Status = %q, want rebuilding", updated.Status)
	}
	if updated.PendingCount != 5 {
		t.Fatalf("PendingCount = %d, want 5", updated.PendingCount)
	}
	if updated.UpdatedAt == 0 {
		t.Fatal("UpdatedAt not set")
	}

	got, _, _ := s.Get(ctx, "compact_memory_documents")
	if got.Version != 2 {
		t.Fatalf("persisted Version = %d, want 2", got.Version)
	}
}

func TestUpdateWithVersionConflictReturnsError(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	in := baseRow("embedding_cache")
	if _, err := s.Upsert(ctx, in); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Deterministic conflict: goroutine 1 blocks inside its mutator after reading
	// version=1; main thread runs a second UpdateWithVersion that bumps version to 2;
	// goroutine 1 then resumes and its UPDATE WHERE version=1 finds 0 rows → ErrVersionConflict.
	rowRead := make(chan struct{})   // goroutine signals it has read the row
	unblock := make(chan struct{})   // main signals goroutine to continue after the conflict is set up

	type result struct {
		row freshness.Row
		err error
	}
	ch := make(chan result, 1)
	go func() {
		r, e := s.UpdateWithVersion(ctx, "embedding_cache", func(row *freshness.Row) error {
			close(rowRead) // signal: row has been read (version=1 captured)
			<-unblock      // wait until the concurrent write has bumped version to 2
			row.Status = "degraded"
			return nil
		})
		ch <- result{r, e}
	}()

	<-rowRead // wait for goroutine to read the row at version=1

	// Concurrent write: bump version 1→2 before goroutine finishes.
	if _, err := s.UpdateWithVersion(ctx, "embedding_cache", func(r *freshness.Row) error {
		r.Status = "rebuilding"
		return nil
	}); err != nil {
		close(unblock)
		t.Fatalf("concurrent UpdateWithVersion: %v", err)
	}

	close(unblock) // let goroutine proceed; its WHERE version=1 will now find 0 rows

	got := <-ch
	if !errors.Is(got.err, freshness.ErrVersionConflict) {
		t.Fatalf("want ErrVersionConflict, got %v (row=%+v)", got.err, got.row)
	}
}

func TestListReturnsAllRows(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)

	ids := []string{"wiki_documents_fts5", "aura_memory_v1", "compact_memory_documents"}
	for _, id := range ids {
		r := baseRow(id)
		r.Kind = "fts5"
		if id == "aura_memory_v1" || id == "compact_memory_documents" {
			r.Kind = "qdrant"
		}
		if _, err := s.Upsert(ctx, r); err != nil {
			t.Fatalf("Upsert %q: %v", id, err)
		}
	}

	rows, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("List returned %d rows, want 3", len(rows))
	}
	seen := make(map[string]bool)
	for _, r := range rows {
		seen[r.ProjectionID] = true
	}
	for _, id := range ids {
		if !seen[id] {
			t.Fatalf("List missing row %q", id)
		}
	}
}
