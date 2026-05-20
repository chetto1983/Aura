package tools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/reindex"
	"github.com/aura/aura/internal/wiki"
)

// newTestWikiStore returns a *wiki.Store rooted at t.TempDir() with a
// silent logger. Real signature (verified at internal/wiki/store.go):
//
//	func NewStore(dir string, logger *slog.Logger) (*Store, error)
func newTestWikiStore(t *testing.T) *wiki.Store {
	t.Helper()
	silent := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := wiki.NewStore(t.TempDir(), silent)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type fakeSubmitter struct {
	calls int64
	last  reindex.Job
}

func (f *fakeSubmitter) Submit(j reindex.Job) bool {
	f.calls++
	f.last = j
	return true
}

// seedPage writes a starter page directly through the store so the
// action tests have something to read/edit/append against. Returns the
// page's updated_at so callers can pass it as expected_updated_at.
func seedPage(t *testing.T, s *wiki.Store, title, body string, related, sources []string) string {
	t.Helper()
	now := "2026-05-12T12:00:00Z"
	page := &wiki.Page{
		Title:         title,
		Body:          body,
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
		Related:       wiki.RelatedFromSlugs(related),
		Sources:       sources,
	}
	if err := s.WritePage(context.Background(), page); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := s.ReadPage(wiki.Slug(title))
	if err != nil {
		t.Fatal(err)
	}
	return got.UpdatedAt
}

func TestWikiPage_Name(t *testing.T) {
	tool := NewWikiPageTool(newTestWikiStore(t), nil)
	if tool == nil {
		t.Fatal("NewWikiPageTool returned nil with non-nil store")
	}
	if got := tool.Name(); got != "wiki_page" {
		t.Fatalf("Name() = %q, want %q", got, "wiki_page")
	}
}

func TestWikiPage_NilStore(t *testing.T) {
	if tool := NewWikiPageTool(nil, nil); tool != nil {
		t.Fatal("NewWikiPageTool(nil, ...) should return nil")
	}
}

func TestWikiPage_RejectsUnknownAction(t *testing.T) {
	tool := NewWikiPageTool(newTestWikiStore(t), nil)
	_, err := tool.Execute(context.Background(), map[string]any{"action": "yeet"})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Fatalf("expected schema validation error, got %v", err)
	}
}

func TestWikiPage_Create_HappyPath(t *testing.T) {
	s := newTestWikiStore(t)
	tool := NewWikiPageTool(s, nil)
	out, err := tool.Execute(context.Background(), map[string]any{
		"action": "create",
		"title":  "Alpha Project",
		"body":   "Notes about [[beta]].",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var result map[string]string
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("result not JSON: %v\n%s", err, out)
	}
	if result["status"] != "ok" || result["slug"] != "alpha-project" {
		t.Fatalf("result = %+v", result)
	}
	got, err := s.ReadPage("alpha-project")
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "Notes about [[beta]]." {
		t.Fatalf("body persisted incorrectly: %q", got.Body)
	}
}

func TestWikiPage_Create_RequiresTitle(t *testing.T) {
	tool := NewWikiPageTool(newTestWikiStore(t), nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"action": "create",
		"body":   "x",
	})
	if err == nil {
		t.Fatal("expected error for missing title")
	}
}

func TestWikiPage_Create_ConflictOnExistingSlug(t *testing.T) {
	s := newTestWikiStore(t)
	seedPage(t, s, "Existing", "body", nil, nil)
	tool := NewWikiPageTool(s, nil)
	out, err := tool.Execute(context.Background(), map[string]any{
		"action": "create",
		"title":  "Existing",
		"body":   "new body",
	})
	if err != nil {
		t.Fatalf("conflict should surface as result, not error: %v", err)
	}
	var result map[string]string
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("not JSON: %s", out)
	}
	if result["error"] != "conflict" {
		t.Fatalf("expected conflict result, got %+v", result)
	}
}

