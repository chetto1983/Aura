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
