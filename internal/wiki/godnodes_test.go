package wiki

import "testing"

// makeGodNodeIndex populates a fresh GraphIndex from the provided page map.
func makeGodNodeIndex(pages map[string]*Page) *GraphIndex {
	g := NewGraphIndex()
	g.LoadFromPages(pages)
	return g
}

func TestTopByDegree_NilIndex(t *testing.T) {
	var g *GraphIndex
	if got := g.TopByDegree(10); got != nil {
		t.Errorf("nil index: got %v, want nil", got)
	}
}

func TestTopByDegree_EmptyIndex(t *testing.T) {
	g := NewGraphIndex()
	if got := g.TopByDegree(10); got != nil {
		t.Errorf("empty index: got %v, want nil", got)
	}
}

func TestTopByDegree_ZeroTopK(t *testing.T) {
	g := makeGodNodeIndex(map[string]*Page{
		"alpha": {Title: "Alpha"},
	})
	if got := g.TopByDegree(0); got != nil {
		t.Errorf("topK=0: got %v, want nil", got)
	}
	if got := g.TopByDegree(-5); got != nil {
		t.Errorf("topK=-5: got %v, want nil", got)
	}
}

func TestTopByDegree_SingleNode(t *testing.T) {
	g := makeGodNodeIndex(map[string]*Page{
		"solo": {Title: "Solo"},
	})
	got := g.TopByDegree(1)
	if len(got) != 1 {
		t.Fatalf("len=%d, want 1", len(got))
	}
	if got[0].Slug != "solo" {
		t.Errorf("slug=%q, want solo", got[0].Slug)
	}
	if got[0].TotalDegree != 0 {
		t.Errorf("TotalDegree=%d, want 0 (isolated node)", got[0].TotalDegree)
	}
}

// TestTopByDegree_DiamondGraph builds a hub→spokes graph and verifies
// degree ranking. Graph:
//
//	hub body → spoke-a, spoke-b, spoke-c     (outbound 3)
//	spoke-a body → hub                       (outbound 1)
//
// Resulting degrees:
//
//	hub:     in=1 (from spoke-a), out=3, total=4
//	spoke-a: in=1 (from hub),     out=1, total=2
//	spoke-b: in=1 (from hub),     out=0, total=1
//	spoke-c: in=1 (from hub),     out=0, total=1
func TestTopByDegree_DiamondGraph(t *testing.T) {
	pages := map[string]*Page{
		"hub":     {Title: "Hub", Body: "[[spoke-a]] [[spoke-b]] [[spoke-c]]"},
		"spoke-a": {Title: "Spoke A", Body: "[[hub]]"},
		"spoke-b": {Title: "Spoke B"},
		"spoke-c": {Title: "Spoke C"},
	}
	g := makeGodNodeIndex(pages)

	got := g.TopByDegree(4)
	if len(got) != 4 {
		t.Fatalf("len=%d, want 4", len(got))
	}
	if got[0].Slug != "hub" || got[0].TotalDegree != 4 {
		t.Errorf("rank 0: got {%s, total=%d}, want {hub, total=4}", got[0].Slug, got[0].TotalDegree)
	}
	if got[1].Slug != "spoke-a" || got[1].TotalDegree != 2 {
		t.Errorf("rank 1: got {%s, total=%d}, want {spoke-a, total=2}", got[1].Slug, got[1].TotalDegree)
	}
	// spoke-b and spoke-c tie at total=1; alphabetical tiebreak.
	if got[2].Slug != "spoke-b" || got[3].Slug != "spoke-c" {
		t.Errorf("rank 2-3 tiebreak: got %q %q, want spoke-b spoke-c", got[2].Slug, got[3].Slug)
	}
}

// TestTopByDegree_TieBreak verifies alphabetical slug ordering when all
// degrees are equal.
func TestTopByDegree_TieBreak(t *testing.T) {
	pages := map[string]*Page{
		"charlie": {Title: "Charlie"},
		"alice":   {Title: "Alice"},
		"bob":     {Title: "Bob"},
	}
	g := makeGodNodeIndex(pages)
	got := g.TopByDegree(3)
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3", len(got))
	}
	want := []string{"alice", "bob", "charlie"}
	for i, w := range want {
		if got[i].Slug != w {
			t.Errorf("rank %d: got %q, want %q", i, got[i].Slug, w)
		}
	}
}

// TestTopByDegree_TopKClamping verifies topK bounds.
func TestTopByDegree_TopKClamping(t *testing.T) {
	pages := map[string]*Page{
		"a": {Title: "A"},
		"b": {Title: "B"},
	}
	g := makeGodNodeIndex(pages)

	// topK > nodeCount → return all
	if got := g.TopByDegree(100); len(got) != 2 {
		t.Errorf("topK=100 (>nodeCount): len=%d, want 2", len(got))
	}
	// topK == nodeCount
	if got := g.TopByDegree(2); len(got) != 2 {
		t.Errorf("topK=nodeCount: len=%d, want 2", len(got))
	}
	// topK < nodeCount
	if got := g.TopByDegree(1); len(got) != 1 {
		t.Errorf("topK=1 (<nodeCount): len=%d, want 1", len(got))
	}
}