func TestWikiPage_Replace_OverwritesBody(t *testing.T) {
	s := newTestWikiStore(t)
	stamp := seedPage(t, s, "Target", "old body", nil, nil)
	tool := NewWikiPageTool(s, nil)
	out, err := tool.Execute(context.Background(), map[string]any{
		"action":              "replace",
		"slug":                "target",
		"body":                "new body about [[beta]]",
		"expected_updated_at": stamp,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	_ = out
	got, err := s.ReadPage("target")
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "new body about [[beta]]" {
		t.Fatalf("body not replaced: %q", got.Body)
	}
	if got.Title != "Target" {
		t.Fatalf("title not preserved: %q", got.Title)
	}
}

func TestWikiPage_Replace_PreservesUnsetFrontmatter(t *testing.T) {
	s := newTestWikiStore(t)
	stamp := seedPage(t, s, "Target", "body", []string{"existing-related"}, []string{"source:src_x"})
	tool := NewWikiPageTool(s, nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"action":              "replace",
		"slug":                "target",
		"body":                "new body",
		"expected_updated_at": stamp,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadPage("target")
	if err != nil {
		t.Fatal(err)
	}
	if !wiki.RelatedContainsSlug(got.Related, "existing-related") {
		t.Fatalf("related not preserved: %v", got.Related)
	}
	if !containsStringSlice(got.Sources, "source:src_x") {
		t.Fatalf("sources not preserved: %v", got.Sources)
	}
}

func TestWikiPage_Replace_ConflictOnStaleETag(t *testing.T) {
	s := newTestWikiStore(t)
	seedPage(t, s, "Target", "body", nil, nil)
	tool := NewWikiPageTool(s, nil)
	out, err := tool.Execute(context.Background(), map[string]any{
		"action":              "replace",
		"slug":                "target",
		"body":                "x",
		"expected_updated_at": "1999-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("conflict should be a result, not an error: %v", err)
	}
	if !strings.Contains(out, `"error":"conflict"`) {
		t.Fatalf("expected conflict result: %s", out)
	}
}

func TestWikiPage_Edit_SingleMatchReplaces(t *testing.T) {
	s := newTestWikiStore(t)
	stamp := seedPage(t, s, "Target", "The quick brown fox.", nil, nil)
	tool := NewWikiPageTool(s, nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"action":              "edit",
		"slug":                "target",
		"old_text":            "brown fox",
		"new_text":            "red panda",
		"expected_updated_at": stamp,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, err := s.ReadPage("target")
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "The quick red panda." {
		t.Fatalf("edit produced %q", got.Body)
	}
}

func TestWikiPage_Edit_RejectsZeroMatches(t *testing.T) {
	s := newTestWikiStore(t)
	stamp := seedPage(t, s, "Target", "some body", nil, nil)
	tool := NewWikiPageTool(s, nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"action":              "edit",
		"slug":                "target",
		"old_text":            "missing",
		"new_text":            "x",
		"expected_updated_at": stamp,
	})
	if err == nil {
		t.Fatal("expected error for zero-match edit")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
}

func TestWikiPage_Edit_RejectsMultipleMatches(t *testing.T) {
	s := newTestWikiStore(t)
	stamp := seedPage(t, s, "Target", "foo bar foo", nil, nil)
	tool := NewWikiPageTool(s, nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"action":              "edit",
		"slug":                "target",
		"old_text":            "foo",
		"new_text":            "X",
		"expected_updated_at": stamp,
	})
	if err == nil {
		t.Fatal("expected error for ambiguous edit")
	}
	if !strings.Contains(err.Error(), "appears") {
		t.Fatalf("expected ambiguity error, got: %v", err)
	}
}

func TestWikiPage_Append_AddsSection(t *testing.T) {
	s := newTestWikiStore(t)
	stamp := seedPage(t, s, "Target", "existing body", nil, nil)
	tool := NewWikiPageTool(s, nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"action":              "append",
		"slug":                "target",
		"heading":             "Update",
		"body":                "Latest news.",
		"expected_updated_at": stamp,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, err := s.ReadPage("target")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Body, "## Update") {
		t.Fatalf("append did not add the heading:\n%s", got.Body)
	}
	if !strings.Contains(got.Body, "Latest news.") {
		t.Fatalf("append did not add the body:\n%s", got.Body)
	}
}

func TestWikiPage_Append_WithSourceIDAnnotatesAndAddsSources(t *testing.T) {
	s := newTestWikiStore(t)
	stamp := seedPage(t, s, "Target", "existing body", nil, nil)
	tool := NewWikiPageTool(s, nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"action":              "append",
		"slug":                "target",
		"heading":             "From a new source",
		"body":                "Aura mentioned this fact.\n\nAnother point.",
		"source_id":           "src_abc123",
		"expected_updated_at": stamp,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, err := s.ReadPage("target")
	if err != nil {
		t.Fatal(err)
	}
	// Both non-heading lines should carry the provenance marker.
	if strings.Count(got.Body, "^[src_abc123]") < 2 {
		t.Fatalf("expected ≥2 provenance markers, body:\n%s", got.Body)
	}
	// Sources frontmatter should have the new source tagged.
	if !containsStringSlice(got.Sources, "source:src_abc123") {
		t.Fatalf("source not appended to Sources: %v", got.Sources)
	}
}

func TestWikiPage_Append_BlockquoteLinesGetInnerAnnotation(t *testing.T) {
	s := newTestWikiStore(t)
	stamp := seedPage(t, s, "Target", "existing", nil, nil)
	tool := NewWikiPageTool(s, nil)
	_, err := tool.Execute(context.Background(), map[string]any{
		"action":              "append",
		"slug":                "target",
		"heading":             "Quotes",
		"body":                "> claim one\n> claim two",
		"source_id":           "src_xyz",
		"expected_updated_at": stamp,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadPage("target")
	if err != nil {
		t.Fatal(err)
	}
	// Blockquote lines: marker goes after the quote content, with "> " prefix intact.
	if !strings.Contains(got.Body, "> claim one ^[src_xyz]") {
		t.Fatalf("blockquote not annotated correctly:\n%s", got.Body)
	}
}

func TestWikiPage_SubmitsReindex(t *testing.T) {
	s := newTestWikiStore(t)
	sub := &fakeSubmitter{}
	tool := NewWikiPageTool(s, sub)
	if _, err := tool.Execute(context.Background(), map[string]any{
		"action": "create",
		"title":  "Reindex Me",
		"body":   "x",
	}); err != nil {
		t.Fatal(err)
	}
	if sub.calls == 0 {
		t.Fatal("expected reindex submit on successful create")
	}
	if sub.last.Slug != "reindex-me" {
		t.Fatalf("reindex slug = %q", sub.last.Slug)
	}
}
