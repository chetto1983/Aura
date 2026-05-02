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
	// Manually resolve it.
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
	// Re-list: should be resolved now.
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