// TestIsAuxiliaryHubNode covers the four classes the filter must reject
// and the two it must accept (real entities + categorised concept pages
// without the hub tag).
func TestIsAuxiliaryHubNode(t *testing.T) {
	cases := []struct {
		name string
		slug string
		meta NodeMeta
		want bool
	}{
		{"operational index", "index", NodeMeta{Title: "Wiki Index"}, true},
		{"operational log", "log", NodeMeta{Title: "Log"}, true},
		{"operational schema", "schema", NodeMeta{Title: "Schema"}, true},
		{"category uncategorized", "uncategorized", NodeMeta{Title: "Uncategorized", Category: "uncategorized"}, true},
		{"tag hub on real slug", "design-system", NodeMeta{Title: "Design System", Tags: []string{"hub", "design"}}, true},
		{"empty title placeholder", "ghost", NodeMeta{}, true},
		{"real entity person", "davide-marchetto", NodeMeta{Title: "Davide Marchetto", Category: "entity"}, false},
		{"concept page", "robot", NodeMeta{Title: "Robot", Category: "concept"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsAuxiliaryHubNode(tc.slug, tc.meta); got != tc.want {
				t.Errorf("IsAuxiliaryHubNode(%q, %+v) = %v, want %v", tc.slug, tc.meta, got, tc.want)
			}
		})
	}
}

// TestTopByDegreeFiltered_SkipsAuxiliaryHubs builds a graph where the
// auto-generated hubs are the highest-degree nodes (the realistic shape:
// index.md links every page, uncategorized.md links every orphan) and
// asserts the filter surfaces real entities instead.
func TestTopByDegreeFiltered_SkipsAuxiliaryHubs(t *testing.T) {
	pages := map[string]*Page{
		"index": {
			Title: "Wiki Index",
			Body:  "[[davide-marchetto]] [[robot]] [[calandra]] [[albatech]]",
		},
		"uncategorized": {
			Title:    "Uncategorized",
			Category: "uncategorized",
			Body:     "[[davide-marchetto]] [[robot]]",
		},
		"davide-marchetto": {
			Title:    "Davide Marchetto",
			Category: "entity",
			Body:     "Person profile. Linked to [[robot]] and [[albatech]].",
		},
		"robot":    {Title: "Robot", Category: "concept", Body: "[[calandra]]"},
		"calandra": {Title: "Calandra", Category: "concept"},
		"albatech": {Title: "Albatech", Category: "entity"},
	}
	g := makeGodNodeIndex(pages)

	// Raw view includes index + uncategorized hubs.
	raw := g.TopByDegree(10)
	if len(raw) != 6 {
		t.Fatalf("raw len=%d, want 6 (all nodes)", len(raw))
	}
	rawTop := raw[0].Slug
	if rawTop != "index" && rawTop != "davide-marchetto" {
		t.Errorf("raw top=%q, want one of [index, davide-marchetto] (highest degree)", rawTop)
	}

	// Filtered view must drop both hubs.
	filtered := g.TopByDegreeFiltered(10, IsAuxiliaryHubNode)
	for _, n := range filtered {
		if n.Slug == "index" || n.Slug == "uncategorized" {
			t.Errorf("filter leaked hub %q: %+v", n.Slug, filtered)
		}
	}
	if len(filtered) != 4 {
		t.Fatalf("filtered len=%d, want 4 (real entities only)", len(filtered))
	}
	// davide-marchetto inbound: index, uncategorized, davide body → 3
	// davide-marchetto outbound: robot, albatech → 2 → total 5, top.
	if filtered[0].Slug != "davide-marchetto" {
		t.Errorf("filtered top=%q, want davide-marchetto", filtered[0].Slug)
	}
}

// TestTopByDegreeFiltered_FullWindowAfterFilter asserts that the topK
// trim happens AFTER skip so the caller still receives topK real hits
// when the graph contains many auxiliary hubs.
func TestTopByDegreeFiltered_FullWindowAfterFilter(t *testing.T) {
	pages := map[string]*Page{
		"index":         {Title: "Wiki Index", Body: "[[a]] [[b]] [[c]] [[d]] [[e]]"},
		"uncategorized": {Title: "Uncategorized", Category: "uncategorized", Body: "[[a]]"},
		"hub-design":    {Title: "Design Hub", Tags: []string{"hub"}, Body: "[[a]] [[b]]"},
		"a":             {Title: "A", Category: "entity"},
		"b":             {Title: "B", Category: "entity"},
		"c":             {Title: "C", Category: "entity"},
		"d":             {Title: "D", Category: "entity"},
		"e":             {Title: "E", Category: "entity"},
	}
	g := makeGodNodeIndex(pages)
	filtered := g.TopByDegreeFiltered(3, IsAuxiliaryHubNode)
	if len(filtered) != 3 {
		t.Fatalf("filtered len=%d, want 3 (full window after filter)", len(filtered))
	}
	for _, n := range filtered {
		if IsAuxiliaryHubNode(n.Slug, NodeMeta{Slug: n.Slug, Title: n.Title, Category: n.Category}) {
			t.Errorf("auxiliary hub %q leaked into filtered top-K", n.Slug)
		}
	}
}

// TestTopByDegreeFiltered_NilSkipFallsBack asserts that passing nil skip
// is equivalent to the raw TopByDegree view.
func TestTopByDegreeFiltered_NilSkipFallsBack(t *testing.T) {
	pages := map[string]*Page{
		"index": {Title: "Wiki Index", Body: "[[a]]"},
		"a":     {Title: "A"},
	}
	g := makeGodNodeIndex(pages)
	raw := g.TopByDegree(10)
	viaFiltered := g.TopByDegreeFiltered(10, nil)
	if len(raw) != len(viaFiltered) {
		t.Fatalf("nil skip len mismatch: raw=%d filtered=%d", len(raw), len(viaFiltered))
	}
	for i := range raw {
		if raw[i].Slug != viaFiltered[i].Slug {
			t.Errorf("nil skip rank %d: raw=%q filtered=%q", i, raw[i].Slug, viaFiltered[i].Slug)
		}
	}
}
