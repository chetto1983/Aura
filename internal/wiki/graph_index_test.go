package wiki

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"
)

// makeTestPage is a small ergonomic helper for graph index tests.
func makeTestPage(title, body string, related []string) *Page {
	now := "2026-05-12T12:00:00Z"
	return &Page{
		Title:         title,
		Body:          body,
		Related:       related,
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

func TestGraphIndex_LoadFromPagesBuildsBidirectionalAdjacency(t *testing.T) {
	g := NewGraphIndex()
	g.LoadFromPages(map[string]*Page{
		"alpha": makeTestPage("Alpha", "Mentions [[beta]] and [[gamma]].", nil),
		"beta":  makeTestPage("Beta", "Refers to [[alpha]].", nil),
		"gamma": makeTestPage("Gamma", "Standalone.", nil),
	})
	if got := g.OutNeighbors("alpha"); !slices.Equal(got, []string{"beta", "gamma"}) {
		t.Fatalf("alpha out = %v, want [beta gamma]", got)
	}
	if got := g.InNeighbors("alpha"); !slices.Equal(got, []string{"beta"}) {
		t.Fatalf("alpha in = %v, want [beta]", got)
	}
	if got := g.InNeighbors("gamma"); !slices.Equal(got, []string{"alpha"}) {
		t.Fatalf("gamma in = %v, want [alpha]", got)
	}
	if !g.HasNode("alpha") || !g.HasNode("gamma") {
		t.Fatal("HasNode missed an indexed node")
	}
	if g.HasNode("nope") {
		t.Fatal("HasNode returned true for unindexed slug")
	}
}

func TestGraphIndex_NeighborsTraversesBothDirectionsToDepth(t *testing.T) {
	// Topology:
	//   alpha -> beta -> gamma -> delta
	//   epsilon -> alpha
	// Neighbors("alpha", 1) = {beta, epsilon} (out + in 1 hop)
	// Neighbors("alpha", 2) = {beta, gamma, epsilon}
	// Neighbors("alpha", 3) = {beta, gamma, delta, epsilon}
	g := NewGraphIndex()
	g.LoadFromPages(map[string]*Page{
		"alpha":   makeTestPage("Alpha", "[[beta]]", nil),
		"beta":    makeTestPage("Beta", "[[gamma]]", nil),
		"gamma":   makeTestPage("Gamma", "[[delta]]", nil),
		"delta":   makeTestPage("Delta", "leaf", nil),
		"epsilon": makeTestPage("Epsilon", "[[alpha]]", nil),
	})
	cases := []struct {
		depth int
		want  []string
	}{
		{1, []string{"beta", "epsilon"}},
		{2, []string{"beta", "epsilon", "gamma"}},
		{3, []string{"beta", "delta", "epsilon", "gamma"}},
	}
	for _, tc := range cases {
		got := g.Neighbors("alpha", tc.depth)
		if !slices.Equal(got, tc.want) {
			t.Errorf("depth=%d: got %v, want %v", tc.depth, got, tc.want)
		}
	}
}

func TestGraphIndex_NeighborsDepthZeroReturnsNil(t *testing.T) {
	g := NewGraphIndex()
	g.LoadFromPages(map[string]*Page{
		"alpha": makeTestPage("Alpha", "[[beta]]", nil),
	})
	if got := g.Neighbors("alpha", 0); got != nil {
		t.Fatalf("depth=0 should return nil, got %v", got)
	}
}

func TestGraphIndex_RefreshPagePrunesStaleEdges(t *testing.T) {
	// alpha initially links to beta. After refresh with body that links
	// to gamma instead, alpha's outbound MUST drop beta and acquire
	// gamma. beta's inbound MUST drop alpha.
	g := NewGraphIndex()
	g.LoadFromPages(map[string]*Page{
		"alpha": makeTestPage("Alpha", "[[beta]]", nil),
		"beta":  makeTestPage("Beta", "leaf", nil),
		"gamma": makeTestPage("Gamma", "leaf", nil),
	})
	if got := g.OutNeighbors("alpha"); !slices.Equal(got, []string{"beta"}) {
		t.Fatalf("initial alpha out = %v, want [beta]", got)
	}

	g.RefreshPage("alpha", makeTestPage("Alpha", "[[gamma]]", nil))
	if got := g.OutNeighbors("alpha"); !slices.Equal(got, []string{"gamma"}) {
		t.Fatalf("after refresh alpha out = %v, want [gamma]", got)
	}
	if got := g.InNeighbors("beta"); len(got) != 0 {
		t.Fatalf("beta still has stale inbound from alpha: %v", got)
	}
	if got := g.InNeighbors("gamma"); !slices.Equal(got, []string{"alpha"}) {
		t.Fatalf("gamma inbound after refresh = %v, want [alpha]", got)
	}
}

func TestGraphIndex_RemoveNodeDropsEdgesBothWays(t *testing.T) {
	// alpha -> beta -> gamma. Remove beta. Expect:
	//   alpha.outbound still has beta as a *broken* target? No — we drop it
	//   beta gone entirely from outbound/inbound/meta
	//   gamma.inbound drops beta
	g := NewGraphIndex()
	g.LoadFromPages(map[string]*Page{
		"alpha": makeTestPage("Alpha", "[[beta]]", nil),
		"beta":  makeTestPage("Beta", "[[gamma]]", nil),
		"gamma": makeTestPage("Gamma", "leaf", nil),
	})
	g.RemoveNode("beta")
	if g.HasNode("beta") {
		t.Fatal("beta should be gone from meta")
	}
	if got := g.OutNeighbors("alpha"); len(got) != 0 {
		t.Fatalf("alpha.outbound retained dead beta: %v", got)
	}
	if got := g.InNeighbors("gamma"); len(got) != 0 {
		t.Fatalf("gamma.inbound retained dead beta: %v", got)
	}
}

func TestGraphIndex_RelatedFrontmatterContributesEdges(t *testing.T) {
	// Backlink-style: B has related: [A] but no [[A]] in body. The index
	// must still surface A as a target of B (B->A).
	g := NewGraphIndex()
	g.LoadFromPages(map[string]*Page{
		"alpha": makeTestPage("Alpha", "alpha body", nil),
		"beta":  makeTestPage("Beta", "beta body", []string{"alpha"}),
	})
	if got := g.OutNeighbors("beta"); !slices.Equal(got, []string{"alpha"}) {
		t.Fatalf("beta out from related = %v, want [alpha]", got)
	}
	if got := g.InNeighbors("alpha"); !slices.Equal(got, []string{"beta"}) {
		t.Fatalf("alpha in from beta.related = %v, want [beta]", got)
	}
}

func TestGraphIndex_SelfLoopIgnored(t *testing.T) {
	// A body containing [[self]] should NOT register self-edges.
	g := NewGraphIndex()
	g.LoadFromPages(map[string]*Page{
		"self": makeTestPage("Self", "I am [[self]].", []string{"self"}),
	})
	if got := g.OutNeighbors("self"); len(got) != 0 {
		t.Fatalf("self-loop tracked: %v", got)
	}
	if got := g.InNeighbors("self"); len(got) != 0 {
		t.Fatalf("self-loop tracked as inbound: %v", got)
	}
}

func TestGraphIndex_DegreeCounts(t *testing.T) {
	g := NewGraphIndex()
	g.LoadFromPages(map[string]*Page{
		"hub":   makeTestPage("Hub", "[[a]] [[b]] [[c]]", nil),
		"a":     makeTestPage("A", "[[hub]]", nil),
		"b":     makeTestPage("B", "leaf", nil),
		"c":     makeTestPage("C", "leaf", nil),
		"alone": makeTestPage("Alone", "no links", nil),
	})
	in, out := g.Degree("hub")
	if out != 3 || in != 1 {
		t.Fatalf("hub degree = in:%d out:%d, want in:1 out:3", in, out)
	}
	in, out = g.Degree("alone")
	if out != 0 || in != 0 {
		t.Fatalf("alone degree = in:%d out:%d, want 0/0", in, out)
	}
}

func TestGraphIndex_MetaSnapshotIsIsolated(t *testing.T) {
	// Mutating the returned Tags slice must NOT affect future lookups.
	// (Documented as caller-must-not-mutate, but we still freeze the
	// input on insert so an external mutation can't leak into the index.)
	g := NewGraphIndex()
	g.LoadFromPages(map[string]*Page{
		"alpha": makeTestPage("Alpha", "body", nil),
	})
	// inject a real tags slice via RefreshPage so we exercise the copy.
	p := makeTestPage("Alpha", "body", nil)
	p.Tags = []string{"first", "second"}
	g.RefreshPage("alpha", p)

	m1, _ := g.Meta("alpha")
	// Mutate the source page's tags AFTER inserting; the index snapshot
	// must remain unchanged.
	p.Tags[0] = "TAMPERED"

	m2, _ := g.Meta("alpha")
	if m2.Tags[0] == "TAMPERED" {
		t.Fatalf("index tags leaked: %v", m2.Tags)
	}
	if m1.Tags[0] != m2.Tags[0] {
		t.Fatalf("meta differs unexpectedly: %v vs %v", m1.Tags, m2.Tags)
	}
}

// Integration with Store: WritePage should refresh the index so
// subsequent traversals see the new edges without a manual rebuild.
func TestStore_WritePageRefreshesGraphIndex(t *testing.T) {
	s := newWritesTestStore(t)
	ctx := context.Background()

	writeFixturePage(t, s, "Target", "target body", "2026-05-12T00:00:00Z")
	writer := makeTestPage("Writer", "References [[target]].", nil)
	if err := s.WritePage(ctx, writer); err != nil {
		t.Fatal(err)
	}

	idx := s.GraphIndex()
	if idx == nil {
		t.Fatal("Store has no graph index")
	}
	if got := idx.OutNeighbors("writer"); !slices.Equal(got, []string{"target"}) {
		t.Fatalf("writer out = %v, want [target]", got)
	}
	// Wave 2.1 backlink maintenance adds writer to target.Related, so
	// target's outbound (Related-derived) should also include writer.
	if got := idx.OutNeighbors("target"); !slices.Contains(got, "writer") {
		t.Fatalf("target out missing writer (backlink not refreshed): %v", got)
	}
}

func TestStore_DeletePagePurgesGraphIndex(t *testing.T) {
	s := newWritesTestStore(t)
	ctx := context.Background()
	writeFixturePage(t, s, "Alpha", "Refers to [[beta]].", "2026-05-12T00:00:00Z")
	writeFixturePage(t, s, "Beta", "leaf", "2026-05-12T00:00:00Z")

	if err := s.DeletePage(ctx, "beta"); err != nil {
		t.Fatal(err)
	}

	idx := s.GraphIndex()
	if idx.HasNode("beta") {
		t.Fatal("beta still in graph index after DeletePage")
	}
	// alpha.outbound should no longer point at the (now deleted) beta.
	if got := idx.OutNeighbors("alpha"); slices.Contains(got, "beta") {
		t.Fatalf("alpha.outbound retained deleted beta: %v", got)
	}
}

// BenchmarkGraphIndex_NeighborsDepth2 sets a baseline for traversal
// latency on a realistic-ish corpus. The Wave 2 plan committed to
// "<5ms for 1000 nodes depth 2"; this exercises that envelope.
func BenchmarkGraphIndex_NeighborsDepth2(b *testing.B) {
	g := NewGraphIndex()
	pages := make(map[string]*Page, 1000)
	// Build a graph where each node links to its 5 successors mod 1000.
	for i := 0; i < 1000; i++ {
		body := ""
		for j := 1; j <= 5; j++ {
			body += fmt.Sprintf("[[n%d]] ", (i+j)%1000)
		}
		slug := fmt.Sprintf("n%d", i)
		pages[slug] = &Page{
			Title:         slug,
			Body:          body,
			SchemaVersion: CurrentSchemaVersion,
			PromptVersion: "v1",
			CreatedAt:     "2026-05-12T00:00:00Z",
			UpdatedAt:     "2026-05-12T00:00:00Z",
		}
	}
	g.LoadFromPages(pages)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.Neighbors("n0", 2)
	}
}

// Sanity check: with 5 outbound per node + bidirectional traversal,
// depth 2 reaches ~25 nodes. Real assertion is timing, but we can
// also verify the cardinality is in the expected ballpark.
func TestGraphIndex_Depth2BallparkOnSyntheticChain(t *testing.T) {
	g := NewGraphIndex()
	pages := map[string]*Page{
		"n0": makeTestPage("N0", "[[n1]] [[n2]]", nil),
		"n1": makeTestPage("N1", "[[n3]] [[n4]]", nil),
		"n2": makeTestPage("N2", "[[n5]]", nil),
		"n3": makeTestPage("N3", "leaf", nil),
		"n4": makeTestPage("N4", "leaf", nil),
		"n5": makeTestPage("N5", "leaf", nil),
	}
	g.LoadFromPages(pages)
	got := g.Neighbors("n0", 2)
	// Expected: n1, n2 at depth 1; n3, n4, n5 at depth 2.
	want := []string{"n1", "n2", "n3", "n4", "n5"}
	if !slices.Equal(got, want) {
		t.Fatalf("depth=2 from n0: got %v, want %v", got, want)
	}
}

func TestShortestPath_SameNode(t *testing.T) {
	g := NewGraphIndex()
	g.LoadFromPages(map[string]*Page{
		"alpha": makeTestPage("Alpha", "body", nil),
	})
	got := g.ShortestPath("alpha", "alpha", 5)
	if len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("same-node path = %v, want [alpha]", got)
	}
}

