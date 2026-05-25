package search

import (
	"testing"
)

// TestMergeHybridResultsPreservesScoreComponents verifies that a Result
// appearing in exactly one channel has only that channel's component non-zero,
// and that the fused Score is non-zero for all hits.
func TestMergeHybridResultsPreservesScoreComponents(t *testing.T) {
	rExact := Result{Kind: "wiki", Slug: "exact-doc", Title: "Exact Only"}
	rFTS := Result{Kind: "wiki", Slug: "fts-doc", Title: "FTS Only"}
	rVector := Result{Kind: "wiki", Slug: "vector-doc", Title: "Vector Only"}

	// Groups: [exact], [fts], [vector]
	results := mergeHybridResults("q", 10,
		[]Result{rExact},
		[]Result{rFTS},
		[]Result{rVector},
	)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	bySlug := map[string]Result{}
	for _, r := range results {
		bySlug[r.Slug] = r
	}

	t.Run("exact-only result", func(t *testing.T) {
		r, ok := bySlug["exact-doc"]
		if !ok {
			t.Fatal("exact-doc missing from results")
		}
		if r.Score == 0 {
			t.Error("fused Score must be non-zero")
		}
		if r.ScoreExact == 0 {
			t.Error("ScoreExact must be non-zero")
		}
		if r.ScoreFTS != 0 {
			t.Errorf("ScoreFTS must be zero, got %v", r.ScoreFTS)
		}
		if r.ScoreVector != 0 {
			t.Errorf("ScoreVector must be zero, got %v", r.ScoreVector)
		}
	})

	t.Run("fts-only result", func(t *testing.T) {
		r, ok := bySlug["fts-doc"]
		if !ok {
			t.Fatal("fts-doc missing from results")
		}
		if r.Score == 0 {
			t.Error("fused Score must be non-zero")
		}
		if r.ScoreExact != 0 {
			t.Errorf("ScoreExact must be zero, got %v", r.ScoreExact)
		}
		if r.ScoreFTS == 0 {
			t.Error("ScoreFTS must be non-zero")
		}
		if r.ScoreVector != 0 {
			t.Errorf("ScoreVector must be zero, got %v", r.ScoreVector)
		}
	})

	t.Run("vector-only result", func(t *testing.T) {
		r, ok := bySlug["vector-doc"]
		if !ok {
			t.Fatal("vector-doc missing from results")
		}
		if r.Score == 0 {
			t.Error("fused Score must be non-zero")
		}
		if r.ScoreExact != 0 {
			t.Errorf("ScoreExact must be zero, got %v", r.ScoreExact)
		}
		if r.ScoreFTS != 0 {
			t.Errorf("ScoreFTS must be zero, got %v", r.ScoreFTS)
		}
		if r.ScoreVector == 0 {
			t.Error("ScoreVector must be non-zero")
		}
	})
}

// TestMergeHybridResultsMultiChannelAccumulatesComponents verifies that a
// result appearing in all three channels accumulates each component and that
// Score equals the sum of all three channel scores. After normalisation to
// [0,1] both sides scale by the same constant, so the additive invariant holds.
func TestMergeHybridResultsMultiChannelAccumulatesComponents(t *testing.T) {
	r := Result{Kind: "wiki", Slug: "all-channels", Title: "All Channels"}

	results := mergeHybridResults("q", 10,
		[]Result{r},
		[]Result{r},
		[]Result{r},
	)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	got := results[0]
	if got.ScoreExact == 0 {
		t.Error("ScoreExact must be non-zero")
	}
	if got.ScoreFTS == 0 {
		t.Error("ScoreFTS must be non-zero")
	}
	if got.ScoreVector == 0 {
		t.Error("ScoreVector must be non-zero")
	}
	wantSum := got.ScoreExact + got.ScoreFTS + got.ScoreVector
	if got.Score != wantSum {
		t.Errorf("Score=%v != ScoreExact+ScoreFTS+ScoreVector=%v", got.Score, wantSum)
	}
}

