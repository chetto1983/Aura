package examplebank

import (
	"context"
	"errors"
	"hash/fnv"
	"math"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/semindex"
)

// fakeEmbedder embeds a text as its entry in vectors, else as a vector hashed from it. Each Space
// call takes the next entry of spaces, staying on the last.
type fakeEmbedder struct {
	mu      sync.Mutex
	vectors map[string][]float64
	spaces  []string
	fail    int // the first fail Embed calls error
	calls   [][]string
	gate    chan struct{} // when set, Embed answers only once it is closed
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	f.mu.Lock()
	f.calls = append(f.calls, slices.Clone(texts))
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail > 0 {
		f.fail--
		return nil, errors.New("embedder down")
	}
	out := make([][]float64, len(texts))
	for i, text := range texts {
		if vector, ok := f.vectors[text]; ok {
			out[i] = vector
			continue
		}
		out[i] = hashed(text)
	}
	return out, nil
}

func (f *fakeEmbedder) Space(context.Context) (embeddings.Space, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	space := f.spaces[0]
	if len(f.spaces) > 1 {
		f.spaces = f.spaces[1:]
	}
	return embeddings.Space{ID: space}, nil
}

func (f *fakeEmbedder) embedCalls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func hashed(text string) []float64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	seed := float64(h.Sum64()%1000) + 1
	return []float64{math.Sin(seed), math.Cos(seed), math.Sin(2 * seed)}
}

// fakeStore keeps examples in memory and records what it was asked.
type fakeStore struct {
	mu        sync.Mutex
	examples  []arcadedb.Example
	reads     []string // the space of each Examples call
	forgotten []string
	forgetErr error
}

func (s *fakeStore) PutExample(_ context.Context, _ string, e arcadedb.Example) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.examples = append(s.examples, e)
	return nil
}

func (s *fakeStore) Examples(_ context.Context, _, domain, space string) ([]arcadedb.Example, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads = append(s.reads, space)
	var out []arcadedb.Example
	for _, e := range s.examples {
		if e.Domain == domain && e.Space == space {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *fakeStore) ForgetExamples(_ context.Context, _, _, owner string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.forgotten = append(s.forgotten, owner)
	return 0, s.forgetErr
}

func (s *fakeStore) readCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.reads)
}

func everyoneAlive(context.Context, string) (map[string]bool, error) {
	return map[string]bool{"a": true}, nil
}

func twoSeedDomain() Domain {
	return Domain{
		Name:          "tier",
		Seeds:         []Seed{{Text: "seed a", Label: "a"}, {Text: "seed b", Label: "b"}},
		ExampleWeight: 6,
		Owners:        everyoneAlive,
	}
}

func twoSeedEmbedder(spaces ...string) *fakeEmbedder {
	return &fakeEmbedder{
		vectors: map[string][]float64{"seed a": {1, 0, 0}, "seed b": {0, 1, 0}},
		spaces:  spaces,
	}
}