func TestShortestPath_DirectNeighbor(t *testing.T) {
	g := NewGraphIndex()
	g.LoadFromPages(map[string]*Page{
		"alpha": makeTestPage("Alpha", "[[beta]]", nil),
		"beta":  makeTestPage("Beta", "leaf", nil),
	})
	got := g.ShortestPath("alpha", "beta", 5)
	want := []string{"alpha", "beta"}
	if !slices.Equal(got, want) {
		t.Fatalf("direct path = %v, want %v", got, want)
	}
}

func TestShortestPath_3HopChain(t *testing.T) {
	// alpha -> beta -> gamma -> delta
	g := NewGraphIndex()
	g.LoadFromPages(map[string]*Page{
		"alpha": makeTestPage("Alpha", "[[beta]]", nil),
		"beta":  makeTestPage("Beta", "[[gamma]]", nil),
		"gamma": makeTestPage("Gamma", "[[delta]]", nil),
		"delta": makeTestPage("Delta", "leaf", nil),
	})
	got := g.ShortestPath("alpha", "delta", 5)
	want := []string{"alpha", "beta", "gamma", "delta"}
	if !slices.Equal(got, want) {
		t.Fatalf("3-hop path = %v, want %v", got, want)
	}
}

func TestShortestPath_NoPath(t *testing.T) {
	g := NewGraphIndex()
	g.LoadFromPages(map[string]*Page{
		"alpha": makeTestPage("Alpha", "body", nil),
		"beta":  makeTestPage("Beta", "body", nil),
	})
	got := g.ShortestPath("alpha", "beta", 5)
	if got != nil {
		t.Fatalf("expected nil for disconnected nodes, got %v", got)
	}
}

