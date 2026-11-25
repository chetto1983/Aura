package semindex

import (
	"sync"
	"testing"
)

func TestClassifier_NilReceiverIsNotOk(t *testing.T) {
	t.Parallel()
	var nilC *Classifier
	if v := nilC.RankVecs([]float64{1, 0, 0}); v.Ok {
		t.Fatal("nil classifier RankVecs must report not-ok")
	}
}

func TestClassifier_RankVecsArgmaxAndMargin(t *testing.T) {
	t.Parallel()
	c := NewClassifier()
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

// A group spanning two intents loses on its centroid to a compact group nearby, and wins on
// its nearest exemplars when the query is one of its intents.
func TestClassifier_RankNearestFollowsTheMatchingIntent(t *testing.T) {
	t.Parallel()
	c := NewClassifier()
	x, z := []float64{1, 0, 0}, []float64{0, 0, 1}
	c.AddVecs("spread", x, x, x, z, z, z)
	c.AddVecs("compact", []float64{0.8, 0.6, 0}, []float64{0.8, 0.6, 0})

	if v := c.RankVecs(x); v.Label != "compact" {
		t.Fatalf("RankVecs = %+v; want compact (spread's centroid sits at cos 0.707)", v)
	}
	v := c.RankNearest(x, 3)
	if !v.Ok || v.Label != "spread" {
		t.Fatalf("RankNearest = %+v; want spread", v)
	}
	// spread's three nearest are x itself (1.0); compact has only two exemplars, both 0.8.
	if !approxEq(v.Score, 1.0) || !approxEq(v.Margin, 0.2) {
		t.Fatalf("score/margin = %v/%v; want 1.0/0.2", v.Score, v.Margin)
	}
	// Past spread's x exemplars a z one (cos 0) joins its mean, 3/4, and compact's 0.8 wins:
	// k is what decides how far a tier's other intents can dilute the one that matches.
	if v := c.RankNearest(x, 4); v.Label != "compact" || !approxEq(v.Margin, 0.05) {
		t.Fatalf("RankNearest k=4 = %+v; want compact by 0.05", v)
	}
}

func TestClassifier_RankNearestRefusesWhatItCannotRank(t *testing.T) {
	t.Parallel()
	var nilC *Classifier
	if v := nilC.RankNearest([]float64{1, 0, 0}, 3); v.Ok {
		t.Fatal("nil classifier RankNearest must report not-ok")
	}
	c := NewClassifier()
	if v := c.RankNearest([]float64{1, 0, 0}, 3); v.Ok {
		t.Fatalf("empty-bank verdict = %+v; want not ok", v)
	}
	c.AddVecs("chat", []float64{1, 0, 0})
	if v := c.RankNearest([]float64{1, 0, 0}, 0); v.Ok {
		t.Fatalf("k=0 verdict = %+v; want not ok", v)
	}
}

func TestClassifier_EmptyBankNotOk(t *testing.T) {
	t.Parallel()
	c := NewClassifier()
	if v := c.RankVecs([]float64{1, 0, 0}); v.Ok {
		t.Fatalf("empty-bank verdict = %+v; want not ok", v)
	}
}

func TestClassifier_ConcurrentAddRank(t *testing.T) {
	t.Parallel()
	c := NewClassifier()
	c.AddVecs("chat", []float64{1, 0, 0})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { c.AddVecs("code", []float64{0, 0, 1}) })
		wg.Go(func() { _ = c.RankVecs([]float64{1, 0, 0}) })
		wg.Go(func() { _ = c.RankNearest([]float64{1, 0, 0}, 3) })
	}
	wg.Wait()
}
