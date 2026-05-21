package wiki

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func putMemoryHygienePage(t *testing.T, store *Store, page *Page) {
	t.Helper()
	if page.SchemaVersion == 0 {
		page.SchemaVersion = CurrentSchemaVersion
	}
	if page.PromptVersion == "" {
		page.PromptVersion = "v1"
	}
	if page.CreatedAt == "" {
		page.CreatedAt = "2026-05-06T00:00:00Z"
	}
	if page.UpdatedAt == "" {
		page.UpdatedAt = "2026-05-06T00:00:00Z"
	}
	if err := store.WritePage(context.Background(), page); err != nil {
		t.Fatalf("WritePage(%q): %v", page.Title, err)
	}
}

func TestCleanMemoryDryRunPlansSharedBrokenHubWithoutWriting(t *testing.T) {
	store, dir := newTestStore(t)
	putMemoryHygienePage(t, store, &Page{
		Title:    "Trading Signal One",
		Body:     "First signal.",
		Category: "trading-signals",
		Related:  RelatedFromSlugs([]string{"segnali-di-trading"}),
	})
	putMemoryHygienePage(t, store, &Page{
		Title:    "Trading Signal Two",
		Body:     "Second signal.",
		Category: "trading-signals",
		Related:  RelatedFromSlugs([]string{"segnali-di-trading"}),
	})

	report, err := store.CleanMemory(context.Background(), MemoryHygieneOptions{})
	if err != nil {
		t.Fatalf("CleanMemory dry-run: %v", err)
	}

	if !slices.Contains(report.PlannedHubs, "segnali-di-trading") {
		t.Fatalf("PlannedHubs = %v, want segnali-di-trading", report.PlannedHubs)
	}
	if len(report.BrokenLinks) != 2 {
		t.Fatalf("BrokenLinks = %d, want 2", len(report.BrokenLinks))
	}
	if _, err := os.Stat(filepath.Join(dir, "segnali-di-trading.md")); err == nil {
		t.Fatal("dry-run wrote segnali-di-trading.md")
	}
}

func TestCleanMemoryPlansSharedBrokenHubAcrossCategories(t *testing.T) {
	store, _ := newTestStore(t)
	putMemoryHygienePage(t, store, &Page{
		Title:    "Installed Skills",
		Body:     "Skill inventory.",
		Category: "engineering",
		Related:  RelatedFromSlugs([]string{"aura-source-extraction"}),
	})
	putMemoryHygienePage(t, store, &Page{
		Title:    "Source Note",
		Body:     "Extracted source summary.",
		Category: "sources",
		Related:  RelatedFromSlugs([]string{"aura-source-extraction"}),
	})

	report, err := store.CleanMemory(context.Background(), MemoryHygieneOptions{})
	if err != nil {
		t.Fatalf("CleanMemory dry-run: %v", err)
	}
	if !slices.Contains(report.PlannedHubs, "aura-source-extraction") {
		t.Fatalf("PlannedHubs = %v, want aura-source-extraction", report.PlannedHubs)
	}
}

