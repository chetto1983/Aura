package wiki

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	logger := slog.Default()
	store, err := NewStore(dir, logger)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	return store, dir
}

func TestNewStoreCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "wiki")
	store, err := NewStore(dir, slog.Default())
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("wiki dir not created")
	}
	_ = store
}

func TestNewStoreInitGit(t *testing.T) {
	store, dir := newTestStore(t)
	_ = store

	gitDir := filepath.Join(dir, ".git")
	if fi, err := os.Stat(gitDir); err != nil || !fi.IsDir() {
		t.Fatalf(".git dir not created")
	}
}

func TestWritePage(t *testing.T) {
	store, dir := newTestStore(t)

	page := &Page{
		Title:         "Test Page",
		Body:          "This is test content.",
		Tags:          []string{"test"},
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     "2026-04-28T10:00:00Z",
		UpdatedAt:     "2026-04-28T10:00:00Z",
	}

	if err := store.WritePage(context.Background(), page); err != nil {
		t.Fatalf("WritePage failed: %v", err)
	}

	// Verify file exists as .md
	path := filepath.Join(dir, "test-page.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("wiki file not created: %v", err)
	}

	// Verify content roundtrips
	readBack, err := store.ReadPage("test-page")
	if err != nil {
		t.Fatalf("ReadPage failed: %v", err)
	}
	if readBack.Title != page.Title {
		t.Errorf("title = %q, want %q", readBack.Title, page.Title)
	}
	if readBack.Body != page.Body {
		t.Errorf("body = %q, want %q", readBack.Body, page.Body)
	}
}

func TestWritePageAtomic(t *testing.T) {
	store, dir := newTestStore(t)

	page := &Page{
		Title:         "Atomic Test",
		Body:          "Initial content.",
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     "2026-04-28T10:00:00Z",
		UpdatedAt:     "2026-04-28T10:00:00Z",
	}

	if err := store.WritePage(context.Background(), page); err != nil {
		t.Fatalf("WritePage failed: %v", err)
	}

	// Update the page
	page.Body = "Updated content."
	page.UpdatedAt = "2026-04-28T11:00:00Z"
	if err := store.WritePage(context.Background(), page); err != nil {
		t.Fatalf("WritePage update failed: %v", err)
	}

	readBack, err := store.ReadPage("atomic-test")
	if err != nil {
		t.Fatalf("ReadPage failed: %v", err)
	}
	if readBack.Body != "Updated content." {
		t.Errorf("body = %q, want %q", readBack.Body, "Updated content.")
	}

	// Verify no temp files left behind
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestWritePageGitCommit(t *testing.T) {
	store, dir := newTestStore(t)

	page := &Page{
		Title:         "Git Test",
		Body:          "Git tracked content.",
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     "2026-04-28T10:00:00Z",
		UpdatedAt:     "2026-04-28T10:00:00Z",
	}

	if err := store.WritePage(context.Background(), page); err != nil {
		t.Fatalf("WritePage failed: %v", err)
	}

	// Verify git log has a commit
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("opening git repo: %v", err)
	}

	iter, err := repo.Log(&git.LogOptions{})
	if err != nil {
		t.Fatalf("git log: %v", err)
	}

	count := 0
	if _, err := iter.Next(); err != nil {
		t.Fatal("expected at least one git commit")
	}
	count++
	_ = count
}

func TestReadPageNotFound(t *testing.T) {
	store, _ := newTestStore(t)

	_, err := store.ReadPage("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent page")
	}
}

func TestListPages(t *testing.T) {
	store, _ := newTestStore(t)

	page1 := &Page{
		Title:         "Page One",
		Body:          "Content 1",
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     "2026-04-28T10:00:00Z",
		UpdatedAt:     "2026-04-28T10:00:00Z",
	}
	page2 := &Page{
		Title:         "Page Two",
		Body:          "Content 2",
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     "2026-04-28T10:00:00Z",
		UpdatedAt:     "2026-04-28T10:00:00Z",
	}

	store.WritePage(context.Background(), page1)
	store.WritePage(context.Background(), page2)

	slugs, err := store.ListPages()
	if err != nil {
		t.Fatalf("ListPages failed: %v", err)
	}
	if len(slugs) != 2 {
		t.Errorf("ListPages returned %d slugs, want 2", len(slugs))
	}
}
