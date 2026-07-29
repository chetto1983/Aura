package semindex

import (
	"context"
	"sync"
	"testing"
)

func TestRanker_NilEmbedderIsNil(t *testing.T) {
	t.Parallel()
	if r := NewRanker(nil); r != nil {
		t.Fatal("NewRanker(nil) must return nil")
	}
	var nilR *Ranker
	if got := nilR.RankVecs([]float64{1, 0, 0}, 3); got != nil {
		t.Fatalf("nil ranker RankVecs = %v; want nil", got)
	}
}

func TestRanker_TopKByCosine(t *testing.T) {
	t.Parallel()
	r := NewRanker(&fakeEmbedder{})
	r.AddVecs(Item{Label: "chat", Vec: []float64{1, 0, 0}})
	r.AddVecs(Item{Label: "weather", Vec: []float64{0, 1, 0}})
	r.AddVecs(Item{Label: "code", Vec: []float64{0, 0, 1}})

	q := l2normalize([]float64{0.8, 0.6, 0})
	got := r.RankVecs(q, 2)
	if len(got) != 2 {
		t.Fatalf("top-2 returned %d results", len(got))
	}
	if got[0].Label != "chat" || got[1].Label != "weather" {
		t.Fatalf("top-2 order = %v; want [chat weather]", got)
	}
	if !approxEq(got[0].Score, 0.8) || !approxEq(got[1].Score, 0.6) {
		t.Fatalf("top-2 scores = %v; want [0.8 0.6]", got)
	}
}

// TestRanker_DeterministicTieBreak asserts a fixed []Scored order under equal
// scores (stable tie-break by insertion index — copied from bm25 rank).
func TestRanker_DeterministicTieBreak(t *testing.T) {
	t.Parallel()
	r := NewRanker(&fakeEmbedder{})
	// Three items all orthogonal to the query → all score 0 (equal). Insertion
	// order must be preserved deterministically.
	r.AddVecs(
		Item{Label: "alpha", Vec: []float64{0, 1, 0}},
		Item{Label: "beta", Vec: []float64{0, 1, 0}},
		Item{Label: "gamma", Vec: []float64{0, 1, 0}},
	)
	for range 20 {
		got := r.RankVecs([]float64{1, 0, 0}, 3)
		if len(got) != 3 || got[0].Label != "alpha" || got[1].Label != "beta" || got[2].Label != "gamma" {
			t.Fatalf("tie-break order = %v; want [alpha beta gamma]", got)
		}
	}
}

// TestRanker_IncrementalAppend is the D-03 enabler: Add appends, the bank grows
// by exactly the appended count, and previously-added items still rank.
func TestRanker_IncrementalAppend(t *testing.T) {
	t.Parallel()
	r := NewRanker(&fakeEmbedder{})
	r.AddVecs(Item{Label: "chat", Vec: []float64{1, 0, 0}})
	if n := r.Len(); n != 1 {
		t.Fatalf("after first add Len = %d; want 1", n)
	}
	r.AddVecs(
		Item{Label: "weather", Vec: []float64{0, 1, 0}},
		Item{Label: "code", Vec: []float64{0, 0, 1}},
	)
	if n := r.Len(); n != 3 {
		t.Fatalf("after append Len = %d; want 3 (grew by exactly 2)", n)
	}
	// The originally-added "chat" must still rank top-1 for an x-axis query.
	got := r.RankVecs([]float64{1, 0, 0}, 1)
	if len(got) != 1 || got[0].Label != "chat" {
		t.Fatalf("prior item lost after append: %v", got)
	}
}

func TestRanker_AddTextEmbedsItems(t *testing.T) {
	t.Parallel()
	r := NewRanker(&fakeEmbedder{})
	if err := r.Add(context.Background(),
		"weather in Rome",
		"debug this script",
	); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if r.Len() != 2 {
		t.Fatalf("Len after text Add = %d; want 2", r.Len())
	}
	got := r.Rank(context.Background(), "what is the weather", 1)
	if len(got.Results) != 1 || got.Results[0].Label != "weather in Rome" {
		t.Fatalf("text Rank top-1 = %v; want weather item", got.Results)
	}
	if got.Err != nil {
		t.Fatalf("text Rank err = %v", got.Err)
	}
}

func TestRanker_RankQueryEmbedFailureSurfacesError(t *testing.T) {
	t.Parallel()
	r := NewRanker(&fakeEmbedder{})
	if err := r.Add(context.Background(), "hello there"); err != nil {
		t.Fatalf("seed Add: %v", err)
	}
	r.embed.(*fakeEmbedder).failQuery = true
	res := r.Rank(context.Background(), "hi", 3)
	if res.Err == nil {
		t.Fatal("Rank should surface the embedder error (Req-6 error surface)")
	}
}

func TestRanker_EmptyBankReturnsEmpty(t *testing.T) {
	t.Parallel()
	r := NewRanker(&fakeEmbedder{})
	if got := r.RankVecs([]float64{1, 0, 0}, 5); len(got) != 0 {
		t.Fatalf("empty-bank rank = %v; want empty", got)
	}
}

func TestRanker_KLargerThanBankReturnsAll(t *testing.T) {
	t.Parallel()
	r := NewRanker(&fakeEmbedder{})
	r.AddVecs(Item{Label: "a", Vec: []float64{1, 0, 0}}, Item{Label: "b", Vec: []float64{0, 1, 0}})
	got := r.RankVecs([]float64{1, 1, 0}, 10)
	if len(got) != 2 {
		t.Fatalf("k>bank returned %d; want 2 (all)", len(got))
	}
}

func TestRanker_KZeroOrNegativeReturnsAll(t *testing.T) {
	t.Parallel()
	r := NewRanker(&fakeEmbedder{})
	r.AddVecs(Item{Label: "a", Vec: []float64{1, 0, 0}}, Item{Label: "b", Vec: []float64{0, 1, 0}})
	if got := r.RankVecs([]float64{1, 1, 0}, 0); len(got) != 2 {
		t.Fatalf("k=0 returned %d; want all 2", len(got))
	}
}

func TestRanker_ConcurrentAddRank(t *testing.T) {
	t.Parallel()
	// RESEARCH risk #5: validate -race cleanliness of concurrent Add (write
	// lock) + Rank (read lock). COW deferred unless a benchmark shows contention.
	r := NewRanker(&fakeEmbedder{})
	r.AddVecs(Item{Label: "seed", Vec: []float64{1, 0, 0}})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() { defer wg.Done(); r.AddVecs(Item{Label: "x", Vec: []float64{0, 0, 1}}) }()
		go func() { defer wg.Done(); _ = r.RankVecs([]float64{1, 0, 0}, 3) }()
	}
	wg.Wait()
}