func TestCleanMemoryApplyCreatesHubsRepairsAliasesAndLeavesNoBrokenLinks(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	putMemoryHygienePage(t, store, &Page{
		Title:    "GOA AI Framework Design First per Agenti Agentic",
		Body:     "Design notes.",
		Category: "engineering",
	})
	putMemoryHygienePage(t, store, &Page{
		Title:    "Golang e agenti AI personali interessi e panoramica",
		Body:     "Personal agent interests.",
		Category: "engineering",
	})
	putMemoryHygienePage(t, store, &Page{
		Title:    "Golem agente AI personale in Go",
		Body:     "Uses [[golang-agenti-ai-personali]] as context.",
		Category: "engineering",
		Related:  RelatedFromSlugs([]string{"goa-ai-framework"}),
	})
	putMemoryHygienePage(t, store, &Page{
		Title:    "Trading Signal One",
		Body:     "First signal.",
		Category: "trading-signals",
		Related:  RelatedFromSlugs([]string{"segnali-di-trading"}),
	})
	putMemoryHygienePage(t, store, &Page{
		Title:    "Trading Signal Two",
		Body:     "Second signal.",
		Category: "trading-signals",
		Related:  RelatedFromSlugs([]string{"segnali-di-trading"}),
	})

	report, err := store.CleanMemory(ctx, MemoryHygieneOptions{Apply: true})
	if err != nil {
		t.Fatalf("CleanMemory apply: %v", err)
	}
	if !slices.Contains(report.CreatedHubs, "segnali-di-trading") {
		t.Fatalf("CreatedHubs = %v, want segnali-di-trading", report.CreatedHubs)
	}
	if len(report.RepairedLinks) < 2 {
		t.Fatalf("RepairedLinks = %v, want alias repairs", report.RepairedLinks)
	}

	golem, err := store.ReadPage("golem-agente-ai-personale-in-go")
	if err != nil {
		t.Fatalf("ReadPage(golem): %v", err)
	}
	if !RelatedContainsSlug(golem.Related, "goa-ai-framework-design-first-per-agenti-agentic") {
		t.Fatalf("golem related = %v, want canonical goa slug", golem.Related)
	}
	if RelatedContainsSlug(golem.Related, "goa-ai-framework") {
		t.Fatalf("golem related still has broken alias: %v", golem.Related)
	}
	if want := "[[golang-e-agenti-ai-personali-interessi-e-panoramica]]"; !strings.Contains(golem.Body, want) {
		t.Fatalf("golem body = %q, want %s", golem.Body, want)
	}

	hub, err := store.ReadPage("segnali-di-trading")
	if err != nil {
		t.Fatalf("ReadPage(segnali-di-trading): %v", err)
	}
	if hub.Category != "trading-signals" {
		t.Fatalf("hub category = %q, want trading-signals", hub.Category)
	}
	if !strings.Contains(hub.Body, "[[trading-signal-one]]") || !strings.Contains(hub.Body, "[[trading-signal-two]]") {
		t.Fatalf("hub body missing page links: %q", hub.Body)
	}

	finalReport, err := store.CleanMemory(ctx, MemoryHygieneOptions{})
	if err != nil {
		t.Fatalf("CleanMemory final dry-run: %v", err)
	}
	if len(finalReport.BrokenLinks) != 0 {
		t.Fatalf("BrokenLinks after apply = %#v, want none", finalReport.BrokenLinks)
	}
}

func TestCleanMemoryApplyStripsAutoGeneratedSourcePreview(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	putMemoryHygienePage(t, store, &Page{
		Title:    "Source: paper",
		Category: "sources",
		Tags:     []string{"source", "pdf"},
		Sources:  []string{"source:src_1234567890abcdef"},
		Body: "# Source: paper.pdf\n\n" +
			"Auto-generated by the ingest pipeline. Edit freely; rerunning ingest_source on this source will refresh this page.\n\n" +
			"## Metadata\n\n- Source ID: `src_1234567890abcdef`\n\n" +
			"## Extracted Markdown\n\nFull extracted markdown lives at `wiki/raw/src_1234567890abcdef/ocr.md` (read via `source(action=\"read\")`).\n\n" +
			"## Preview\n\nThis is raw OCR content that should stay in source(action=\"read\"), not wiki embeddings.\n",
	})

	report, err := store.CleanMemory(ctx, MemoryHygieneOptions{Apply: true})
	if err != nil {
		t.Fatalf("CleanMemory apply: %v", err)
	}
	if !slices.Contains(report.TouchedPages, "source-paper") {
		t.Fatalf("TouchedPages = %v, want source-paper", report.TouchedPages)
	}
	page, err := store.ReadPage("source-paper")
	if err != nil {
		t.Fatalf("ReadPage(source-paper): %v", err)
	}
	if strings.Contains(page.Body, "## Preview") || strings.Contains(page.Body, "raw OCR content") {
		t.Fatalf("preview not stripped:\n%s", page.Body)
	}
	if !strings.Contains(page.Body, `source(action="read")`) {
		t.Fatalf("source pointer should remain:\n%s", page.Body)
	}
}

