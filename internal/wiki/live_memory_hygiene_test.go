//go:build live_wiki

package wiki

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLiveCleanMemory is an operator-triggered E2E check for the real wiki.
// It is skipped unless AURA_LIVE_WIKI_PATH is set. Set
// AURA_LIVE_WIKI_APPLY=true to run the same write path used by nightly
// maintenance.
func TestLiveCleanMemory(t *testing.T) {
	wikiPath := strings.TrimSpace(os.Getenv("AURA_LIVE_WIKI_PATH"))
	if wikiPath == "" {
		t.Skip("set AURA_LIVE_WIKI_PATH to run live wiki hygiene")
	}
	store, err := NewStore(wikiPath, nil)
	if err != nil {
		t.Fatalf("NewStore(%q): %v", wikiPath, err)
	}

	apply := strings.EqualFold(strings.TrimSpace(os.Getenv("AURA_LIVE_WIKI_APPLY")), "true")
	report, err := store.CleanMemory(context.Background(), MemoryHygieneOptions{Apply: apply})
	if err != nil {
		t.Fatalf("CleanMemory(apply=%v): %v", apply, err)
	}
	t.Logf("pages=%d broken=%d orphans=%d planned_hubs=%d created_hubs=%d touched=%d repairs=%d",
		report.Pages, len(report.BrokenLinks), len(report.Orphans), len(report.PlannedHubs), len(report.CreatedHubs), len(report.TouchedPages), len(report.RepairedLinks))

	finalReport, err := store.CleanMemory(context.Background(), MemoryHygieneOptions{})
	if err != nil {
		t.Fatalf("final CleanMemory dry-run: %v", err)
	}
	if len(finalReport.BrokenLinks) != 0 {
		t.Fatalf("live wiki still has broken links: %#v", finalReport.BrokenLinks)
	}
	if apply {
		assertNoSourcePreviewSections(t, wikiPath)
	}
}

func assertNoSourcePreviewSections(t *testing.T, wikiPath string) {
	t.Helper()
	entries, err := os.ReadDir(wikiPath)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", wikiPath, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "source-") || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(wikiPath, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		if strings.Contains(string(data), "\n## Preview\n") {
			t.Fatalf("%s still contains a source preview section", entry.Name())
		}
	}
}
