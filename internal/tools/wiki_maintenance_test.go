package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aura/aura/internal/wiki"
)

// newTestWikiStore wires a fresh wiki.Store rooted at t.TempDir().
func newTestWikiStore(t *testing.T) (*wiki.Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := wiki.NewStore(dir, nil)
	if err != nil {
		t.Fatalf("wiki.NewStore: %v", err)
	}
	return store, dir
}

// putPage writes a wiki page; slug is derived from title via wiki.Slug
// (mirroring production behavior). Tests pass the title and reason about
// the resulting slug; never pin a slug independently.
func putPage(t *testing.T, store *wiki.Store, title, category, body string, related, sources []string) {
	t.Helper()
	page := &wiki.Page{
		Title:         title,
		Body:          body,
		Category:      category,
		Tags:          []string{"test"},
		Related:       related,
		Sources:       sources,
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "ingest_v1",
		CreatedAt:     "2026-04-30T00:00:00Z",
		UpdatedAt:     "2026-04-30T00:00:00Z",
	}
	if err := store.WritePage(context.Background(), page); err != nil {
		t.Fatalf("WritePage(%s): %v", title, err)
	}
}

func TestListWikiTool_Empty(t *testing.T) {
	store, _ := newTestWikiStore(t)
	tool := NewListWikiTool(store)

	if tool.Name() != "list_wiki" {
		t.Errorf("Name = %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description is empty")
	}

	out, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Wiki is empty") {
		t.Errorf("expected empty marker, got: %s", out)
	}
}

func TestListWikiTool_GroupedByCategory(t *testing.T) {
	store, _ := newTestWikiStore(t)
	putPage(t, store, "Alpha Page", "engineering", "body alpha", nil, nil)
	putPage(t, store, "Beta Page", "engineering", "body beta", nil, nil)
	putPage(t, store, "Source: uta", "sources", "body uta", nil, []string{"source:src_x"})

	tool := NewListWikiTool(store)
	out, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		"3 wiki page(s)",
		"## engineering",
		"## sources",
		"[[alpha-page]] Alpha Page",
		"[[beta-page]] Beta Page",
		"[[source-uta]] Source: uta",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// engineering must appear before sources (alphabetical category order).
	engIdx := strings.Index(out, "## engineering")
	srcIdx := strings.Index(out, "## sources")
	if engIdx == -1 || srcIdx == -1 || engIdx >= srcIdx {
		t.Errorf("category ordering wrong: eng=%d src=%d", engIdx, srcIdx)
	}
}

func TestListWikiTool_CategoryFilter(t *testing.T) {
	store, _ := newTestWikiStore(t)
	putPage(t, store, "Alpha", "engineering", "body", nil, nil)
	putPage(t, store, "Source: x", "sources", "body", nil, nil)

	tool := NewListWikiTool(store)
	out, err := tool.Execute(context.Background(), map[string]any{"category": "sources"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "[[source-x]]") {
		t.Errorf("filter should keep source-x: %s", out)
	}
	if strings.Contains(out, "[[alpha]]") {
		t.Errorf("filter should drop alpha: %s", out)
	}
	if !strings.Contains(out, "category \"sources\"") {
		t.Errorf("output should mention the filter: %s", out)
	}

	// Case-insensitive filter — the LLM may pass any casing.
	out2, err := tool.Execute(context.Background(), map[string]any{"category": "SOURCES"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out2, "[[source-x]]") {
		t.Errorf("case-insensitive filter failed: %s", out2)
	}
}

func TestListWikiTool_FilterEmpty(t *testing.T) {
	store, _ := newTestWikiStore(t)
	putPage(t, store, "Alpha", "engineering", "body", nil, nil)

	tool := NewListWikiTool(store)
	out, err := tool.Execute(context.Background(), map[string]any{"category": "missing"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "No wiki pages found") {
		t.Errorf("expected 'No wiki pages found': %s", out)
	}
}

func TestListWikiTool_Limit(t *testing.T) {
	store, _ := newTestWikiStore(t)
	for i := range 5 {
		title := "Title " + string(rune('A'+i))
		putPage(t, store, title, "general", "body", nil, nil)
	}

	tool := NewListWikiTool(store)
	out, err := tool.Execute(context.Background(), map[string]any{"limit": 2})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "5 wiki page(s) (showing first 2)") {
		t.Errorf("expected truncation marker: %s", out)
	}
	if strings.Count(out, "[[") < 2 {
		t.Errorf("should list at least 2 entries: %s", out)
	}
}

func TestListWikiTool_NilStore(t *testing.T) {
	tool := NewListWikiTool(nil)
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("expected error on nil store")
	}
}

func TestLintWikiTool_Clean(t *testing.T) {
	store, _ := newTestWikiStore(t)
	putPage(t, store, "Alpha", "engineering", "body alpha", nil, nil)

	tool := NewLintWikiTool(store)
	out, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Wiki is clean") {
		t.Errorf("expected clean marker: %s", out)
	}
}

