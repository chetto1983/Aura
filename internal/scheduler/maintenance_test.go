package scheduler_test

import (
	"context"
	"errors"
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

// mockWiki is a controllable WikiMaintainer for branch coverage tests.
type mockWiki struct {
	lintIssues  []wiki.LintIssue
	lintErr     error
	slugs       []string
	listErr     error
	repairErr   error
	repairCalls int
}

func (m *mockWiki) Lint(_ context.Context) ([]wiki.LintIssue, error) {
	return m.lintIssues, m.lintErr
}
func (m *mockWiki) ListPages() ([]string, error) {
	return m.slugs, m.listErr
}
func (m *mockWiki) RepairLink(_ context.Context, _, _ string) error {
	m.repairCalls++
	return m.repairErr
}

// TestMaintenanceJob_LintError covers the lint failure return path.
func TestMaintenanceJob_LintError(t *testing.T) {
	w := &mockWiki{lintErr: errors.New("lint unavailable")}
	job := scheduler.NewMaintenanceJob(w, nil)
	_, _, err := job.Run(context.Background())
	if err == nil {
		t.Fatal("want error when Lint fails, got nil")
	}
}

// TestMaintenanceJob_ListPagesError covers the ListPages failure return path.
func TestMaintenanceJob_ListPagesError(t *testing.T) {
	w := &mockWiki{
		lintIssues: []wiki.LintIssue{},
		listErr:    errors.New("list unavailable"),
	}
	job := scheduler.NewMaintenanceJob(w, nil)
	_, _, err := job.Run(context.Background())
	if err == nil {
		t.Fatal("want error when ListPages fails, got nil")
	}
}

// TestMaintenanceJob_RepairFails_Enqueues covers the repair-fails rollback path:
// RepairLink returns an error → issue is enqueued as high-severity deferred.
func TestMaintenanceJob_RepairFails_Enqueues(t *testing.T) {
	w := &mockWiki{
		lintIssues: []wiki.LintIssue{
			{Slug: "page-a", Message: "broken link: [[missing-slug]]"},
		},
		slugs:     []string{"missing-sluf"}, // distance 1 → single candidate
		repairErr: errors.New("repair failure"),
	}
	db := scheduler.NewTestDB(t)
	issuesStore := scheduler.NewIssuesStore(db)

	job := scheduler.NewMaintenanceJob(w, nil).
		WithIssuesStore(issuesStore)

	fixed, deferred, err := job.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fixed != 0 {
		t.Fatalf("want 0 fixed (repair failed), got %d", fixed)
	}
	if deferred != 1 {
		t.Fatalf("want 1 deferred, got %d", deferred)
	}

	// Verify the issue was persisted.
	rows, err := issuesStore.List(context.Background(), "open")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 issue in store, got %d", len(rows))
	}
}

// TestMaintenanceJob_NoCandidates_Enqueues covers the no-match deferred path:
// broken link with no slugs within distance 2 → enqueued.
func TestMaintenanceJob_NoCandidates_Enqueues(t *testing.T) {
	w := &mockWiki{
		lintIssues: []wiki.LintIssue{
			{Slug: "page-x", Message: "broken link: [[zzzzz]]"},
		},
		slugs: []string{"aaa", "bbb"}, // all far from "zzzzz"
	}
	db := scheduler.NewTestDB(t)
	issuesStore := scheduler.NewIssuesStore(db)

	job := scheduler.NewMaintenanceJob(w, nil).
		WithIssuesStore(issuesStore)

	fixed, deferred, err := job.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fixed != 0 {
		t.Fatalf("want 0 fixed, got %d", fixed)
	}
	if deferred != 1 {
		t.Fatalf("want 1 deferred, got %d", deferred)
	}
}

// TestMaintenanceJob_NonLinkIssue covers the non-broken-link branch
// (missing category and orphan issues → classifyKind + classifyNonLink).
func TestMaintenanceJob_NonLinkIssue(t *testing.T) {
	w := &mockWiki{
		lintIssues: []wiki.LintIssue{
			{Slug: "page-a", Message: "missing category"},
			{Slug: "page-b", Message: "orphan page"},
		},
		slugs: []string{"page-a", "page-b"},
	}
	job := scheduler.NewMaintenanceJob(w, nil)
	fixed, deferred, err := job.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fixed != 0 {
		t.Fatalf("want 0 fixed, got %d", fixed)
	}
	if deferred != 2 {
		t.Fatalf("want 2 deferred (non-link issues), got %d", deferred)
	}
}

// TestMaintenanceJob_OwnerNotifier_CalledOnce verifies that when multiple
// high-severity issues are found, the notifier is called exactly once.
func TestMaintenanceJob_OwnerNotifier_CalledOnce(t *testing.T) {
	w := &mockWiki{
		lintIssues: []wiki.LintIssue{
			{Slug: "p1", Message: "broken link: [[aaa]]"},
			{Slug: "p2", Message: "broken link: [[bbb]]"},
		},
		slugs: []string{}, // no candidates → both deferred as high-severity
	}

	notifyCalls := 0
	notifier := func(_ context.Context, _ string) { notifyCalls++ }

	job := scheduler.NewMaintenanceJob(w, nil).
		WithOwnerNotifier(notifier)

	_, deferred, err := job.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if deferred != 2 {
		t.Fatalf("want 2 deferred, got %d", deferred)
	}
	if notifyCalls != 1 {
		t.Fatalf("want notifier called once for batch of high issues, got %d", notifyCalls)
	}
}

// TestMaintenanceJob_WithIssuesStore_EnqueueFailure covers the enqueue-failed
// warn path (IssuesStore.Enqueue returns error → logged, not propagated).
func TestMaintenanceJob_WithIssuesStore_EnqueueFailure(t *testing.T) {
	w := &mockWiki{
		lintIssues: []wiki.LintIssue{
			{Slug: "p1", Message: "broken link: [[zzz]]"},
		},
		slugs: []string{}, // no candidates → deferred
	}
	db := scheduler.NewTestDB(t)
	issuesStore := scheduler.NewIssuesStore(db)
	db.Close() // close DB so Enqueue fails

	job := scheduler.NewMaintenanceJob(w, nil).
		WithIssuesStore(issuesStore)

	// Should not return error — enqueue failure is logged and swallowed.
	_, deferred, err := job.Run(context.Background())
	if err != nil {
		t.Fatalf("Run should not propagate enqueue error, got %v", err)
	}
	if deferred != 1 {
		t.Fatalf("want 1 deferred counted even on enqueue failure, got %d", deferred)
	}
}

// TestMaintenanceJob_Levenshtein2Boundary covers the distance-exactly-2 boundary:
// a slug at distance 2 is still a candidate; distance 3 is not.
func TestMaintenanceJob_Levenshtein2Boundary(t *testing.T) {
	// "fooo" is distance 2 from "fo" (delete 2 chars). It should be a candidate.
	// "foooo" is distance 3 from "fo". It should not be a candidate.
	w := &mockWiki{
		lintIssues: []wiki.LintIssue{
			{Slug: "page", Message: "broken link: [[fo]]"},
		},
		slugs: []string{"fooo", "foooo"}, // only "fooo" within distance 2
	}
	job := scheduler.NewMaintenanceJob(w, nil)
	fixed, deferred, err := job.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Single candidate "fooo" (distance 2) → auto-fixed (repairErr nil by default).
	if fixed != 1 {
		t.Fatalf("want 1 fixed (distance-2 candidate), got %d (deferred=%d)", fixed, deferred)
	}
}
