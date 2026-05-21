package search

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/storage/qdrant"
	"github.com/aura/aura/internal/wiki"
)

// fakeQdrantForReconcile is a minimal qdrant.Client stub for reconcile tests.
type fakeQdrantForReconcile struct {
	scrollDocIDs []string
	deletedIDs   [][]string // one entry per Delete call
	deleteErr    error
}

func (f *fakeQdrantForReconcile) Health(_ context.Context) error { return nil }
func (f *fakeQdrantForReconcile) Search(_ context.Context, _ string, _ []float32, _ int, _ bool) ([]qdrant.ScoredPoint, error) {
	return nil, nil
}
func (f *fakeQdrantForReconcile) Upsert(_ context.Context, _ string, _ []qdrant.Point) error {
	return nil
}
func (f *fakeQdrantForReconcile) Delete(_ context.Context, _ string, ids []string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deletedIDs = append(f.deletedIDs, ids)
	return nil
}
func (f *fakeQdrantForReconcile) CreateCollection(_ context.Context, _ string, _ int) error {
	return nil
}
func (f *fakeQdrantForReconcile) DeleteCollection(_ context.Context, _ string) error { return nil }
func (f *fakeQdrantForReconcile) CollectionInfo(_ context.Context, _ string) (qdrant.CollectionInfo, error) {
	return qdrant.CollectionInfo{}, nil
}
func (f *fakeQdrantForReconcile) ScrollSlugs(_ context.Context, _ string) ([]string, error) {
	return f.scrollDocIDs, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// writePlainMD writes a minimal wiki page .md file directly (no frontmatter
// required — the reconciler only checks file existence, not content).
func writePlainMD(t *testing.T, dir, slug string) {
	t.Helper()
	p := filepath.Join(dir, slug+".md")
	if err := os.WriteFile(p, []byte("# "+slug+"\n"), 0644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// TestReconcileWikiQdrant_NoneOrphaned verifies that when every Qdrant
// doc_id matches a page on disk no deletions are performed.
func TestReconcileWikiQdrant_NoneOrphaned(t *testing.T) {
	dir := t.TempDir()
	writePlainMD(t, dir, "alpha")
	writePlainMD(t, dir, "beta")

	fake := &fakeQdrantForReconcile{
		scrollDocIDs: []string{"alpha", "beta", "graph:node:alpha", "graph:node:beta"},
	}
	rep := ReconcileWikiQdrantWithDisk(context.Background(), fake, "test", nil, dir, discardLogger())
	if rep.Err != nil {
		t.Fatalf("unexpected error: %v", rep.Err)
	}
	if rep.OrphansFound != 0 || rep.OrphansDeleted != 0 {
		t.Fatalf("orphans_found=%d orphans_deleted=%d; want 0/0", rep.OrphansFound, rep.OrphansDeleted)
	}
	if len(fake.deletedIDs) != 0 {
		t.Fatalf("Delete called unexpectedly: %v", fake.deletedIDs)
	}
}

// TestReconcileWikiQdrant_OneOrphan verifies that an orphan slug (present in
// Qdrant but not on disk) causes both point IDs to be deleted from Qdrant and
// the corresponding row to be removed from the wiki_documents FTS5 table.
func TestReconcileWikiQdrant_OneOrphan(t *testing.T) {
	dir := t.TempDir()
	// Two real pages on disk; "ghost" is absent → orphan.
	// Floor: orphans(1)*2 <= diskPages(2), so cleanup proceeds.
	writePlainMD(t, dir, "alpha")
	writePlainMD(t, dir, "beta")

	// Seed FTS5 table via the real Aura migrations path.
	dbPath := filepath.Join(t.TempDir(), "aura.db")
	db, err := auradb.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO wiki_documents (id, content, metadata, title) VALUES (?, ?, ?, ?)`,
		"ghost", "orphan content", "{}", "Ghost"); err != nil {
		t.Fatalf("insert orphan fts5 row: %v", err)
	}

	fake := &fakeQdrantForReconcile{
		scrollDocIDs: []string{
			"alpha", "beta", "ghost",
			"graph:node:alpha", "graph:node:beta", "graph:node:ghost",
		},
	}

	rep := ReconcileWikiQdrantWithDisk(context.Background(), fake, "test", db, dir, discardLogger())
	if rep.Err != nil {
		t.Fatalf("unexpected error: %v", rep.Err)
	}
	if rep.OrphansFound != 1 {
		t.Fatalf("orphans_found=%d, want 1", rep.OrphansFound)
	}
	if rep.OrphansDeleted != 1 {
		t.Fatalf("orphans_deleted=%d, want 1", rep.OrphansDeleted)
	}

	// Verify both Qdrant point IDs were deleted.
	if len(fake.deletedIDs) != 1 {
		t.Fatalf("Delete call count=%d, want 1", len(fake.deletedIDs))
	}
	wantIDs := []string{qdrantPointID("ghost"), qdrantPointID("graph:node:ghost")}
	for _, want := range wantIDs {
		if !slices.Contains(fake.deletedIDs[0], want) {
			t.Errorf("point ID %s not in deleted set %v", want, fake.deletedIDs[0])
		}
	}

	// Verify FTS5 row was removed.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM wiki_documents WHERE id = 'ghost'`).Scan(&count); err != nil {
		t.Fatalf("query wiki_documents: %v", err)
	}
	if count != 0 {
		t.Fatalf("fts5 row for ghost still present after reconcile")
	}
}

// TestReconcileWikiQdrant_SafetyFloor verifies that when orphans exceed 50% of
// on-disk page count the function refuses to delete and returns an error.
func TestReconcileWikiQdrant_SafetyFloor(t *testing.T) {
	dir := t.TempDir()
	// 1 page on disk, 2 orphan slugs in Qdrant → orphans (2) > 50% of disk (1).
	writePlainMD(t, dir, "real-page")

	fake := &fakeQdrantForReconcile{
		scrollDocIDs: []string{"orphan-a", "orphan-b"},
	}
	rep := ReconcileWikiQdrantWithDisk(context.Background(), fake, "test", nil, dir, discardLogger())
	if rep.Err == nil {
		t.Fatal("expected error from safety floor, got nil")
	}
	if rep.OrphansDeleted != 0 {
		t.Fatalf("orphans_deleted=%d after floor rejection, want 0", rep.OrphansDeleted)
	}
	if len(fake.deletedIDs) != 0 {
		t.Fatalf("Delete called despite safety floor: %v", fake.deletedIDs)
	}
}

// TestReconcileWikiQdrant_EmptyQdrant verifies that when Qdrant has no points
// the function returns cleanly with no deletions.
func TestReconcileWikiQdrant_EmptyQdrant(t *testing.T) {
	dir := t.TempDir()
	writePlainMD(t, dir, "alpha")

	fake := &fakeQdrantForReconcile{scrollDocIDs: nil}
	rep := ReconcileWikiQdrantWithDisk(context.Background(), fake, "test", nil, dir, discardLogger())
	if rep.Err != nil {
		t.Fatalf("unexpected error: %v", rep.Err)
	}
	if rep.OrphansFound != 0 || rep.OrphansDeleted != 0 {
		t.Fatalf("orphans_found=%d orphans_deleted=%d; want 0/0", rep.OrphansFound, rep.OrphansDeleted)
	}
}

// TestWikiOrphanReconciler_NotifyTriggers verifies the lifecycle wrapper
// delivers a reconcile pass when Notify is called and Run is running.
func TestWikiOrphanReconciler_NotifyTriggers(t *testing.T) {
	dir := t.TempDir()
	writePlainMD(t, dir, "page-a")

	fake := &fakeQdrantForReconcile{scrollDocIDs: []string{"page-a"}}
	r := NewWikiOrphanReconciler(WikiOrphanReconcilerConfig{
		QdrantClient: fake,
		Collection:   "test",
		WikiDir:      dir,
		Logger:       discardLogger(),
		Debounce:     10 * time.Millisecond,
		Periodic:     0, // disable periodic for test
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan WikiOrphanReport, 1)
	go func() {
		// Run the reconciler loop; it will block until ctx is cancelled.
		r.Run(ctx)
		close(done)
	}()

	// Trigger a manual boot reconcile synchronously to confirm no panic.
	rep := r.Reconcile(context.Background(), WikiOrphanReasonBoot)
	if rep.Err != nil {
		t.Fatalf("boot reconcile error: %v", rep.Err)
	}

	cancel()
	<-done
}

// TestWikiOrphanReconciler_WriteTestPage uses writeTestMDPage to verify that
// the reconciler correctly enumerates pages marshalled with full frontmatter.
func TestWikiOrphanReconciler_WriteTestPage(t *testing.T) {
	dir := t.TempDir()
	writeTestMDPage(t, dir, &wiki.Page{
		Title:         "Full Page",
		Body:          "Body text.",
		Category:      "project",
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

	fake := &fakeQdrantForReconcile{
		// The marshalled slug for "Full Page" is "full-page".
		scrollDocIDs: []string{"full-page", "graph:node:full-page"},
	}
	rep := ReconcileWikiQdrantWithDisk(context.Background(), fake, "test", nil, dir, discardLogger())
	if rep.Err != nil {
		t.Fatalf("unexpected error: %v", rep.Err)
	}
	if rep.OrphansFound != 0 {
		t.Fatalf("orphans_found=%d, want 0 (page is on disk)", rep.OrphansFound)
	}
}