// warmed polls Classifier until the seeds are ready, so the build goroutine has ended.
func warmed(t *testing.T, b *Bank, d Domain, space string) *semindex.Classifier {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		classifier, err := b.Classifier(context.Background(), "id-1", d, space)
		if err == nil {
			return classifier
		}
		if !errors.Is(err, ErrSeedsWarming) || time.Now().After(deadline) {
			t.Fatalf("classifier: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// idle waits until no seed build is in flight.
func idle(t *testing.T, b *Bank) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		b.mu.Lock()
		building := len(b.warming)
		b.mu.Unlock()
		if building == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d seed builds still running", building)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func sameVerdict(t *testing.T, got, want semindex.Verdict) {
	t.Helper()
	if got.Label != want.Label || got.Ok != want.Ok || math.Abs(got.Score-want.Score) > 1e-12 || math.Abs(got.Margin-want.Margin) > 1e-12 {
		t.Fatalf("verdict = %+v, want %+v", got, want)
	}
}

func TestRowNormalisesTheEmbeddingAndAppendsTheFacts(t *testing.T) {
	got := Row([]float64{3, 4}, []float64{0.3, 0})
	want := []float64{0.6, 0.8, 0.3, 0}
	for i := range want {
		if math.Abs(got[i]-want[i]) > 1e-12 || len(got) != len(want) {
			t.Fatalf("Row = %v, want %v", got, want)
		}
	}
}

func TestAnExampleWeighsAsMuchAsSixSeeds(t *testing.T) {
	store := &fakeStore{}
	b := &Bank{Store: store, Embedder: twoSeedEmbedder("s1")}
	d := twoSeedDomain()
	v := []float64{0.6, 0, 0.8}
	if err := b.Put(context.Background(), "id-1", d, arcadedb.Example{Domain: "tier", Owner: "a", Ref: "1", Label: "b", Vector: v, Space: "s1"}); err != nil {
		t.Fatal(err)
	}
	got := warmed(t, b, d, "s1")

	want := semindex.NewClassifier(&fakeEmbedder{})
	want.AddVecs("a", []float64{1, 0, 0})
	want.AddVecs("b", []float64{0, 1, 0}, v, v, v, v, v, v)
	for _, query := range [][]float64{{1, 0, 0}, {0, 1, 0}, {0.7, 0, 0.7}} {
		sameVerdict(t, got.RankVecs(query), want.RankVecs(query))
	}
}

func TestTheSeedsWarmOnceInTheBackground(t *testing.T) {
	embedder := twoSeedEmbedder("s1")
	embedder.gate = make(chan struct{})
	b := &Bank{Store: &fakeStore{}, Embedder: embedder}
	d := twoSeedDomain()
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if _, err := b.Classifier(context.Background(), "id-1", d, "s1"); !errors.Is(err, ErrSeedsWarming) {
				t.Errorf("cold classifier: %v, want ErrSeedsWarming while the seeds embed", err)
			}
		})
	}
	wg.Wait()
	close(embedder.gate)
	warmed(t, b, d, "s1")
	calls := embedder.embedCalls()
	if len(calls) != 1 || !slices.Equal(calls[0], []string{"seed a", "seed b"}) {
		t.Fatalf("embedder calls = %v, want one call with both seeds", calls)
	}
}

func TestAFailedSeedBuildIsRetried(t *testing.T) {
	embedder := twoSeedEmbedder("s1")
	embedder.fail = 1
	b := &Bank{Store: &fakeStore{}, Embedder: embedder}
	d := twoSeedDomain()
	deadline := time.Now().Add(5 * time.Second)
	for len(embedder.embedCalls()) < 1 && time.Now().Before(deadline) {
		_, _ = b.Classifier(context.Background(), "id-1", d, "s1")
		time.Sleep(5 * time.Millisecond)
	}
	warmed(t, b, d, "s1")
	idle(t, b)
	if calls := embedder.embedCalls(); len(calls) != 2 {
		t.Fatalf("embedder calls = %d, want the failed build and its retry", len(calls))
	}
}