func TestLintWikiTool_GroupsBrokenLinks(t *testing.T) {
	store, _ := newTestWikiStore(t)
	// Two issues on alpha: broken link + broken related.
	putPage(t, store, "Alpha", "engineering", "see [[ghost]]", []string{"missing-related"}, nil)
	// Beta has a missing category to exercise the third issue type.
	putPage(t, store, "Beta", "", "fine body", nil, nil)

	tool := NewLintWikiTool(store)
	out, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		"## [[alpha]]",
		"broken link: [[ghost]]",
		"broken related ref: missing-related",
		"## [[beta]]",
		"missing category",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLintWikiTool_NilStore(t *testing.T) {
	tool := NewLintWikiTool(nil)
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("expected error on nil store")
	}
}

func TestRebuildIndexTool(t *testing.T) {
	store, dir := newTestWikiStore(t)
	putPage(t, store, "Alpha", "engineering", "body", nil, nil)
	putPage(t, store, "Beta", "engineering", "body", nil, nil)

	// Corrupt index.md to prove the rebuild actually rewrites it.
	indexPath := filepath.Join(dir, "index.md")
	if err := os.WriteFile(indexPath, []byte("CORRUPTED"), 0o644); err != nil {
		t.Fatalf("seed corrupt index: %v", err)
	}

	tool := NewRebuildIndexTool(store)
	out, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "2 page(s)") {
		t.Errorf("expected page count: %s", out)
	}

	idx, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read rebuilt index: %v", err)
	}
	body := string(idx)
	if strings.Contains(body, "CORRUPTED") {
		t.Errorf("rebuild didn't overwrite corrupted index")
	}
	if !strings.Contains(body, "[[alpha]]") || !strings.Contains(body, "[[beta]]") {
		t.Errorf("rebuilt index missing pages: %s", body)
	}
}

func TestAppendLogTool_WithSlug(t *testing.T) {
	store, dir := newTestWikiStore(t)
	tool := NewAppendLogTool(store)

	out, err := tool.Execute(context.Background(), map[string]any{
		"action": "query",
		"slug":   "alpha",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Logged: query [[alpha]]") {
		t.Errorf("response = %q", out)
	}

	logBytes, err := os.ReadFile(filepath.Join(dir, "log.md"))
	if err != nil {
		t.Fatalf("read log.md: %v", err)
	}
	body := string(logBytes)
	if !strings.Contains(body, "| query | [[alpha]] |") {
		t.Errorf("log.md missing entry: %s", body)
	}
}

func TestAppendLogTool_EmptySlug(t *testing.T) {
	store, dir := newTestWikiStore(t)
	tool := NewAppendLogTool(store)

	out, err := tool.Execute(context.Background(), map[string]any{"action": "summary"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Logged: summary") {
		t.Errorf("response = %q", out)
	}
	if strings.Contains(out, "[[") {
		t.Errorf("empty slug should not produce wiki-link in response: %q", out)
	}

	logBytes, err := os.ReadFile(filepath.Join(dir, "log.md"))
	if err != nil {
		t.Fatalf("read log.md: %v", err)
	}
	body := string(logBytes)
	// Empty slug must render as a blank cell, never as the literal "[[]]"
	// (which would appear as a broken wiki-link in the rendered table).
	if strings.Contains(body, "[[]]") {
		t.Errorf("log.md should not contain literal [[]] for empty slug:\n%s", body)
	}
	if !strings.Contains(body, "| summary |  |") {
		t.Errorf("log.md should have empty page cell:\n%s", body)
	}
}

func TestAppendLogTool_TruncatesLongAction(t *testing.T) {
	store, dir := newTestWikiStore(t)
	tool := NewAppendLogTool(store)

	long := strings.Repeat("x", 200)
	if _, err := tool.Execute(context.Background(), map[string]any{"action": long}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	logBytes, err := os.ReadFile(filepath.Join(dir, "log.md"))
	if err != nil {
		t.Fatalf("read log.md: %v", err)
	}
	body := string(logBytes)
	// Action is capped at maxLogActionChars (50). The log row should
	// contain exactly that many x's surrounded by table separators, never
	// the full 200-char value.
	if strings.Contains(body, strings.Repeat("x", 51)) {
		t.Errorf("action wasn't truncated to %d chars:\n%s", maxLogActionChars, body)
	}
	if !strings.Contains(body, strings.Repeat("x", maxLogActionChars)) {
		t.Errorf("truncated action missing from log.md:\n%s", body)
	}
}

func TestAppendLogTool_RejectsEmptyAction(t *testing.T) {
	store, _ := newTestWikiStore(t)
	tool := NewAppendLogTool(store)

	if _, err := tool.Execute(context.Background(), map[string]any{"action": "   "}); err == nil {
		t.Error("expected error on whitespace-only action")
	}
	if _, err := tool.Execute(context.Background(), map[string]any{}); err == nil {
		t.Error("expected error on missing action")
	}
}

func TestAppendLogTool_NilStore(t *testing.T) {
	tool := NewAppendLogTool(nil)
	if _, err := tool.Execute(context.Background(), map[string]any{"action": "q"}); err == nil {
		t.Error("expected error on nil store")
	}
}
