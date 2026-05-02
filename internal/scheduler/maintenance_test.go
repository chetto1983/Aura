package scheduler_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/wiki"
)

func newTestWiki(t *testing.T) *wiki.Store {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "wiki")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir wiki: %v", err)
	}
	store, err := wiki.NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func writeTestPage(t *testing.T, store *wiki.Store, slug, body string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	page := &wiki.Page{
		Title:         slug,
		Category:      "test",
		Body:          body,
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.WritePage(context.Background(), page); err != nil {
		t.Fatalf("WritePage %q: %v", slug, err)
	}
}

func TestMaintenanceJob_AutoFixesSingleMatch(t *testing.T) {
	wikiStore := newTestWiki(t)

	// Page "foo" exists.
	writeTestPage(t, wikiStore, "foo", "This is the foo page.")
	// Page "bar" has a typo link [[fooo]] — distance 1 from "foo".
	writeTestPage(t, wikiStore, "bar", "See also [[fooo]] for details.")
	// Unrelated page.
	writeTestPage(t, wikiStore, "baz", "Unrelated content.")

	job := scheduler.NewMaintenanceJob(wikiStore, nil)
	fixed, deferred, err := job.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fixed != 1 {
		t.Fatalf("want 1 auto-fixed, got %d", fixed)
	}
	if deferred != 0 {
		t.Fatalf("want 0 deferred, got %d", deferred)
	}

	// Verify the page body was repaired.
	page, err := wikiStore.ReadPage("bar")
	if err != nil {
		t.Fatalf("ReadPage bar: %v", err)
	}
	if page.Body == "See also [[fooo]] for details." {
		t.Fatal("want [[fooo]] replaced in body, but it wasn't")
	}
}

func TestMaintenanceJob_AmbiguousTypo_NotFixed(t *testing.T) {
	wikiStore := newTestWiki(t)

	// Two pages both close to "barr": "bar" (distance 1) and "bari" (distance 1).
	writeTestPage(t, wikiStore, "bar", "The bar page.")
	writeTestPage(t, wikiStore, "bari", "The bari page.")
	// "test" page has broken link [[barr]] — 2 candidates within distance 1.
	writeTestPage(t, wikiStore, "test", "See [[barr]] for info.")

	job := scheduler.NewMaintenanceJob(wikiStore, nil)
	fixed, deferred, err := job.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fixed != 0 {
		t.Fatalf("want 0 auto-fixed (ambiguous), got %d", fixed)
	}
	if deferred != 1 {
		t.Fatalf("want 1 deferred, got %d", deferred)
	}
}

func TestMaintenanceJob_NoBrokenLinks(t *testing.T) {
	wikiStore := newTestWiki(t)
	writeTestPage(t, wikiStore, "alpha", "No external links.")
	writeTestPage(t, wikiStore, "beta", "References [[alpha]] which exists.")

	job := scheduler.NewMaintenanceJob(wikiStore, nil)
	fixed, deferred, err := job.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fixed != 0 || deferred != 0 {
		t.Fatalf("want 0/0, got fixed=%d deferred=%d", fixed, deferred)
	}
}