// TestMergeHybridResultsNormalisesScoreToUnit asserts that a top-rank hit
// across all three channels scores 1.0, not the raw RRF cap of 2.4/61
// ≈ 0.039. The bug observed in chat 1148481707 turn 269 was that a
// perfect-fit page surfaced as score=0.04, which the model read as
// "almost zero" and re-searched four times. Normalisation fixes that
// signal — see mergeHybridResults docstring for the rationale.
func TestMergeHybridResultsNormalisesScoreToUnit(t *testing.T) {
	r := Result{Kind: "wiki", Slug: "perfect-fit"}
	results := mergeHybridResults("q", 10,
		[]Result{r},
		[]Result{r},
		[]Result{r},
	)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	got := results[0]
	const tolerance = 1e-6
	if diff := float64(got.Score) - 1.0; diff > tolerance || diff < -tolerance {
		t.Errorf("perfect top-1 hit Score = %v, want ≈ 1.0", got.Score)
	}
	// Component fractions should equal weight / totalWeight (1.0/2.4, 0.6/2.4, 0.8/2.4).
	wantExact := float32(rrfWeightExactResult / (rrfWeightExactResult + rrfWeightFTSResult + rrfWeightVectorResult))
	wantFTS := float32(rrfWeightFTSResult / (rrfWeightExactResult + rrfWeightFTSResult + rrfWeightVectorResult))
	wantVector := float32(rrfWeightVectorResult / (rrfWeightExactResult + rrfWeightFTSResult + rrfWeightVectorResult))
	checkClose := func(name string, got, want float32) {
		if d := got - want; d > tolerance || d < -tolerance {
			t.Errorf("%s = %v, want ≈ %v", name, got, want)
		}
	}
	checkClose("ScoreExact", got.ScoreExact, wantExact)
	checkClose("ScoreFTS", got.ScoreFTS, wantFTS)
	checkClose("ScoreVector", got.ScoreVector, wantVector)
}

// TestMergeHybridResultsSingleChannelPerfectHit asserts that a doc ranked #1
// in exactly one channel reaches the full weight share of that channel —
// 0.4167 for exact, 0.25 for FTS, 0.3333 for vector. This bounds the
// "single backend votes strongly" case so the agent reads the channel
// contribution correctly.
func TestMergeHybridResultsSingleChannelPerfectHit(t *testing.T) {
	totalWeight := rrfWeightExactResult + rrfWeightFTSResult + rrfWeightVectorResult
	cases := []struct {
		name      string
		groupIdx  int
		wantScore float32
	}{
		{"exact only", 0, float32(rrfWeightExactResult / totalWeight)},
		{"fts only", 1, float32(rrfWeightFTSResult / totalWeight)},
		{"vector only", 2, float32(rrfWeightVectorResult / totalWeight)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Result{Kind: "wiki", Slug: "channel-only"}
			groups := make([][]Result, 3)
			groups[tc.groupIdx] = []Result{r}
			results := mergeHybridResults("q", 10, groups...)
			if len(results) != 1 {
				t.Fatalf("expected 1 result, got %d", len(results))
			}
			got := results[0].Score
			const tolerance = 1e-6
			if d := got - tc.wantScore; d > tolerance || d < -tolerance {
				t.Errorf("Score = %v, want ≈ %v", got, tc.wantScore)
			}
		})
	}
}

func TestMergeHybridResultsPinsExactSlugBeforeFusion(t *testing.T) {
	exact := []Result{{Kind: "wiki_page", Slug: "davide-marchetto", Title: "Davide Marchetto"}}
	fts := []Result{{Kind: "wiki_page", Slug: "davide-notes", Title: "Davide Notes"}}
	vector := []Result{{Kind: "wiki_page", Slug: "davide-notes", Title: "Davide Notes"}}

	got := mergeHybridResults("davide-marchetto", 5, exact, fts, vector)
	if len(got) == 0 {
		t.Fatal("merge returned no results")
	}
	if got[0].Slug != "davide-marchetto" {
		t.Fatalf("top slug = %q, want exact literal slug; results=%+v", got[0].Slug, got)
	}
	if got[0].Score != 1 || got[0].ScoreExact != 1 {
		t.Fatalf("pinned score = (%v,%v), want Score=1 ScoreExact=1", got[0].Score, got[0].ScoreExact)
	}
}

func TestMergeHybridResultsDoesNotPinNaturalLanguageQuery(t *testing.T) {
	exact := []Result{{Kind: "wiki_page", Slug: "davide-marchetto", Title: "Davide Marchetto"}}
	fts := []Result{{Kind: "wiki_page", Slug: "davide-notes", Title: "Davide Notes"}}
	vector := []Result{{Kind: "wiki_page", Slug: "davide-notes", Title: "Davide Notes"}}

	got := mergeHybridResults("tell me davide", 5, exact, fts, vector)
	if len(got) == 0 {
		t.Fatal("merge returned no results")
	}
	if got[0].Slug == "davide-marchetto" && got[0].Score == 1 {
		t.Fatalf("natural-language query triggered literal slug pin: %+v", got)
	}
}