func TestShortestPath_MaxHopsCap(t *testing.T) {
	// alpha -> beta -> gamma -> delta: path exists at 3 hops; max_hops=2 returns nil
	g := NewGraphIndex()
	g.LoadFromPages(map[string]*Page{
		"alpha": makeTestPage("Alpha", "[[beta]]", nil),
		"beta":  makeTestPage("Beta", "[[gamma]]", nil),
		"gamma": makeTestPage("Gamma", "[[delta]]", nil),
		"delta": makeTestPage("Delta", "leaf", nil),
	})
	if got := g.ShortestPath("alpha", "delta", 2); got != nil {
		t.Fatalf("expected nil when path requires 3 hops but max_hops=2, got %v", got)
	}
	// max_hops=3 should find it.
	if got := g.ShortestPath("alpha", "delta", 3); len(got) != 4 {
		t.Fatalf("expected 4-node path at max_hops=3, got %v", got)
	}
}

func TestShortestPath_UnknownSlug(t *testing.T) {
	g := NewGraphIndex()
	g.LoadFromPages(map[string]*Page{
		"alpha": makeTestPage("Alpha", "body", nil),
	})
	if got := g.ShortestPath("alpha", "nonexistent", 5); got != nil {
		t.Fatalf("unknown to returns non-nil: %v", got)
	}
	if got := g.ShortestPath("nonexistent", "alpha", 5); got != nil {
		t.Fatalf("unknown from returns non-nil: %v", got)
	}
}

// _ = time.Now is a tiny hedge to keep the time import even if no test
// touches the field directly — schema requires CreatedAt/UpdatedAt and
// helper makeTestPage generates them.
var _ = time.Now
