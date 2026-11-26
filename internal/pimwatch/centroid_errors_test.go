package pimwatch

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/examplebank"
)

// scriptedEmbedder is centroidEmbedder with failures and spaces set by the test, after the seeds warm.
type scriptedEmbedder struct {
	centroidEmbedder
	mu       sync.Mutex
	spaces   []string // answered in order by Space; the last one repeats
	spaceErr error
	embedErr error
}

func (e *scriptedEmbedder) Space(context.Context) (embeddings.Space, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.spaceErr != nil {
		return embeddings.Space{}, e.spaceErr
	}
	if len(e.spaces) == 0 {
		return embeddings.Space{ID: "space-1"}, nil
	}
	id := e.spaces[0]
	if len(e.spaces) > 1 {
		e.spaces = e.spaces[1:]
	}
	return embeddings.Space{ID: id}, nil
}

func (e *scriptedEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	e.mu.Lock()
	err := e.embedErr
	e.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return e.centroidEmbedder.Embed(ctx, texts)
}

func (e *scriptedEmbedder) set(f func(*scriptedEmbedder)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	f(e)
}

// warmCentroid is a centroid whose seeds are embedded, so the next Judge reaches the page.
func warmCentroid(t *testing.T) (*CentroidShadow, *scriptedEmbedder) {
	t.Helper()
	embedder := &scriptedEmbedder{}
	centroid, err := NewCentroidShadow(&examplebank.Bank{Store: &exampleStore{}, Embedder: embedder}, correctedAccounts{"acc"})
	if err != nil {
		t.Fatal(err)
	}
	judged(t, centroid, []ShadowMail{{From: "a@b.example", Subject: "warm"}})
	return centroid, embedder
}

var invoicePage = []ShadowMail{{From: "a@b.example", Subject: "Fattura"}}

func TestJudgeReportsAnEmbedderThatCannotNameItsSpace(t *testing.T) {
	centroid, embedder := warmCentroid(t)
	down := errors.New("sidecar down")
	embedder.set(func(e *scriptedEmbedder) { e.spaceErr = down })
	if _, err := centroid.Judge(context.Background(), "id-1", invoicePage); !errors.Is(err, down) {
		t.Fatalf("Judge = %v, want the space error", err)
	}
}

func TestJudgeReportsAPageItCouldNotEmbed(t *testing.T) {
	centroid, embedder := warmCentroid(t)
	refused := errors.New("embed refused")
	embedder.set(func(e *scriptedEmbedder) { e.embedErr = refused })
	if answers, err := centroid.Judge(context.Background(), "id-1", invoicePage); !errors.Is(err, refused) || answers != nil {
		t.Fatalf("Judge = %v, %v; want no answers and the embed error", answers, err)
	}
}

// A page embedded in another space than the classifier's would be ranked against the wrong seeds.
func TestJudgeRefusesAPageEmbeddedInAnotherSpace(t *testing.T) {
	centroid, embedder := warmCentroid(t)
	embedder.set(func(e *scriptedEmbedder) { e.spaces = []string{"space-1", "space-2"} })
	answers, err := centroid.Judge(context.Background(), "id-1", invoicePage)
	if err == nil || !strings.Contains(err.Error(), "embedding space changed") || answers != nil {
		t.Fatalf("Judge = %v, %v; want no answers and the space change named", answers, err)
	}
}

type failingExampleStore struct {
	exampleStore
	err error
}

func (s *failingExampleStore) PutExample(context.Context, string, arcadedb.Example) error {
	return s.err
}

func TestLearnSaysWhichStepFailed(t *testing.T) {
	refused := errors.New("embed refused")
	embedder := &scriptedEmbedder{embedErr: refused}
	centroid, err := NewCentroidShadow(&examplebank.Bank{Store: &exampleStore{}, Embedder: embedder}, correctedAccounts{"acc"})
	if err != nil {
		t.Fatal(err)
	}
	lesson := Lesson{AccountID: "acc", ItemID: "m1", From: "a@b.example", Subject: "Oggetto", Tier: TierLater}
	if err := centroid.Learn(context.Background(), "id-1", lesson); !errors.Is(err, refused) || !strings.Contains(err.Error(), "embed the correction") {
		t.Fatalf("Learn with a failing embedder = %v", err)
	}

	full := errors.New("store full")
	centroid, err = NewCentroidShadow(&examplebank.Bank{Store: &failingExampleStore{err: full}, Embedder: &centroidEmbedder{}}, correctedAccounts{"acc"})
	if err != nil {
		t.Fatal(err)
	}
	if err := centroid.Learn(context.Background(), "id-1", lesson); !errors.Is(err, full) || !strings.Contains(err.Error(), "store the correction's example") {
		t.Fatalf("Learn with a failing store = %v", err)
	}
}

type failingAccounts struct{ err error }

func (f failingAccounts) CorrectedAccounts(context.Context, string) ([]string, error) {
	return nil, f.err
}

// Only an account still corrected keeps its examples in the classifier.
func TestTheCentroidCountsTheExamplesOfEveryCorrectedAccount(t *testing.T) {
	centroid, err := NewCentroidShadow(&examplebank.Bank{Store: &exampleStore{}, Embedder: &centroidEmbedder{}}, correctedAccounts{"acc-a", "acc-b"})
	if err != nil {
		t.Fatal(err)
	}
	alive, err := centroid.domain.Owners(context.Background(), "id-1")
	if err != nil || len(alive) != 2 || !alive["acc-a"] || !alive["acc-b"] {
		t.Fatalf("owners = %v, %v; want both corrected accounts", alive, err)
	}

	gone := errors.New("store unreachable")
	centroid, err = NewCentroidShadow(&examplebank.Bank{Store: &exampleStore{}, Embedder: &centroidEmbedder{}}, failingAccounts{err: gone})
	if err != nil {
		t.Fatal(err)
	}
	if alive, err := centroid.domain.Owners(context.Background(), "id-1"); !errors.Is(err, gone) || alive != nil {
		t.Fatalf("owners = %v, %v; want the listing error and no owners", alive, err)
	}
}

// withSeeds swaps the shipped seeds for the length of one test. The pimwatch tests that run in
// parallel start only after every serial test has returned, so none reads the swapped file.
func withSeeds(t *testing.T, raw string) {
	t.Helper()
	shipped := mailTierSeeds
	mailTierSeeds = []byte(raw)
	t.Cleanup(func() { mailTierSeeds = shipped })
}

func TestSeedsThatDoNotParseAreRefused(t *testing.T) {
	withSeeds(t, `[{"from":`)
	if _, err := NewCentroidShadow(&examplebank.Bank{}, correctedAccounts{}); err == nil || !strings.Contains(err.Error(), "read the mail tier seeds") {
		t.Fatalf("NewCentroidShadow = %v, want the seeds refused", err)
	}
}

func TestASeedWithAnUnknownTierIsRefused(t *testing.T) {
	withSeeds(t, `[{"from":"a@b.example","subject":"x","tier":"now"},{"from":"c@d.example","subject":"y","tier":"soonish"}]`)
	if _, err := NewCentroidShadow(&examplebank.Bank{}, correctedAccounts{}); err == nil || !strings.Contains(err.Error(), `"soonish"`) {
		t.Fatalf("NewCentroidShadow = %v, want the tier named", err)
	}
}