func TestCleanMemoryApplyRenamesOpaqueSourceSlugAndUpdatesReferences(t *testing.T) {
	store, dir := newTestStore(t)
	ctx := context.Background()
	sourceID := "src_1234567890abcdef"
	oldSlug := "source-4-5942613039617418204"
	newSlug := "source-test-1-verifica-operativa-gestione-richiesta-offerta-cliente-pms"

	rawDir := filepath.Join(dir, "raw", sourceID)
	if err := os.MkdirAll(rawDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "ocr.md"), []byte("# Source OCR: 4_5942613039617418204.pdf\n\n## Page 1\n\n# TEST 1 - Verifica operativa gestione richiesta offerta cliente (PMS)\n\nBody."), 0644); err != nil {
		t.Fatal(err)
	}
	sourceMeta := map[string]any{
		"id":         sourceID,
		"filename":   "4_5942613039617418204.pdf",
		"wiki_pages": []string{oldSlug},
	}
	metaBytes, err := json.MarshalIndent(sourceMeta, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "source.json"), metaBytes, 0644); err != nil {
		t.Fatal(err)
	}

	putMemoryHygienePage(t, store, &Page{
		Title:    "Source: 4_5942613039617418204",
		Category: "sources",
		Tags:     []string{"source", "pdf"},
		Sources:  []string{"source:" + sourceID},
		Body: "# Source: 4_5942613039617418204.pdf\n\n" +
			"Auto-generated by the ingest pipeline.\n\n" +
			"## Extracted Markdown\n\nFull extracted markdown lives at `wiki/raw/src_1234567890abcdef/ocr.md`.\n",
	})
	putMemoryHygienePage(t, store, &Page{
		Title:    "PMS Project Notes",
		Category: "projects",
		Related:  RelatedFromSlugs([]string{oldSlug}),
		Body:     "Evidence is in [[" + oldSlug + "]].",
	})

	report, err := store.CleanMemory(ctx, MemoryHygieneOptions{Apply: true})
	if err != nil {
		t.Fatalf("CleanMemory apply: %v", err)
	}
	if len(report.RenamedPages) != 1 {
		t.Fatalf("RenamedPages = %#v, want one rename", report.RenamedPages)
	}
	if got := report.RenamedPages[0]; got.From != oldSlug || got.To != newSlug {
		t.Fatalf("rename = %#v, want %s -> %s", got, oldSlug, newSlug)
	}
	if _, err := os.Stat(filepath.Join(dir, oldSlug+".md")); !os.IsNotExist(err) {
		t.Fatalf("old source page still exists or stat failed: %v", err)
	}
	if _, err := store.ReadPage(newSlug); err != nil {
		t.Fatalf("ReadPage(new slug): %v", err)
	}

	project, err := store.ReadPage("pms-project-notes")
	if err != nil {
		t.Fatalf("ReadPage(project): %v", err)
	}
	if strings.Contains(project.Body, oldSlug) || RelatedContainsSlug(project.Related, oldSlug) {
		t.Fatalf("project still references old slug: body=%q related=%v", project.Body, project.Related)
	}
	if !strings.Contains(project.Body, "[["+newSlug+"]]") || !RelatedContainsSlug(project.Related, newSlug) {
		t.Fatalf("project missing new slug: body=%q related=%v", project.Body, project.Related)
	}

	updatedMeta, err := os.ReadFile(filepath.Join(rawDir, "source.json"))
	if err != nil {
		t.Fatal(err)
	}
	var gotMeta struct {
		WikiPages []string `json:"wiki_pages"`
	}
	if err := json.Unmarshal(updatedMeta, &gotMeta); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(gotMeta.WikiPages, []string{newSlug}) {
		t.Fatalf("wiki_pages = %v, want [%s]", gotMeta.WikiPages, newSlug)
	}
}

func TestCleanMemoryApplyNoopDoesNotAppendLog(t *testing.T) {
	store, dir := newTestStore(t)
	ctx := context.Background()
	putMemoryHygienePage(t, store, &Page{
		Title:    "Stable Memory",
		Body:     "Already clean.",
		Category: "notes",
	})
	before, err := os.ReadFile(filepath.Join(dir, "log.md"))
	if err != nil {
		t.Fatalf("read log before: %v", err)
	}

	report, err := store.CleanMemory(ctx, MemoryHygieneOptions{Apply: true})
	if err != nil {
		t.Fatalf("CleanMemory apply: %v", err)
	}
	if len(report.RenamedPages) != 0 || len(report.RepairedLinks) != 0 || len(report.CreatedHubs) != 0 || len(report.TouchedPages) != 0 {
		t.Fatalf("CleanMemory changed clean wiki: %#v", report)
	}
	after, err := os.ReadFile(filepath.Join(dir, "log.md"))
	if err != nil {
		t.Fatalf("read log after: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("log changed on no-op apply:\nbefore=%s\nafter=%s", before, after)
	}
}
