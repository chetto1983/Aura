package prompt

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeEmbedder maps each text to a 3-dim one-hot by keyword so the per-tier
// centroids land on orthogonal axes (none=[1,0,0], low=[0,1,0], high=[0,0,1]).
// The keywords match the real package defs/seeds, so the test is robust to their
// exact wording. failFirst lets a test force a transient build failure.
type fakeEmbedder struct {
	mu        sync.Mutex
	calls     int
	failUntil int  // return an error for the first failUntil calls
	failQuery bool // error specifically on a single-text (query) call
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failUntil {
		return nil, errors.New("embed sidecar down")
	}
	if f.failQuery && len(texts) == 1 {
		return nil, errors.New("query embed failed")
	}
	out := make([][]float64, len(texts))
	for i, t := range texts {
		out[i] = vecFor(t)
	}
	return out, nil
}

func vecFor(text string) []float64 {
	l := strings.ToLower(text)
	hasAny := func(ks ...string) bool {
		for _, k := range ks {
			if strings.Contains(l, k) {
				return true
			}
		}
		return false
	}
	switch {
	case hasAny("script", "debug", "schema", "dimostra", "rifattor", "scraping", "python", "codice", "progetta"):
		return []float64{0, 0, 1} // high
	case hasAny("meteo", "tempo", "notizie", "bitcoin", "farmacia", "ristorante", "prezzo", "orari", "cerca", "costa"):
		return []float64{0, 1, 0} // low
	default:
		return []float64{1, 0, 0} // none
	}
}

func TestReasoningClassifier_RoutesByProximity(t *testing.T) {
	t.Parallel()
	c := NewReasoningClassifier(&fakeEmbedder{}, nil)
	cases := []struct {
		prompt string
		want   ReasoningTier
	}{
		{"debugga il mio script python", ReasoningTierHigh},
		{"progetta lo schema di un database", ReasoningTierHigh},
		{"che meteo fa a Roma domani", ReasoningTierLow},
		{"cerca le notizie di oggi", ReasoningTierLow},
		{"qual e la capitale dell'Italia", ReasoningTierNone},
	}
	for _, tc := range cases {
		got, ok := c.Classify(context.Background(), tc.prompt)
		if !ok || got != tc.want {
			t.Errorf("Classify(%q) = %q,%v; want %q,true", tc.prompt, got, ok, tc.want)
		}
	}
}

func TestReasoningClassifier_GreetingPrefilterSkipsEmbed(t *testing.T) {
	t.Parallel()
	f := &fakeEmbedder{}
	c := NewReasoningClassifier(f, nil)
	for _, g := range []string{"ciao", "Buonasera!", "  Grazie mille ", "ok perfetto", "a presto!"} {
		got, ok := c.Classify(context.Background(), g)
		if !ok || got != ReasoningTierNone {
			t.Errorf("greeting %q = %q,%v; want none,true", g, got, ok)
		}
	}
	if f.calls != 0 {
		t.Errorf("greeting pre-filter hit the embedder %d times; want 0 (no round-trip)", f.calls)
	}
}

func TestReasoningClassifier_QueryEmbedFailureFallsBack(t *testing.T) {
	t.Parallel()
	c := NewReasoningClassifier(&fakeEmbedder{failQuery: true}, nil)
	if got, ok := c.Classify(context.Background(), "debugga il mio script"); ok {
		t.Errorf("query embed failure should yield (_,false); got %q,%v", got, ok)
	}
}

func TestReasoningClassifier_AnchorBuildRetriesAfterTransientFailure(t *testing.T) {
	t.Parallel()
	// First Embed call (first tier's anchor build) errors → whole build fails →
	// (_,false). The next Classify retries the build and succeeds.
	f := &fakeEmbedder{failUntil: 1}
	c := NewReasoningClassifier(f, nil)
	if _, ok := c.Classify(context.Background(), "debugga lo script"); ok {
		t.Fatal("first classify should fail while the sidecar is down")
	}
	if _, ok := c.Classify(context.Background(), "debugga lo script"); !ok {
		t.Fatal("second classify should succeed after the sidecar recovers (build not cached on failure)")
	}
}

