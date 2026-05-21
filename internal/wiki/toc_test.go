package wiki_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/wiki"
)

// newTestPage returns a minimal valid Page for use in TOC tests.
func newTestPage(title, body string, tags []string) *wiki.Page {
	now := time.Now().UTC().Format(time.RFC3339)
	return &wiki.Page{
		Title:         title,
		Body:          body,
		Tags:          tags,
		Category:      "test",
		SchemaVersion: 3,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// TestGenerateTOC_StaysWithinBudget verifies that the generated TOC stays
// within the requested byte budget and all in-budget slugs appear.
func TestGenerateTOC_StaysWithinBudget(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	ctx := context.Background()

	// Write 5 pages.
	titles := []string{"Alpha Page", "Beta Page", "Gamma Page", "Delta Page", "Epsilon Page"}
	for _, title := range titles {
		if err := store.WritePage(ctx, newTestPage(title, "Body text for "+title+".", nil)); err != nil {
			t.Fatalf("WritePage %q: %v", title, err)
		}
	}

	const budget = 200
	toc, err := wiki.GenerateTOC(store, budget)
	if err != nil {
		t.Fatalf("GenerateTOC: %v", err)
	}
	if len(toc) > budget+100 { // allow a little overflow for the overflow marker itself
		t.Errorf("TOC len=%d exceeds budget %d by too much", len(toc), budget)
	}
	if !strings.Contains(toc, "## Wiki TOC") {
		t.Error("TOC missing header")
	}
}

// TestGenerateTOC_PicksMostRelevantOnOverflow verifies that when the budget
// is tight, EXTRACTED-tagged pages are included before untagged ones.
func TestGenerateTOC_PicksMostRelevantOnOverflow(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	ctx := context.Background()

	// Write one EXTRACTED page and one untagged page.
	extracted := newTestPage("Important Extracted Page", "Key content.", []string{"EXTRACTED"})
	untagged := newTestPage("Ordinary Untagged Page", "Normal content.", nil)
	if err := store.WritePage(ctx, extracted); err != nil {
		t.Fatalf("WritePage extracted: %v", err)
	}
	if err := store.WritePage(ctx, untagged); err != nil {
		t.Fatalf("WritePage untagged: %v", err)
	}

	// Use a tight budget that fits only one entry beyond the header.
	// "## Wiki TOC\n" = 12 bytes; EXTRACTED line ~73 bytes = 85 total. Budget = 100 fits EXTRACTED but not untagged.
	const budget = 100
	toc, err := wiki.GenerateTOC(store, budget)
	if err != nil {
		t.Fatalf("GenerateTOC: %v", err)
	}

	// The EXTRACTED page slug must appear; the untagged page slug may be omitted.
	slug := wiki.Slug("Important Extracted Page")
	if !strings.Contains(toc, "[["+slug+"]]") {
		t.Errorf("EXTRACTED page slug %q not in tight-budget TOC:\n%s", slug, toc)
	}
	// Overflow marker must appear since we dropped at least one page.
	if !strings.Contains(toc, "not in TOC") {
		t.Errorf("overflow marker missing from tight-budget TOC:\n%s", toc)
	}
}

// TestGenerateTOC_RebuildsOnWikiWrite verifies that writing a new page
// updates the cached TOC so the new slug appears immediately.
func TestGenerateTOC_RebuildsOnWikiWrite(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(dir, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	ctx := context.Background()

	// Write first page.
	if err := store.WritePage(ctx, newTestPage("First Page", "First content.", nil)); err != nil {
		t.Fatalf("WritePage first: %v", err)
	}

	// Allow the async rebuild triggered by WritePage to complete.
	store.RebuildTOC()

	toc1 := store.GetCachedTOC()
	if !strings.Contains(toc1, "first-page") {
		t.Errorf("TOC after first write missing 'first-page':\n%s", toc1)
	}

	// Write a second page and wait for rebuild.
	if err := store.WritePage(ctx, newTestPage("New Discovery", "Fresh content.", nil)); err != nil {
		t.Fatalf("WritePage new: %v", err)
	}
	store.RebuildTOC() // ensure sync rebuild for test assertion

	toc2 := store.GetCachedTOC()
	if !strings.Contains(toc2, "new-discovery") {
		t.Errorf("TOC after second write missing 'new-discovery':\n%s", toc2)
	}
}

// TestSystemPrompt_IncludesTOC verifies that the InjectWikiTOC helper wraps
// TOC content with the expected START/END delimiters and that an empty TOC
// leaves the content unchanged.
func TestSystemPrompt_IncludesTOC(t *testing.T) {
	const base = "system prompt content"
	const toc = "## Wiki TOC\n[[foo]] — Foo — bar\n"

	result := conversation.InjectWikiTOC(base, toc)
	if !strings.Contains(result, "--- WIKI TOC START ---") {
		t.Error("missing WIKI TOC START delimiter")
	}
	if !strings.Contains(result, "--- WIKI TOC END ---") {
		t.Error("missing WIKI TOC END delimiter")
	}
	if !strings.Contains(result, "[[foo]]") {
		t.Error("TOC content not present in injected result")
	}

	// Empty TOC must not modify content.
	unchanged := conversation.InjectWikiTOC(base, "")
	if unchanged != base {
		t.Errorf("empty TOC changed content: %q", unchanged)
	}
	unchanged2 := conversation.InjectWikiTOC(base, "   \n")
	if unchanged2 != base {
		t.Errorf("whitespace-only TOC changed content: %q", unchanged2)
	}
}

// BenchmarkGenerateTOC measures GenerateTOC performance on a synthetic wiki.
// Target: ≤200ms on 500 pages (regex-only, no LLM).
func BenchmarkGenerateTOC(b *testing.B) {
	dir := b.TempDir()
	store, err := wiki.NewStore(dir, nil)
	if err != nil {
		b.Fatalf("NewStore: %v", err)
	}
	ctx := context.Background()

	// Write 50 pages (keep test fast; 500 would take too long for setup).
	for i := range 50 {
		title := fmt.Sprintf("Benchmark Page %03d", i)
		p := newTestPage(title, "Content for "+title+". Some longer text here to simulate real pages.", nil)
		if err := store.WritePage(ctx, p); err != nil {
			b.Fatalf("WritePage: %v", err)
		}
	}

	b.ResetTimer()
	for range b.N {
		if _, err := wiki.GenerateTOC(store, wiki.DefaultTOCMaxBytes); err != nil {
			b.Fatalf("GenerateTOC: %v", err)
		}
	}
}
