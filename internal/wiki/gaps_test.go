package wiki

import "testing"

func TestKnowledgeGaps_ReturnsIsolatedPageFromDiamondGraph(t *testing.T) {
	g := makeGodNodeIndex(map[string]*Page{
		"hub":    {Title: "Hub", Category: "concept", Body: "[[left]] [[right]]"},
		"left":   {Title: "Left", Category: "concept", Body: "[[hub]] [[right]]"},
		"right":  {Title: "Right", Category: "concept", Body: "[[hub]] [[left]]"},
		"island": {Title: "Island", Category: "concept", Body: "No links yet."},
	})

	got := g.KnowledgeGaps()
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1: %#v", len(got), got)
	}
	if got[0].Slug != "island" || got[0].TotalDegree != 0 {
		t.Fatalf("gap = %+v, want island with total degree 0", got[0])
	}
}

func TestKnowledgeGaps_IncludesLeafAndSkipsAuxiliaryNodes(t *testing.T) {
	g := makeGodNodeIndex(map[string]*Page{
		"index": {
			Title: "Wiki Index",
			Body:  "Operational page.",
		},
		"uncategorized": {
			Title:    "Uncategorized",
			Category: "uncategorized",
			Body:     "Uncategorized bucket.",
		},
		"tagged-hub": {
			Title: "Tagged Hub",
			Tags:  []string{"hub"},
			Body:  "Explicit hub marker.",
		},
		"empty-title": {
			Body: "Placeholder.",
		},
		"leaf": {
			Title:    "Leaf",
			Category: "concept",
			Body:     "Mentions [[anchor]].",
		},
		"anchor": {
			Title:    "Anchor",
			Category: "concept",
			Body:     "[[other]] [[third]]",
		},
		"other": {
			Title:    "Other",
			Category: "concept",
			Body:     "[[anchor]]",
		},
		"third": {
			Title:    "Third",
			Category: "concept",
			Body:     "[[anchor]]",
		},
	})

	got := g.KnowledgeGaps()
	want := []string{"leaf"}
	if len(got) != len(want) {
		t.Fatalf("len=%d, want %d: %#v", len(got), len(want), got)
	}
	for i, slug := range want {
		if got[i].Slug != slug {
			t.Fatalf("rank %d slug=%q, want %q: %#v", i, got[i].Slug, slug, got)
		}
		if got[i].TotalDegree != 1 {
			t.Fatalf("%s total degree=%d, want 1", got[i].Slug, got[i].TotalDegree)
		}
	}
}