func TestClassifierReadsOnlyTheLiveSpace(t *testing.T) {
	store := &fakeStore{}
	embedder := twoSeedEmbedder("s1")
	b := &Bank{Store: store, Embedder: embedder}
	d := twoSeedDomain()
	warmed(t, b, d, "s1")
	embedder.mu.Lock()
	embedder.spaces = []string{"s2"}
	embedder.mu.Unlock()
	warmed(t, b, d, "s2")
	if calls := embedder.embedCalls(); len(calls) != 2 {
		t.Fatalf("embedder calls = %d, want one seed build per space", len(calls))
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if !slices.Equal(store.reads, []string{"s1", "s2"}) {
		t.Fatalf("reads = %v, want each space read once", store.reads)
	}
}

func TestSeedsEmbeddedInAnotherSpaceAreDiscarded(t *testing.T) {
	embedder := twoSeedEmbedder("s1", "s2")
	b := &Bank{Store: &fakeStore{}, Embedder: embedder}
	d := twoSeedDomain()
	if _, err := b.Classifier(context.Background(), "id-1", d, "s1"); !errors.Is(err, ErrSeedsWarming) {
		t.Fatalf("cold classifier: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(embedder.embedCalls()) < 2 && time.Now().Before(deadline) {
		_, _ = b.Classifier(context.Background(), "id-1", d, "s1")
		time.Sleep(5 * time.Millisecond)
	}
	if calls := embedder.embedCalls(); len(calls) < 2 {
		t.Fatalf("embedder calls = %d: seeds that changed space mid-build were kept", len(calls))
	}
	idle(t, b)
	warmed(t, b, d, "s2")
	idle(t, b)
}

func TestClassifierLeavesOutAndForgetsOrphanOwners(t *testing.T) {
	store := &fakeStore{examples: []arcadedb.Example{
		{Domain: "tier", Owner: "a", Ref: "1", Label: "b", Vector: []float64{0, 0.6, 0.8}, Space: "s1"},
		{Domain: "tier", Owner: "gone", Ref: "2", Label: "a", Vector: []float64{0, 0, 1}, Space: "s1"},
	}}
	b := &Bank{Store: store, Embedder: twoSeedEmbedder("s1")}
	d := twoSeedDomain()
	got := warmed(t, b, d, "s1")

	want := semindex.NewClassifier(&fakeEmbedder{})
	want.AddVecs("a", []float64{1, 0, 0})
	a := []float64{0, 0.6, 0.8}
	want.AddVecs("b", []float64{0, 1, 0}, a, a, a, a, a, a)
	for _, query := range [][]float64{{0, 0, 1}, {1, 0, 0}, {0, 1, 0}} {
		sameVerdict(t, got.RankVecs(query), want.RankVecs(query))
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if !slices.Equal(store.forgotten, []string{"gone"}) {
		t.Fatalf("forgotten = %v, want gone once", store.forgotten)
	}
}

func TestAClassifierWhoseOrphanStayedIsNotCached(t *testing.T) {
	store := &fakeStore{
		examples:  []arcadedb.Example{{Domain: "tier", Owner: "gone", Ref: "2", Label: "a", Vector: []float64{0, 0, 1}, Space: "s1"}},
		forgetErr: errors.New("arcadedb down"),
	}
	b := &Bank{Store: store, Embedder: twoSeedEmbedder("s1")}
	d := twoSeedDomain()
	warmed(t, b, d, "s1")
	if _, err := b.Classifier(context.Background(), "id-1", d, "s1"); err != nil {
		t.Fatal(err)
	}
	if reads := store.readCount(); reads != 2 {
		t.Fatalf("reads = %d, want a rebuild while the orphan stays", reads)
	}
}

func TestClassifierIsCachedUntilAnExampleChanges(t *testing.T) {
	store := &fakeStore{}
	b := &Bank{Store: store, Embedder: twoSeedEmbedder("s1")}
	d := twoSeedDomain()
	ctx := context.Background()
	warmed(t, b, d, "s1")
	if _, err := b.Classifier(ctx, "id-1", d, "s1"); err != nil {
		t.Fatal(err)
	}
	if reads := store.readCount(); reads != 1 {
		t.Fatalf("reads = %d, want the cached classifier", reads)
	}
	if err := b.Put(ctx, "id-1", d, arcadedb.Example{Domain: "tier", Owner: "a", Ref: "1", Label: "a", Vector: []float64{1, 0, 0}, Space: "s1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Classifier(ctx, "id-1", d, "s1"); err != nil {
		t.Fatal(err)
	}
	if err := b.ForgetOwner(ctx, "id-1", d, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Classifier(ctx, "id-1", d, "s1"); err != nil {
		t.Fatal(err)
	}
	if reads := store.readCount(); reads != 3 {
		t.Fatalf("reads = %d, want a fresh read after the put and after the forget", reads)
	}
}

func TestAFailedForgetStillDropsTheClassifier(t *testing.T) {
	store := &fakeStore{forgetErr: errors.New("arcadedb down")}
	b := &Bank{Store: store, Embedder: twoSeedEmbedder("s1")}
	d := twoSeedDomain()
	ctx := context.Background()
	warmed(t, b, d, "s1")
	if err := b.ForgetOwner(ctx, "id-1", d, "a"); err == nil {
		t.Fatal("the forget's failure was lost")
	}
	if _, err := b.Classifier(ctx, "id-1", d, "s1"); err != nil {
		t.Fatal(err)
	}
	if reads := store.readCount(); reads != 2 {
		t.Fatalf("reads = %d, want a rebuild: a forget that may have half-landed leaves no classifier cached", reads)
	}
}

func TestEmbedRefusesASpaceThatChangedMidRequest(t *testing.T) {
	b := &Bank{Store: &fakeStore{}, Embedder: twoSeedEmbedder("s1", "s2")}
	if _, _, err := b.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("vectors embedded across a space swap were returned")
	}
}

func TestEmbedNamesTheSpaceOfItsVectors(t *testing.T) {
	b := &Bank{Store: &fakeStore{}, Embedder: twoSeedEmbedder("s1")}
	vectors, space, err := b.Embed(context.Background(), []string{"seed a"})
	if err != nil || space != "s1" || len(vectors) != 1 || !slices.Equal(vectors[0], []float64{1, 0, 0}) {
		t.Fatalf("Embed = %v, %q, %v", vectors, space, err)
	}
}

func TestEmbedPassesOnTheEmbeddersFailure(t *testing.T) {
	embedder := twoSeedEmbedder("s1")
	embedder.fail = 1
	b := &Bank{Store: &fakeStore{}, Embedder: embedder}
	if _, _, err := b.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("a failed embedding was returned as vectors")
	}
}

func TestAClassifierThatCannotReadItsOwnersIsNotBuilt(t *testing.T) {
	b := &Bank{Store: &fakeStore{}, Embedder: twoSeedEmbedder("s1")}
	d := twoSeedDomain()
	warmed(t, b, d, "s1")
	b.changed("id-1", d.Name)
	d.Owners = func(context.Context, string) (map[string]bool, error) { return nil, errors.New("postgres down") }
	if _, err := b.Classifier(context.Background(), "id-1", d, "s1"); err == nil {
		t.Fatal("a classifier was built without knowing which owners are alive")
	}
}

type failingStore struct{ fakeStore }

func (*failingStore) Examples(context.Context, string, string, string) ([]arcadedb.Example, error) {
	return nil, errors.New("arcadedb down")
}

func TestAClassifierThatCannotReadItsExamplesIsNotBuilt(t *testing.T) {
	b := &Bank{Store: &failingStore{}, Embedder: twoSeedEmbedder("s1")}
	d := twoSeedDomain()
	d.Seeds = nil
	if _, err := b.Classifier(context.Background(), "id-1", d, "s1"); err == nil {
		t.Fatal("a classifier was built without the examples")
	}
}

func TestPutRefusesAnExampleOfAnotherDomain(t *testing.T) {
	store := &fakeStore{}
	b := &Bank{Store: store, Embedder: twoSeedEmbedder("s1")}
	err := b.Put(context.Background(), "id-1", twoSeedDomain(), arcadedb.Example{Domain: "documents", Owner: "a", Ref: "1", Label: "a", Vector: []float64{1}, Space: "s1"})
	if err == nil || len(store.examples) != 0 {
		t.Fatalf("an example of another domain was stored: %v", err)
	}
}

func TestADomainWithNoSeedsIsReadyAtOnce(t *testing.T) {
	store := &fakeStore{examples: []arcadedb.Example{{Domain: "tier", Owner: "a", Ref: "1", Label: "b", Vector: []float64{0, 1, 0}, Space: "s1"}}}
	embedder := twoSeedEmbedder("s1")
	b := &Bank{Store: store, Embedder: embedder}
	d := twoSeedDomain()
	d.Seeds = nil
	classifier, err := b.Classifier(context.Background(), "id-1", d, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if got := classifier.RankVecs([]float64{0, 1, 0}); got.Label != "b" {
		t.Fatalf("verdict = %+v", got)
	}
	if calls := embedder.embedCalls(); len(calls) != 0 {
		t.Fatalf("a domain with no seeds embedded %v", calls)
	}
}
