package wiki

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestSurprisingEdges_ConfidenceWeighting(t *testing.T) {
	pages := map[string]*Page{}
	addSurprisePair(pages, "low", "topic", "topic", ConfidenceAmbiguous)
	addSurprisePair(pages, "high", "topic", "topic", ConfidenceExtracted)
	store := newSurpriseTestStore(t, pages)

	low := findSurprise(t, store.SurprisingEdges(10), "low-a", "low-b")
	high := findSurprise(t, store.SurprisingEdges(10), "high-a", "high-b")
	if low.Score <= high.Score {
		t.Fatalf("ambiguous score = %d, extracted score = %d; want ambiguous higher", low.Score, high.Score)
	}
	if low.Confidence != ConfidenceAmbiguous {
		t.Fatalf("confidence = %q, want %q", low.Confidence, ConfidenceAmbiguous)
	}
}

func TestSurprisingEdges_CategoryCrossingBonus(t *testing.T) {
	pages := map[string]*Page{}
	addSurprisePair(pages, "cross", "person", "project", ConfidenceExtracted)
	addSurprisePair(pages, "same", "person", "person", ConfidenceExtracted)
	store := newSurpriseTestStore(t, pages)

	cross := findSurprise(t, store.SurprisingEdges(10), "cross-a", "cross-b")
	same := findSurprise(t, store.SurprisingEdges(10), "same-a", "same-b")
	if cross.Score <= same.Score {
		t.Fatalf("cross score = %d, same score = %d; want cross-category higher", cross.Score, same.Score)
	}
	if !strings.Contains(cross.Why, "crosses categories person->project") {
		t.Fatalf("why = %q, want category crossing reason", cross.Why)
	}
}

func TestSurprisingEdges_PeripheralToHubBonus(t *testing.T) {
	pages := map[string]*Page{
		"hub":       {Title: "Hub", Category: "topic", Body: "Hub."},
		"control-a": {Title: "Control A", Category: "topic", Body: "Control A.", Related: []RelatedRef{{Slug: "control-b", Confidence: ConfidenceExtracted}}},
		"control-b": {Title: "Control B", Category: "topic", Body: "Control B."},
	}
	for i := 1; i <= 5; i++ {
		slug := fmt.Sprintf("spoke-%d", i)
		pages[slug] = &Page{Title: fmt.Sprintf("Spoke %d", i), Category: "topic", Body: "Spoke.", Related: []RelatedRef{{Slug: "hub", Confidence: ConfidenceExtracted}}}
	}
	store := newSurpriseTestStore(t, pages)

	hub := findSurprise(t, store.SurprisingEdges(10), "spoke-1", "hub")
	control := findSurprise(t, store.SurprisingEdges(10), "control-a", "control-b")
	if hub.Score <= control.Score {
		t.Fatalf("hub score = %d, control score = %d; want peripheral-to-hub higher", hub.Score, control.Score)
	}
	if !strings.Contains(hub.Why, "peripheral-to-hub") {
		t.Fatalf("why = %q, want peripheral-to-hub reason", hub.Why)
	}
}

func TestSurprisingEdges_DedupByPair(t *testing.T) {
	store := newSurpriseTestStore(t, map[string]*Page{
		"alpha": {Title: "Alpha", Category: "topic", Body: "Alpha links [[beta]]."},
		"beta":  {Title: "Beta", Category: "topic", Body: "Beta links [[alpha]]."},
	})

	var count int
	for _, item := range store.SurprisingEdges(10) {
		if surprisePairKey(item.SrcSlug, item.TgtSlug) == surprisePairKey("alpha", "beta") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("alpha/beta surprise count = %d, want 1", count)
	}
}

func addSurprisePair(pages map[string]*Page, prefix, srcCategory, tgtCategory, confidence string) {
	source := prefix + "-a"
	target := prefix + "-b"
	pages[source] = &Page{
		Title:    source,
		Category: srcCategory,
		Body:     source + ".",
		Related:  []RelatedRef{{Slug: target, Confidence: confidence}},
	}
	pages[target] = &Page{
		Title:    target,
		Category: tgtCategory,
		Body:     target + ".",
	}
}

func newSurpriseTestStore(t *testing.T, pages map[string]*Page) *Store {
	t.Helper()
	store, err := NewStore(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for _, page := range pages {
		page.SchemaVersion = CurrentSchemaVersion
		page.PromptVersion = "v1"
		page.CreatedAt = "2026-05-25T00:00:00Z"
		page.UpdatedAt = "2026-05-25T00:00:00Z"
		if err := store.WritePage(context.Background(), page); err != nil {
			t.Fatalf("WritePage(%s): %v", page.Title, err)
		}
	}
	store.RebuildGraph(context.Background())
	return store
}

func findSurprise(t *testing.T, items []Surprise, source, target string) Surprise {
	t.Helper()
	for _, item := range items {
		if item.SrcSlug == source && item.TgtSlug == target {
			return item
		}
	}
	t.Fatalf("missing surprise %s -> %s in %#v", source, target, items)
	return Surprise{}
}