type fakeStore struct {
	examples []LabeledVec
	err      error
}

func (s *fakeStore) LoadExamples(_ context.Context) ([]LabeledVec, error) {
	return s.examples, s.err
}

// A stable fact ("capitale...") embeds near the low axis (vecFor → none only when
// no keyword matches; "capitale" has none, so it lands none here) — to exercise
// the fold, seed an explicit example vector that pulls a borderline query.
func TestReasoningClassifier_FoldsStoredExamples(t *testing.T) {
	t.Parallel()
	// Without examples, a vector tilted toward low+high equally is ambiguous; an
	// example on the none axis should pull it to none after folding.
	// The borderline query sits away from the [1,0,0] none-seed centroid (so the
	// baseline mis-routes it to low/high), in the region a stable-fact question
	// occupies. Oracle examples at that same region, labeled none, pull the none
	// centroid toward it — exactly the spike-053 mechanism.
	emb := &vecEmbedder{m: map[string][]float64{
		"borderline": {0.40, 0.45, 0.45},
	}}
	base := NewReasoningClassifier(emb, nil)
	basePred, _ := base.Classify(context.Background(), "borderline")
	if basePred == ReasoningTierNone {
		t.Fatal("precondition: baseline should NOT already route the borderline query to none")
	}

	store := &fakeStore{examples: []LabeledVec{
		{Tier: ReasoningTierNone, Vec: []float64{0.40, 0.45, 0.45}},
		{Tier: ReasoningTierNone, Vec: []float64{0.42, 0.44, 0.44}},
		{Tier: ReasoningTierNone, Vec: []float64{0.38, 0.46, 0.46}},
	}}
	learned := NewReasoningClassifier(emb, store)
	learnedPred, ok := learned.Classify(context.Background(), "borderline")
	if !ok {
		t.Fatal("learned classify not usable")
	}
	t.Logf("base=%s learned=%s (fold moved the decision)", basePred, learnedPred)
	if learnedPred != ReasoningTierNone {
		t.Errorf("folding none examples should pull the borderline query to none; got %s", learnedPred)
	}
}

// vecEmbedder returns a fixed vector for known texts, else routes by keyword.
type vecEmbedder struct{ m map[string][]float64 }

func (e *vecEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, t := range texts {
		if v, ok := e.m[t]; ok {
			out[i] = v
		} else {
			out[i] = vecFor(t)
		}
	}
	return out, nil
}

func TestReasoningClassifier_StoreErrorFallsBackToSeeds(t *testing.T) {
	t.Parallel()
	c := NewReasoningClassifier(&fakeEmbedder{}, &fakeStore{err: context.DeadlineExceeded})
	// A clear high query still classifies despite the store erroring (seed-only).
	if got, ok := c.Classify(context.Background(), "debugga il mio script python"); !ok || got != ReasoningTierHigh {
		t.Errorf("store error should fall back to seeds; got %q,%v", got, ok)
	}
}

func TestReasoningClassifier_RefreshReloads(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	c := NewReasoningClassifier(&fakeEmbedder{}, store)
	if _, ok := c.Classify(context.Background(), "ciao mondo qualcosa"); !ok {
		t.Fatal("initial classify failed")
	}
	// Add an example, Refresh, and the next classify rebuilds with it (no panic,
	// still usable — the fold path is re-exercised).
	store.examples = append(store.examples, LabeledVec{Tier: ReasoningTierLow, Vec: []float64{0, 1, 0}})
	c.Refresh()
	if _, ok := c.Classify(context.Background(), "ciao mondo qualcosa"); !ok {
		t.Fatal("post-refresh classify failed")
	}
}

func TestNewReasoningClassifier_NilEmbedderIsNil(t *testing.T) {
	t.Parallel()
	c := NewReasoningClassifier(nil, nil)
	if c != nil {
		t.Fatal("NewReasoningClassifier(nil, nil) must return nil")
	}
	// Classify on a nil receiver is safe and reports unusable.
	if _, ok := c.Classify(context.Background(), "ciao"); ok {
		t.Fatal("nil classifier Classify must return false")
	}
}
