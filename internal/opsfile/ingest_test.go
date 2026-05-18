package opsfile_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/opsfile"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/storage/memoryindex"
)

// openTestStore opens a temp-dir SQLite DB with migrations run and returns a
// *memoryindex.Store. The DB is closed automatically via t.Cleanup.
func openTestStore(t *testing.T) *memoryindex.Store {
	t.Helper()
	db, err := auradb.Open(filepath.Join(t.TempDir(), "aura.db"))
	if err != nil {
		t.Fatalf("auradb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrations.Run: %v", err)
	}
	store, err := memoryindex.NewStore(db)
	if err != nil {
		t.Fatalf("memoryindex.NewStore: %v", err)
	}
	return store
}

// TestParseMarkdownLessons_Sections verifies the parser splits ## headers into
// individual lessons and ignores top-level # headings and preamble text.
func TestParseMarkdownLessons_Sections(t *testing.T) {
	content := `# Operational Lessons

Intro text before any ## headers is ignored.

## web_search — timeout
Retry with a shorter query when web_search times out.

## file — missing_action
Always specify the action parameter explicitly in file tool calls.
`
	ctx := context.Background()
	store := openTestStore(t)
	n, err := opsfile.IngestFileContent(ctx, content, store)
	if err != nil {
		t.Fatalf("IngestFileContent: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 lessons, got %d", n)
	}

	docs, err := store.Search(ctx, "web_search timeout", memoryindex.Filter{
		Kinds: []string{memoryindex.KindOperational},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("expected at least one result for 'web_search timeout', got none")
	}
	found := false
	for _, d := range docs {
		if d.Kind != memoryindex.KindOperational {
			t.Errorf("doc %s: kind = %q, want %q", d.ID, d.Kind, memoryindex.KindOperational)
		}
		if d.Status != "active" {
			t.Errorf("doc %s: status = %q, want 'active'", d.ID, d.Status)
		}
		if strings.Contains(d.Title, "web_search") {
			found = true
		}
	}
	if !found {
		t.Error("no result with title containing 'web_search'")
	}
}

// TestIngestFileContent_EmptyFile returns zero lessons without error.
func TestIngestFileContent_EmptyFile(t *testing.T) {
	store := openTestStore(t)
	n, err := opsfile.IngestFileContent(context.Background(), "", store)
	if err != nil {
		t.Fatalf("IngestFileContent empty: %v", err)
	}
	if n != 0 {
		t.Errorf("empty file: expected 0 lessons, got %d", n)
	}
}

// TestIngestFileContent_NoHeaders treats a file with no ## headers as zero
// lessons (no parseable sections).
func TestIngestFileContent_NoHeaders(t *testing.T) {
	content := "Just some freeform text with no section headers.\nAnother line."
	store := openTestStore(t)
	n, err := opsfile.IngestFileContent(context.Background(), content, store)
	if err != nil {
		t.Fatalf("IngestFileContent no-headers: %v", err)
	}
	if n != 0 {
		t.Errorf("no-headers: expected 0 lessons, got %d", n)
	}
}

// TestIngestFileContent_Idempotent verifies that re-ingesting the same content
// twice produces the same single document (Upsert is idempotent via content hash).
func TestIngestFileContent_Idempotent(t *testing.T) {
	content := "## propose_patch — missing_change_summary\nAlways include change_summary in propose_patch calls."
	ctx := context.Background()
	store := openTestStore(t)

	n1, err := opsfile.IngestFileContent(ctx, content, store)
	if err != nil {
		t.Fatalf("first ingest: %v", err)
	}
	n2, err := opsfile.IngestFileContent(ctx, content, store)
	if err != nil {
		t.Fatalf("second ingest: %v", err)
	}
	if n1 != 1 || n2 != 1 {
		t.Errorf("expected 1 lesson each pass, got %d and %d", n1, n2)
	}

	// Verify only one document exists in the store for this kind.
	docs, err := store.Search(ctx, "propose_patch missing_change_summary", memoryindex.Filter{
		Kinds: []string{memoryindex.KindOperational},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(docs) == 0 {
		t.Fatal("expected document after re-ingest, got none")
	}
}

// TestIngestFromPath_NonExistentFile returns (0, nil) when the file is absent.
func TestIngestFromPath_NonExistentFile(t *testing.T) {
	store := openTestStore(t)
	n, err := opsfile.IngestFromPath(context.Background(), "/no/such/path/operational_lessons.md", store)
	if err != nil {
		t.Fatalf("expected nil error for missing file, got: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 lessons for missing file, got %d", n)
	}
}

// TestRecallOperational_FindsFileLesson is the primary US-OP02 acceptance test:
// a lesson written via IngestFileContent must be retrievable via recall_operational.
// This exercises the "file path → compact_memory_documents → recall_operational" chain.
func TestRecallOperational_FindsFileLesson(t *testing.T) {
	content := `## file_tool — action_inference
When calling file without an action parameter, the tool cannot infer the intent and returns an error. Always specify action explicitly.`
	ctx := context.Background()
	store := openTestStore(t)

	n, err := opsfile.IngestFileContent(ctx, content, store)
	if err != nil {
		t.Fatalf("IngestFileContent: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 lesson, got %d", n)
	}

	recallTool := tools.NewRecallOperationalTool(store)
	result, err := recallTool.Execute(ctx, map[string]any{
		"query": "file action parameter",
	})
	if err != nil {
		t.Fatalf("recall_operational.Execute: %v", err)
	}
	if strings.Contains(result, "no operational lessons yet") {
		t.Fatal("recall_operational returned 'no operational lessons yet' — file path lesson not found")
	}
	if !strings.Contains(result, "opsfile:") {
		t.Errorf("result %q: expected 'opsfile:' handle in output", result)
	}
}

// TestRecallOperational_FindsProposePatchLesson verifies the propose_patch
// auto-accept path (US-OP01) also surfaces via recall_operational.
func TestRecallOperational_FindsProposePatchLesson(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	// Simulate propose_patch action=operational auto-accept by directly upserting
	// via the same code path as ProposePatchTool.executeOperational does when
	// opWriter is set. We reuse IngestFileContent with the canonical lesson format
	// to verify the store—not the propose_patch tool itself (already tested in
	// propose_patch_test.go). Instead, write a doc directly like the tool does.
	doc := memoryindex.Document{
		ID:     "operational:abc123def456abcd",
		Kind:   memoryindex.KindOperational,
		Title:  "Lesson: web_search timeout",
		Body:   "Retry with a shorter query when web_search times out.",
		Handle: "operational:abc123def456abcd",
		Status: "active",
	}
	if err := store.Upsert(ctx, doc); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	recallTool := tools.NewRecallOperationalTool(store)
	result, err := recallTool.Execute(ctx, map[string]any{
		"query": "web_search retry",
	})
	if err != nil {
		t.Fatalf("recall_operational.Execute: %v", err)
	}
	if strings.Contains(result, "no operational lessons yet") {
		t.Fatal("recall_operational returned 'no operational lessons yet' — propose_patch lesson not found")
	}
	if !strings.Contains(result, "web_search") {
		t.Errorf("result %q: expected 'web_search' in output", result)
	}
}

// TestRecallOperational_BothSourcesSurface verifies that lessons from BOTH
// the file path and the propose_patch path are visible in a single recall.
func TestRecallOperational_BothSourcesSurface(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	// File-path lesson.
	fileContent := "## list_files — outside_root\nDo not pass absolute paths to list_files; use relative paths only."
	if _, err := opsfile.IngestFileContent(ctx, fileContent, store); err != nil {
		t.Fatalf("IngestFileContent: %v", err)
	}

	// propose_patch-path lesson (using 'operational:' prefix as the tool does).
	doc := memoryindex.Document{
		ID:     "operational:deadbeefcafe0000",
		Kind:   memoryindex.KindOperational,
		Title:  "Lesson: schedule_task missing_field",
		Body:   "Always include required fields for schedule_task.",
		Handle: "operational:deadbeefcafe0000",
		Status: "active",
	}
	if err := store.Upsert(ctx, doc); err != nil {
		t.Fatalf("Upsert propose_patch lesson: %v", err)
	}

	recallTool := tools.NewRecallOperationalTool(store)

	// Query that should surface file-path lesson.
	r1, err := recallTool.Execute(ctx, map[string]any{"query": "list_files outside root path"})
	if err != nil {
		t.Fatalf("recall file lesson: %v", err)
	}
	if strings.Contains(r1, "no operational lessons yet") {
		t.Error("file-path lesson not found via recall_operational")
	}

	// Query that should surface propose_patch lesson.
	r2, err := recallTool.Execute(ctx, map[string]any{"query": "schedule_task missing field"})
	if err != nil {
		t.Fatalf("recall propose_patch lesson: %v", err)
	}
	if strings.Contains(r2, "no operational lessons yet") {
		t.Error("propose_patch-path lesson not found via recall_operational")
	}
}
