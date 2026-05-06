package wiki

import (
	"context"
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
		Related:  []string{"segnali-di-trading"},
	})
	putMemoryHygienePage(t, store, &Page{
		Title:    "Trading Signal Two",
		Body:     "Second signal.",
		Category: "trading-signals",
		Related:  []string{"segnali-di-trading"},
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
		Related:  []string{"goa-ai-framework"},
	})
	putMemoryHygienePage(t, store, &Page{
		Title:    "Trading Signal One",
		Body:     "First signal.",
		Category: "trading-signals",
		Related:  []string{"segnali-di-trading"},
	})
	putMemoryHygienePage(t, store, &Page{
		Title:    "Trading Signal Two",
		Body:     "Second signal.",
		Category: "trading-signals",
		Related:  []string{"segnali-di-trading"},
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
	if !slices.Contains(golem.Related, "goa-ai-framework-design-first-per-agenti-agentic") {
		t.Fatalf("golem related = %v, want canonical goa slug", golem.Related)
	}
	if slices.Contains(golem.Related, "goa-ai-framework") {
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
