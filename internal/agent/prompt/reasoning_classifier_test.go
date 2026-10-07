package prompt

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeEmbedder maps each text to a 3-dim one-hot by keyword so each tier's
// exemplars land on its own axis (none=[1,0,0], low=[0,1,0], high=[0,0,1]).
// The keywords cover every real package def/seed: one seed left on another tier's
// axis ties that tier's nearest exemplars with the right one's. failUntil lets a
// test force a transient build failure.
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
	case hasAny("script", "debug", "schema", "dimostra", "rifattor", "scraping", "python", "codice", "progetta",
		"ottimizza", "algoritmo", "stack trace", "pipeline", "build", "foglio", "confronta", "righe", "file",
		"preventiv", "excel", "budget", "fatture", "piano", "registro"):
		return []float64{0, 0, 1} // high
	case hasAny("meteo", "tempo", "notizie", "bitcoin", "farmacia", "ristorante", "prezzo", "orari", "cerca", "costa",
		"treno", "partita", "traffico", "autostrada", "ricordami", "promemoria", "whatsapp", "telegram", "mail",
		"agenda", "calendario", "timer", "avvisami", "notifica", "appuntamento", "libero"):
		return []float64{0, 1, 0} // low
	default:
		return []float64{1, 0, 0} // none
	}
}

func TestReasoningClassifier_RoutesByProximity(t *testing.T) {
	t.Parallel()
	c := NewReasoningClassifier(&fakeEmbedder{})
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
		verdict, ok := c.Classify(context.Background(), tc.prompt)
		if !ok || verdict.Tier != tc.want {
			t.Errorf("Classify(%q) = %q,%v; want %q,true", tc.prompt, verdict.Tier, ok, tc.want)
		}
	}
}

// ensureAnchors publishes the built bank unconditionally, which is only correct because the
// bank is static and built once. That property had no test of its own — it merely looked
// guarded, by a generation counter that was never assigned — so it is pinned here.
func TestReasoningClassifier_AnchorBankIsBuiltOnceAndReused(t *testing.T) {
	t.Parallel()
	embed := &fakeEmbedder{}
	c := NewReasoningClassifier(embed)

	if _, ok := c.Classify(context.Background(), "debugga il mio script python"); !ok {
		t.Fatal("the first Classify failed")
	}
	embed.mu.Lock()
	afterBuild := embed.calls
	embed.mu.Unlock()

	for _, prompt := range []string{"che meteo fa a Roma", "quanto fa 2+2"} {
		if _, ok := c.Classify(context.Background(), prompt); !ok {
			t.Fatalf("Classify(%q) failed", prompt)
		}
	}

	embed.mu.Lock()
	defer embed.mu.Unlock()
	// Asserted as a delta rather than a total so the test pins the property — no rebuild —
	// instead of the number of anchor groups, which is a tier-table detail free to change.
	if embed.calls != afterBuild+2 {
		t.Fatalf("embed calls = %d after %d, want exactly one query embed each and no rebuild",
			embed.calls, afterBuild)
	}
}

// The allowlist moved out of Classify: whether "ok" is a greeting depends on what came before
// it, which only the caller knows (spec 2026-10-06, "The greeting fast path").
func TestIsTrivialGreeting(t *testing.T) {
	t.Parallel()
	for _, greeting := range []string{"ciao", "Buonasera!", "  Grazie mille ", "ok perfetto", "a presto!"} {
		if !IsTrivialGreeting(greeting) {
			t.Errorf("IsTrivialGreeting(%q) = false, want true", greeting)
		}
	}
	for _, request := range []string{"", "   ", "ciao, che tempo fa domani?", "debugga lo script"} {
		if IsTrivialGreeting(request) {
			t.Errorf("IsTrivialGreeting(%q) = true, want false", request)
		}
	}
}

func TestReasoningClassifierReportsTheMargin(t *testing.T) {
	t.Parallel()
	c := NewReasoningClassifier(&fakeEmbedder{})
	verdict, ok := c.Classify(context.Background(), "debugga il mio script python")
	if !ok || verdict.Tier != ReasoningTierHigh {
		t.Fatalf("Classify = %+v,%v; want high", verdict, ok)
	}
	// fakeEmbedder puts every high exemplar on one axis and the others off it, so the high
	// tier scores 1 and the runner-up 0.
	if math.Abs(verdict.Margin-1) > 1e-9 {
		t.Fatalf("margin = %v, want 1", verdict.Margin)
	}
}

func TestClassifyNoLongerShortCircuitsGreetings(t *testing.T) {
	t.Parallel()
	f := &fakeEmbedder{}
	c := NewReasoningClassifier(f)
	if _, ok := c.Classify(context.Background(), "ciao"); !ok {
		t.Fatal("Classify(ciao) failed")
	}
	if f.calls == 0 {
		t.Fatal("Classify answered a greeting without embedding it; the allowlist belongs to the caller now")
	}
}

