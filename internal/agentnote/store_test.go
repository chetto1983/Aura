package agentnote_test

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/agentnote"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/testutil"
)

func openStore(t *testing.T) *agentnote.Store {
	t.Helper()
	db := testutil.OpenTestDB(t, migrations.Run)
	return agentnote.NewStore(db)
}

func TestGetEmptyReturnsNotExists(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	content, exists, err := store.Get(ctx, "conv-1")
	if err != nil {
		t.Fatalf("Get empty: %v", err)
	}
	if exists {
		t.Fatal("Get empty: exists = true, want false")
	}
	if content != "" {
		t.Fatalf("Get empty: content = %q, want empty", content)
	}
}

func TestSetThenGetReturnsContent(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, "conv-2", "hello world"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	content, exists, err := store.Get(ctx, "conv-2")
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if !exists {
		t.Fatal("Get after Set: exists = false, want true")
	}
	if content != "hello world" {
		t.Fatalf("Get after Set: content = %q, want %q", content, "hello world")
	}
}

func TestAppendOnEmptyCreatesNote(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	if err := store.Append(ctx, "conv-3", "first line"); err != nil {
		t.Fatalf("Append on empty: %v", err)
	}
	content, exists, err := store.Get(ctx, "conv-3")
	if err != nil {
		t.Fatalf("Get after Append: %v", err)
	}
	if !exists {
		t.Fatal("Get after Append: exists = false")
	}
	if content != "first line" {
		t.Fatalf("Append on empty: content = %q, want %q", content, "first line")
	}
}

func TestAppendOnExistingConcatenatesWithNewline(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, "conv-4", "line one"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Append(ctx, "conv-4", "line two"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	content, _, err := store.Get(ctx, "conv-4")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := "line one\nline two"
	if content != want {
		t.Fatalf("Append on existing: content = %q, want %q", content, want)
	}
}

func TestClearDeletesRow(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, "conv-5", "to be cleared"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Clear(ctx, "conv-5"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	_, exists, err := store.Get(ctx, "conv-5")
	if err != nil {
		t.Fatalf("Get after Clear: %v", err)
	}
	if exists {
		t.Fatal("Get after Clear: exists = true, want false")
	}
}
