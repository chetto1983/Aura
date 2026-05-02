package scheduler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aura/aura/internal/scheduler"
)

func newIssuesStore(t *testing.T) *scheduler.IssuesStore {
	t.Helper()
	db := scheduler.NewTestDB(t)
	return scheduler.NewIssuesStore(db)
}

func TestIssuesStore_EnqueueAndList(t *testing.T) {
	store := newIssuesStore(t)
	ctx := context.Background()

	issue := scheduler.Issue{
		Kind:       "broken_link",
		Severity:   "high",
		Slug:       "page-a",
		BrokenLink: "missing-slug",
		Message:    "broken link: [[missing-slug]]",
	}
	if err := store.Enqueue(ctx, issue); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	rows, err := store.List(ctx, "open")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].Severity != "high" {
		t.Fatalf("want severity=high, got %q", rows[0].Severity)
	}
}

func TestIssuesStore_Enqueue_Idempotent(t *testing.T) {
	store := newIssuesStore(t)
	ctx := context.Background()

	issue := scheduler.Issue{
		Kind:       "broken_link",
		Severity:   "high",
		Slug:       "page-a",
		BrokenLink: "missing-slug",
		Message:    "broken link: [[missing-slug]]",
	}
	if err := store.Enqueue(ctx, issue); err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	if err := store.Enqueue(ctx, issue); err != nil {
		t.Fatalf("second Enqueue (idempotent): %v", err)
	}

	rows, err := store.List(ctx, "open")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row after idempotent enqueue, got %d", len(rows))
	}
}

func TestIssuesStore_List_FiltersByStatus(t *testing.T) {
	store := newIssuesStore(t)
	ctx := context.Background()

	if err := store.Enqueue(ctx, scheduler.Issue{
		Kind: "broken_link", Severity: "high", Slug: "p1", BrokenLink: "x", Message: "m",
	}); err != nil {
		t.Fatal(err)
	}
	rows, _ := store.List(ctx, "open")
	if len(rows) != 1 {
		t.Fatalf("setup: want 1 open, got %d", len(rows))
	}
	if err := store.Resolve(ctx, rows[0].ID); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	open, _ := store.List(ctx, "open")
	if len(open) != 0 {
		t.Fatalf("want 0 open after resolve, got %d", len(open))
	}
	resolved, _ := store.List(ctx, "resolved")
	if len(resolved) != 1 {
		t.Fatalf("want 1 resolved, got %d", len(resolved))
	}
}

func TestIssuesStore_Resolve_HappyPath(t *testing.T) {
	store := newIssuesStore(t)
	ctx := context.Background()

	if err := store.Enqueue(ctx, scheduler.Issue{
		Kind: "missing_category", Severity: "low", Slug: "p2", Message: "m",
	}); err != nil {
		t.Fatal(err)
	}
	rows, _ := store.List(ctx, "open")
	if len(rows) == 0 {
		t.Fatal("no rows to resolve")
	}
	if err := store.Resolve(ctx, rows[0].ID); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	open, _ := store.List(ctx, "open")
	if len(open) != 0 {
		t.Fatal("want 0 open after resolve")
	}
}

func TestIssuesStore_Resolve_NotFound(t *testing.T) {
	store := newIssuesStore(t)
	err := store.Resolve(context.Background(), 9999)
	if !errors.Is(err, scheduler.ErrIssueNotFound) {
		t.Fatalf("want ErrIssueNotFound, got %v", err)
	}
}

// TestIssuesStore_Resolve_AlreadyResolved covers the row-exists-but-not-open
// path. After HR-07, this returns ErrIssueAlreadyResolved (was nil) so the
// API layer can map cleanly to 409 even under a race.
func TestIssuesStore_Resolve_AlreadyResolved(t *testing.T) {
	store := newIssuesStore(t)
	ctx := context.Background()

	if err := store.Enqueue(ctx, scheduler.Issue{
		Kind: "orphan", Severity: "medium", Slug: "p3", Message: "m",
	}); err != nil {
		t.Fatal(err)
	}
	rows, _ := store.List(ctx, "open")
	id := rows[0].ID

	if err := store.Resolve(ctx, id); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if err := store.Resolve(ctx, id); !errors.Is(err, scheduler.ErrIssueAlreadyResolved) {
		t.Fatalf("second Resolve should return ErrIssueAlreadyResolved, got %v", err)
	}
}