func TestReasoningPolicyFingerprintIsStable(t *testing.T) {
	t.Parallel()
	first, second := ReasoningPolicyFingerprint(), ReasoningPolicyFingerprint()
	if first != second || len(first) != 64 {
		t.Fatalf("fingerprints %q and %q, want one stable sha256 hex", first, second)
	}
}

// The turn reading folds the fingerprint into its policy version, so a change to any seed or
// to the greeting allowlist must move it. Not parallel: it edits package state and puts it
// back before any parallel test runs.
func TestReasoningPolicyFingerprintFollowsTheSeedsAndTheGreetings(t *testing.T) {
	base := ReasoningPolicyFingerprint()

	seeds := reasoningTierSeeds[ReasoningTierHigh]
	reasoningTierSeeds[ReasoningTierHigh] = append(slices.Clone(seeds), "progetta lo schema del database con le migrazioni")
	withSeed := ReasoningPolicyFingerprint()
	reasoningTierSeeds[ReasoningTierHigh] = seeds

	trivialGreetings["ciao a tutti"] = struct{}{}
	withGreeting := ReasoningPolicyFingerprint()
	delete(trivialGreetings, "ciao a tutti")

	if withSeed == base || withGreeting == base {
		t.Fatalf("fingerprint unmoved: seed %t, greeting %t", withSeed == base, withGreeting == base)
	}
	if ReasoningPolicyFingerprint() != base {
		t.Fatal("the seed bank or the allowlist was not restored")
	}
}

func TestReasoningClassifier_QueryEmbedFailureFallsBack(t *testing.T) {
	t.Parallel()
	c := NewReasoningClassifier(&fakeEmbedder{failQuery: true})
	if verdict, ok := c.Classify(context.Background(), "debugga il mio script"); ok {
		t.Errorf("query embed failure should yield (_,false); got %q,%v", verdict.Tier, ok)
	}
}

func TestReasoningClassifier_AnchorBuildRetriesAfterTransientFailure(t *testing.T) {
	t.Parallel()
	// First Embed call (first tier's anchor build) errors → whole build fails →
	// (_,false). The next Classify retries the build and succeeds.
	f := &fakeEmbedder{failUntil: 1}
	c := NewReasoningClassifier(f)
	if _, ok := c.Classify(context.Background(), "debugga lo script"); ok {
		t.Fatal("first classify should fail while the sidecar is down")
	}
	if _, ok := c.Classify(context.Background(), "debugga lo script"); !ok {
		t.Fatal("second classify should succeed after the sidecar recovers (build not cached on failure)")
	}
}

type blockingAnchorEmbedder struct {
	started     chan struct{}
	release     chan struct{}
	blocked     atomic.Bool
	anchorCalls atomic.Int32
	queryCalls  atomic.Int32
}

func (e *blockingAnchorEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	if len(texts) > 1 {
		e.anchorCalls.Add(1)
		if e.blocked.CompareAndSwap(false, true) {
			close(e.started)
			select {
			case <-e.release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	} else {
		e.queryCalls.Add(1)
	}
	out := make([][]float64, len(texts))
	for i, text := range texts {
		out[i] = vecFor(text)
	}
	return out, nil
}

func TestReasoningClassifier_ConcurrentColdStartSingleFlightsAnchorBuild(t *testing.T) {
	emb := &blockingAnchorEmbedder{started: make(chan struct{}), release: make(chan struct{})}
	c := NewReasoningClassifier(emb)
	const callers = 8
	errs := make(chan string, callers)
	for range callers {
		go func() {
			verdict, ok := c.Classify(context.Background(), "debugga lo script")
			if !ok || verdict.Tier != ReasoningTierHigh {
				errs <- string(verdict.Tier)
				return
			}
			errs <- ""
		}()
	}

	select {
	case <-emb.started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("anchor embed did not start")
	}
	time.Sleep(50 * time.Millisecond)
	close(emb.release)
	for i := range callers {
		if got := <-errs; got != "" {
			t.Fatalf("caller %d got %q, want high", i, got)
		}
	}
	if got := emb.anchorCalls.Load(); got != int32(len(classifierTierOrder)) {
		t.Fatalf("anchor build calls = %d, want one build of %d tier batches", got, len(classifierTierOrder))
	}
	if got := emb.queryCalls.Load(); got != callers {
		t.Fatalf("query embed calls = %d, want one per caller", got)
	}
}

func TestNewReasoningClassifier_NilEmbedderIsNil(t *testing.T) {
	t.Parallel()
	c := NewReasoningClassifier(nil)
	if c != nil {
		t.Fatal("NewReasoningClassifier(nil) must return nil")
	}
	// Classify on a nil receiver is safe and reports unusable.
	if _, ok := c.Classify(context.Background(), "ciao"); ok {
		t.Fatal("nil classifier Classify must return false")
	}
}
