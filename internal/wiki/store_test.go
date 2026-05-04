package wiki

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestLintReportsMemoryDecay(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	page := &Page{
		Title:         "Old Decision",
		Body:          "A stable but old project decision.",
		Category:      "decisions",
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     "2025-01-01T00:00:00Z",
		UpdatedAt:     "2025-01-01T00:00:00Z",
	}
	if err := store.WritePage(ctx, page); err != nil {
		t.Fatalf("WritePage failed: %v", err)
	}

	issues, err := store.lintAt(ctx, time.Date(2025, 7, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("lintAt failed: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("want 1 decay issue, got %d: %#v", len(issues), issues)
	}
	issue := issues[0]
	if issue.Kind != "memory_decay" || issue.Severity != "high" || !strings.Contains(issue.Message, "decay=1.00") {
		t.Fatalf("unexpected decay issue: %#v", issue)
	}
}

func TestRepairLinkContinuesAfterWriteFailure(t *testing.T) {
	store, dir := newTestStore(t)
	ctx := context.Background()
	now := "2026-04-28T10:00:00Z"

	pages := []*Page{
		{
			Title:         "Alpha Page",
			Body:          "Alpha points to [[broken-link]].",
			Category:      "debug",
			SchemaVersion: CurrentSchemaVersion,
			PromptVersion: "v1",
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			Title:         "Bad Page",
			Body:          "Bad points to [[broken-link]].",
			Category:      "debug",
			SchemaVersion: CurrentSchemaVersion,
			PromptVersion: "v1",
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		{
			Title:         "Zulu Page",
			Body:          "Zulu points to [[broken-link]].",
			Category:      "debug",
			SchemaVersion: CurrentSchemaVersion,
			PromptVersion: "v1",
			CreatedAt:     now,
			UpdatedAt:     now,
		},
	}
	for _, page := range pages {
		if err := store.WritePage(ctx, page); err != nil {
			t.Fatalf("WritePage(%q) failed: %v", page.Title, err)
		}
	}

	// Make the middle page readable but invalid on rewrite. Older
	// RepairLink returned on this write failure, leaving zulu-page
	// unrepaired and skipping the auto-fix audit log.
	badPath := filepath.Join(dir, "bad-page.md")
	badBytes, err := os.ReadFile(badPath)
	if err != nil {
		t.Fatalf("read bad-page.md: %v", err)
	}
	badText := strings.Replace(string(badBytes), "schema_version: 2", "schema_version: 1", 1)
	if err := os.WriteFile(badPath, []byte(badText), 0o644); err != nil {
		t.Fatalf("corrupt bad-page.md: %v", err)
	}

	err = store.RepairLink(ctx, "broken-link", "fixed-link")
	if err == nil {
		t.Fatal("RepairLink returned nil, want partial-failure error")
	}
	if !strings.Contains(err.Error(), "bad-page") {
		t.Fatalf("RepairLink error = %q, want bad-page context", err)
	}

	alpha, err := store.ReadPage("alpha-page")
	if err != nil {
		t.Fatalf("ReadPage(alpha-page): %v", err)
	}
	if strings.Contains(alpha.Body, "[[broken-link]]") || !strings.Contains(alpha.Body, "[[fixed-link]]") {
		t.Fatalf("alpha-page body not repaired: %q", alpha.Body)
	}

	zulu, err := store.ReadPage("zulu-page")
	if err != nil {
		t.Fatalf("ReadPage(zulu-page): %v", err)
	}
	if strings.Contains(zulu.Body, "[[broken-link]]") || !strings.Contains(zulu.Body, "[[fixed-link]]") {
		t.Fatalf("zulu-page body not repaired after middle failure: %q", zulu.Body)
	}

	logBytes, err := os.ReadFile(filepath.Join(dir, "log.md"))
	if err != nil {
		t.Fatalf("read log.md: %v", err)
	}
	logText := string(logBytes)
	if !strings.Contains(logText, "auto-fix") || !strings.Contains(logText, "broken-link-&gt;fixed-link") && !strings.Contains(logText, "broken-link->fixed-link") {
		t.Fatalf("log.md missing auto-fix entry: %s", logText)
	}
}
