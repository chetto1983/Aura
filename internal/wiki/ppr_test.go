package wiki

import "testing"

func TestPPR_Convergence(t *testing.T) {
	// 5-node chain: a – b – c – d – e (undirected via mutual links).
	pages := map[string]*Page{
		"a": {Title: "A", Body: "[[b]]"},
		"b": {Title: "B", Body: "[[a]] [[c]]"},
		"c": {Title: "C", Body: "[[b]] [[d]]"},
		"d": {Title: "D", Body: "[[c]] [[e]]"},
		"e": {Title: "E", Body: "[[d]]"},
	}
	g := NewGraphIndex()
	g.LoadFromPages(pages)

	// Run 29 vs 30 iterations; per-node delta should be well below epsilon.
	r29 := PersonalizedPageRank(g, []string{"a"}, 0.15, 29)
	r30 := PersonalizedPageRank(g, []string{"a"}, 0.15, 30)

	const epsilon = 0.01
	for slug, v30 := range r30 {
		v29 := r29[slug]
		diff := v30 - v29
		if diff < 0 {
			diff = -diff
		}
		if diff >= epsilon {
			t.Errorf("node %q: |r30-r29| = %f ≥ epsilon %f (not converged at 30 iters)",
				slug, diff, epsilon)
		}
	}
}

func TestPPR_SeedDominance(t *testing.T) {
	// Star graph: hub linked to 4 spokes; remote is isolated.
	pages := map[string]*Page{
		"hub":    {Title: "Hub", Body: "[[s1]] [[s2]] [[s3]] [[s4]]"},
		"s1":     {Title: "S1"},
		"s2":     {Title: "S2"},
		"s3":     {Title: "S3"},
		"s4":     {Title: "S4"},
		"remote": {Title: "Remote"},
	}
	g := NewGraphIndex()
	g.LoadFromPages(pages)

	// Seed on hub: hub + direct neighbors must outrank the isolated remote node.
	scores := PersonalizedPageRank(g, []string{"hub"}, 0.15, 30)

	hubScore := scores["hub"]
	remoteScore := scores["remote"]
	if hubScore <= remoteScore {
		t.Errorf("hub score %f should be > remote score %f (seed node must dominate)", hubScore, remoteScore)
	}
	s1Score := scores["s1"]
	if s1Score <= remoteScore {
		t.Errorf("spoke s1 score %f should be > remote score %f (1-hop neighbor must dominate isolated node)", s1Score, remoteScore)
	}
}

func TestPPR_NilGraph(t *testing.T) {
	if got := PersonalizedPageRank(nil, []string{"x"}, 0.15, 30); got != nil {
		t.Errorf("nil graph: got %v, want nil", got)
	}
}

func TestPPR_EmptySeeds(t *testing.T) {
	pages := map[string]*Page{
		"a": {Title: "A"},
		"b": {Title: "B"},
	}
	g := NewGraphIndex()
	g.LoadFromPages(pages)

	// Empty seeds → uniform restart; both nodes should get non-zero score.
	scores := PersonalizedPageRank(g, nil, 0.15, 30)
	if scores == nil {
		t.Fatal("expected non-nil scores for empty seeds with non-empty graph")
	}
	if scores["a"] <= 0 || scores["b"] <= 0 {
		t.Errorf("expected positive scores with uniform restart; got a=%f b=%f", scores["a"], scores["b"])
	}
}
