package semindex

import (
	"context"
	"sync"
	"testing"
)

func TestClassifier_NilEmbedderIsNil(t *testing.T) {
	t.Parallel()
	if c := NewClassifier(nil); c != nil {
		t.Fatal("NewClassifier(nil) must return nil")
	}
	var nilC *Classifier
	if v := nilC.RankVecs([]float64{1, 0, 0}); v.Ok {
		t.Fatal("nil classifier RankVecs must report not-ok")
	}
}

func TestClassifier_RankVecsArgmaxAndMargin(t *testing.T) {
	t.Parallel()
	c := NewClassifier(&fakeEmbedder{})
	c.AddVecs("chat", []float64{1, 0, 0})
	c.AddVecs("weather", []float64{0, 1, 0})
	c.AddVecs("code", []float64{0, 0, 1})

	// Query tilted toward x (chat) but with some weather component.
	q := l2normalize([]float64{0.8, 0.6, 0})
	v := c.RankVecs(q)
	if !v.Ok || v.Label != "chat" {
		t.Fatalf("RankVecs verdict = %+v; want label=chat ok", v)
	}
	// scores: chat=0.8, weather=0.6, code=0 → margin = 0.8-0.6 = 0.2 (golden).
	if !approxEq(v.Margin, 0.2) {
		t.Fatalf("margin = %v; want 0.2", v.Margin)
	}
	if !approxEq(v.Score, 0.8) {
		t.Fatalf("score = %v; want 0.8", v.Score)
	}
}

func TestClassifier_AddFoldsIntoGroupCentroid(t *testing.T) {
	t.Parallel()
	c := NewClassifier(&fakeEmbedder{})
	// Two exemplars for "chat": [1,0,0] and [0.6,0.8,0]. Their mean is
	// [0.8,0.4,0]; normalized centroid favors x. A query [1,0,0] should still
	// pick chat over a single-exemplar competing group.
	c.AddVecs("chat", []float64{1, 0, 0}, []float64{0.6, 0.8, 0})
	c.AddVecs("code", []float64{0, 0, 1})
	v := c.RankVecs([]float64{1, 0, 0})
	if !v.Ok || v.Label != "chat" {
		t.Fatalf("folded centroid verdict = %+v; want chat", v)
	}
}

func TestClassifier_RankTextEmbedsQuery(t *testing.T) {
	t.Parallel()
	f := &fakeEmbedder{}
	c := NewClassifier(f)
	if err := c.Add(context.Background(), "weather", "weather in Rome", "forecast tomorrow"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := c.Add(context.Background(), "code", "debug this script", "refactor module"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	v, err := c.Rank(context.Background(), "what is the weather forecast")
	if err != nil {
		t.Fatalf("Rank: %v", err)
	}
	if !v.Ok || v.Label != "weather" {
		t.Fatalf("Rank verdict = %+v; want weather", v)
	}
}

func TestClassifier_RankQueryEmbedFailureSurfacesError(t *testing.T) {
	t.Parallel()
	c := NewClassifier(&fakeEmbedder{})
	if err := c.Add(context.Background(), "chat", "hello there"); err != nil {
		t.Fatalf("seed Add: %v", err)
	}
	// Force the next single-text query embed to fail.
	c.embed.(*fakeEmbedder).failQuery = true
	if _, err := c.Rank(context.Background(), "hi"); err == nil {
		t.Fatal("Rank should surface the embedder error (Req-6 error surface)")
	}
}

func TestClassifier_AddEmbedFailureNotCached(t *testing.T) {
	t.Parallel()
	// First Add fails (sidecar down); a retry must succeed — the build failure
	// is never cached (mirror of ensureAnchors self-heal discipline).
	f := &fakeEmbedder{failUntil: 1}
	c := NewClassifier(f)
	if err := c.Add(context.Background(), "chat", "hello"); err == nil {
		t.Fatal("first Add should fail while sidecar is down")
	}
	if err := c.Add(context.Background(), "chat", "hello"); err != nil {
		t.Fatalf("retry Add should succeed after recovery; got %v", err)
	}
	v := c.RankVecs([]float64{1, 0, 0})
	if !v.Ok || v.Label != "chat" {
		t.Fatalf("post-recovery verdict = %+v; want chat", v)
	}
}

func TestClassifier_EmptyBankNotOk(t *testing.T) {
	t.Parallel()
	c := NewClassifier(&fakeEmbedder{})
	if v := c.RankVecs([]float64{1, 0, 0}); v.Ok {
		t.Fatalf("empty-bank verdict = %+v; want not ok", v)
	}
}

func TestClassifier_ConcurrentAddRank(t *testing.T) {
	t.Parallel()
	c := NewClassifier(&fakeEmbedder{})
	c.AddVecs("chat", []float64{1, 0, 0})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); c.AddVecs("code", []float64{0, 0, 1}) }()
		go func() { defer wg.Done(); _ = c.RankVecs([]float64{1, 0, 0}) }()
	}
	wg.Wait()
}