// TestIssuesStore_List_AllStatuses covers the no-filter (empty status) path.
func TestIssuesStore_List_AllStatuses(t *testing.T) {
	store := newIssuesStore(t)
	ctx := context.Background()

	if err := store.Enqueue(ctx, scheduler.Issue{
		Kind: "broken_link", Severity: "high", Slug: "p1", BrokenLink: "x", Message: "m1",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Enqueue(ctx, scheduler.Issue{
		Kind: "orphan", Severity: "medium", Slug: "p2", Message: "m2",
	}); err != nil {
		t.Fatal(err)
	}
	rows, _ := store.List(ctx, "open")
	_ = store.Resolve(ctx, rows[0].ID)

	all, err := store.List(ctx, "")
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 total rows, got %d", len(all))
	}
}

// TestIssuesStore_Enqueue_ClosedDB covers the Enqueue DB error path.
func TestIssuesStore_Enqueue_ClosedDB(t *testing.T) {
	db := scheduler.NewTestDB(t)
	store := scheduler.NewIssuesStore(db)
	db.Close()

	err := store.Enqueue(context.Background(), scheduler.Issue{
		Kind: "orphan", Severity: "medium", Slug: "p", Message: "m",
	})
	if err == nil {
		t.Fatal("want error from closed DB, got nil")
	}
}

// TestIssuesStore_List_ClosedDB covers the List query error path.
func TestIssuesStore_List_ClosedDB(t *testing.T) {
	db := scheduler.NewTestDB(t)
	store := scheduler.NewIssuesStore(db)
	db.Close()

	_, err := store.List(context.Background(), "open")
	if err == nil {
		t.Fatal("want error from closed DB, got nil")
	}
}

// TestIssuesStore_Resolve_ClosedDB covers the Resolve DB exec error path.
func TestIssuesStore_Resolve_ClosedDB(t *testing.T) {
	db := scheduler.NewTestDB(t)
	store := scheduler.NewIssuesStore(db)
	db.Close()

	err := store.Resolve(context.Background(), 1)
	if err == nil {
		t.Fatal("want error from closed DB, got nil")
	}
	if errors.Is(err, scheduler.ErrIssueNotFound) {
		t.Fatal("want DB error, not ErrIssueNotFound")
	}
}

// TestIssuesStore_Get_HappyPath covers Get returning an existing issue.
func TestIssuesStore_Get_HappyPath(t *testing.T) {
	store := newIssuesStore(t)
	ctx := context.Background()

	if err := store.Enqueue(ctx, scheduler.Issue{
		Kind: "broken_link", Severity: "high", Slug: "pg", BrokenLink: "target", Message: "msg",
	}); err != nil {
		t.Fatal(err)
	}
	rows, _ := store.List(ctx, "open")
	id := rows[0].ID

	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Slug != "pg" {
		t.Fatalf("want slug=pg, got %q", got.Slug)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("CreatedAt is zero")
	}
}

// TestIssuesStore_Get_NotFound covers Get returning ErrIssueNotFound.
func TestIssuesStore_Get_NotFound(t *testing.T) {
	store := newIssuesStore(t)
	_, err := store.Get(context.Background(), 9999)
	if !errors.Is(err, scheduler.ErrIssueNotFound) {
		t.Fatalf("want ErrIssueNotFound, got %v", err)
	}
}

// TestIssuesStore_ListBySeverity verifies multi-issue lists contain expected severities.
func TestIssuesStore_ListBySeverity(t *testing.T) {
	store := newIssuesStore(t)
	ctx := context.Background()

	issues := []scheduler.Issue{
		{Kind: "broken_link", Severity: "high", Slug: "p1", BrokenLink: "a", Message: "m1"},
		{Kind: "orphan", Severity: "medium", Slug: "p2", Message: "m2"},
		{Kind: "missing_category", Severity: "low", Slug: "p3", Message: "m3"},
	}
	for _, iss := range issues {
		if err := store.Enqueue(ctx, iss); err != nil {
			t.Fatalf("Enqueue %q: %v", iss.Severity, err)
		}
	}

	all, err := store.List(ctx, "open")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 issues, got %d", len(all))
	}

	severities := map[string]bool{}
	for _, row := range all {
		severities[row.Severity] = true
	}
	for _, want := range []string{"high", "medium", "low"} {
		if !severities[want] {
			t.Errorf("missing severity %q in results", want)
		}
	}
}
