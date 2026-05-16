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
// Score equals the sum of all three channel scores.
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
