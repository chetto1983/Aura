package pimwatch

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/examplebank"
)

// centroidEmbedder hashes each text to a three-wide vector. block makes Embed wait for its context.
type centroidEmbedder struct {
	mu    sync.Mutex
	calls [][]string
	block bool
}

func (e *centroidEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	e.mu.Lock()
	e.calls = append(e.calls, slices.Clone(texts))
	block := e.block
	e.mu.Unlock()
	if block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	out := make([][]float64, len(texts))
	for i, text := range texts {
		h := fnv.New64a()
		_, _ = h.Write([]byte(text))
		seed := float64(h.Sum64()%997) + 1
		out[i] = []float64{math.Sin(seed), math.Cos(seed), math.Sin(3 * seed)}
	}
	return out, nil
}

func (e *centroidEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: "space-1"}, nil
}

func (e *centroidEmbedder) embedded() [][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.calls)
}

// exampleStore keeps what the bank writes. block makes ForgetExamples wait for its context.
type exampleStore struct {
	mu    sync.Mutex
	put   []arcadedb.Example
	block bool
}

func (s *exampleStore) PutExample(_ context.Context, _ string, e arcadedb.Example) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.put = append(s.put, e)
	return nil
}

func (s *exampleStore) Examples(context.Context, string, string, string) ([]arcadedb.Example, error) {
	return nil, nil
}

func (s *exampleStore) ForgetExamples(ctx context.Context, _, _, _ string) (int, error) {
	if s.block {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	return 0, nil
}

type correctedAccounts []string

func (c correctedAccounts) CorrectedAccounts(context.Context, string) ([]string, error) {
	return c, nil
}

func newTestCentroid(t *testing.T, embedder *centroidEmbedder) (*CentroidShadow, *exampleStore) {
	t.Helper()
	store := &exampleStore{}
	centroid, err := NewCentroidShadow(&examplebank.Bank{Store: store, Embedder: embedder}, correctedAccounts{"acc"})
	if err != nil {
		t.Fatal(err)
	}
	return centroid, store
}

// judged polls Judge until the seeds are warm, so the build goroutine has ended.
func judged(t *testing.T, c *CentroidShadow, mails []ShadowMail) []CentroidAnswer {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		answers, err := c.Judge(context.Background(), "id-1", mails)
		if err == nil {
			return answers
		}
		if !errors.Is(err, examplebank.ErrSeedsWarming) || time.Now().After(deadline) {
			t.Fatalf("judge: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestMailTierSeedsAreTheAnonymisedPilot(t *testing.T) {
	var seeds []mailTierSeed
	if err := json.Unmarshal(mailTierSeeds, &seeds); err != nil {
		t.Fatal(err)
	}
	tiers := map[Tier]int{}
	var written, important, authFailed, bulk int
	for _, seed := range seeds {
		tiers[seed.Tier]++
		for _, fact := range []struct {
			on    bool
			count *int
		}{{seed.WrittenTo, &written}, {seed.ProviderImportant, &important}, {seed.AuthFailed, &authFailed}, {seed.Bulk, &bulk}} {
			if fact.on {
				*fact.count++
			}
		}
		if strings.Count(seed.From, "@") != 1 || !strings.HasSuffix(seed.From, ".example") {
			t.Fatalf("a seed's sender is not under .example: %q", seed.From)
		}
		if strings.TrimSpace(seed.Subject) == "" {
			t.Fatal("a seed has no subject")
		}
	}
	if len(seeds) != 200 || tiers[TierNow] != 23 || tiers[TierToday] != 72 || tiers[TierLater] != 105 {
		t.Fatalf("seeds = %d, tiers %v; want 200 as 23 now, 72 today, 105 later", len(seeds), tiers)
	}
	if written != 154 || important != 25 || authFailed != 12 || bulk != 78 {
		t.Fatalf("facts = %d written, %d important, %d auth failed, %d bulk; want 154, 25, 12, 78", written, important, authFailed, bulk)
	}
}

func TestFactsOfReadsWrittenToOnlyWhenTheProviderSaysSo(t *testing.T) {
	yes, no := true, false
	for _, c := range []struct {
		written *bool
		want    bool
	}{{nil, false}, {&no, false}, {&yes, true}} {
		if got := factsOf(Signals{WrittenTo: c.written}).WrittenTo; got != c.want {
			t.Fatalf("WrittenTo %v gave %v", c.written, got)
		}
	}
	got := factsOf(Signals{ProviderImportant: true, AuthFailed: true, Bulk: true})
	if !got.ProviderImportant || !got.AuthFailed || !got.Bulk {
		t.Fatalf("facts = %+v", got)
	}
}

func TestExtraIsTheFourFactsInOrder(t *testing.T) {
	got := MailFacts{WrittenTo: true, AuthFailed: true}.extra()
	if !slices.Equal(got, []float64{0.3, 0, 0.3, 0}) {
		t.Fatalf("extra = %v", got)
	}
}

func TestMailTextIsSenderThenSubject(t *testing.T) {
	for from, want := range map[string]string{
		"a@b.example":               "a@b.example\nOggetto",
		"Mario <Mario@One.example>": "mario@one.example\nOggetto",
		"not an address":            "not an address\nOggetto",
	} {
		if got := mailText(from, "Oggetto"); got != want {
			t.Fatalf("mailText(%q) = %q, want %q: the seeds' bare, lower-case address", from, got, want)
		}
	}
}

func TestJudgeFailsWhileTheSeedsWarm(t *testing.T) {
	embedder := &centroidEmbedder{}
	centroid, _ := newTestCentroid(t, embedder)
	mails := []ShadowMail{{From: "a@b.example", Subject: "Oggetto"}}
	if _, err := centroid.Judge(context.Background(), "id-1", mails); !errors.Is(err, examplebank.ErrSeedsWarming) {
		t.Fatalf("cold judge: %v", err)
	}
	page := []string{mailText("a@b.example", "Oggetto")}
	if slices.ContainsFunc(embedder.embedded(), func(call []string) bool { return slices.Equal(call, page) }) {
		t.Fatal("a cold judge embedded the page it could not answer")
	}
	judged(t, centroid, mails)
}

func TestJudgeAnswersEveryMailOfAPage(t *testing.T) {
	embedder := &centroidEmbedder{}
	centroid, _ := newTestCentroid(t, embedder)
	mails := []ShadowMail{
		{From: "a@one.example", Subject: "Fattura"},
		{From: "b@two.example", Subject: "Udienza domani", Facts: MailFacts{WrittenTo: true, ProviderImportant: true}},
		{From: "c@three.example", Subject: "Offerta", Facts: MailFacts{Bulk: true}},
	}
	answers := judged(t, centroid, mails)
	if len(answers) != 3 {
		t.Fatalf("answers = %+v", answers)
	}
	for _, answer := range answers {
		if !answer.Tier.Valid() || answer.Margin < 0 {
			t.Fatalf("answer = %+v", answer)
		}
	}
	want := []string{"a@one.example\nFattura", "b@two.example\nUdienza domani", "c@three.example\nOfferta"}
	if !slices.ContainsFunc(embedder.embedded(), func(call []string) bool { return slices.Equal(call, want) }) {
		t.Fatalf("no embedding call carried the page's texts in order: %v", want)
	}
}

func TestJudgeOfNoMailEmbedsNothing(t *testing.T) {
	embedder := &centroidEmbedder{}
	centroid, _ := newTestCentroid(t, embedder)
	if answers, err := centroid.Judge(context.Background(), "id-1", nil); err != nil || answers != nil {
		t.Fatalf("judge of nothing = %v, %v", answers, err)
	}
	if calls := embedder.embedded(); len(calls) != 0 {
		t.Fatalf("calls = %v", calls)
	}
}

func TestLearnStoresTheCorrectionAsAnExample(t *testing.T) {
	centroid, store := newTestCentroid(t, &centroidEmbedder{})
	facts := MailFacts{WrittenTo: true, Bulk: true}
	err := centroid.Learn(context.Background(), "id-1", Lesson{
		AccountID: "acc", ItemID: "m1", From: "a@b.example", Subject: "Oggetto", Facts: facts, Tier: TierLater,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.put) != 1 {
		t.Fatalf("put = %+v", store.put)
	}
	got := store.put[0]
	if got.Domain != "mail_tier" || got.Owner != "acc" || got.Ref != "m1" || got.Label != "later" || got.Space != "space-1" {
		t.Fatalf("example = %+v", got)
	}
	if len(got.Vector) != 3+4 || !slices.Equal(got.Vector[3:], facts.extra()) {
		t.Fatalf("vector = %v, want the embedding then %v", got.Vector, facts.extra())
	}
}

func TestAHungStoreCostsForgetAtMostTheTimeout(t *testing.T) {
	centroid, store := newTestCentroid(t, &centroidEmbedder{})
	store.block = true
	centroid.timeout = 50 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	if err := centroid.Forget(ctx, "id-1", "acc"); err == nil || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("forget returned %v after %s", err, time.Since(started))
	}
}

func TestAPageStartsOnlyWithRoomForTheShadow(t *testing.T) {
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	w := &Watcher{Now: func() time.Time { return at }}
	ctx, cancel := context.WithDeadline(context.Background(), at.Add(pimRequestTimeout+shadowTimeout+time.Second))
	defer cancel()
	if w.hasTime(ctx) {
		t.Fatal("a page started with room for the PIM's read and the shadow, but not for the commit")
	}
}

func TestASlowEmbedderCostsAtMostTheTimeout(t *testing.T) {
	embedder := &centroidEmbedder{}
	centroid, _ := newTestCentroid(t, embedder)
	judged(t, centroid, []ShadowMail{{From: "warm@b.example", Subject: "Seeds"}})
	embedder.mu.Lock()
	embedder.block = true
	embedder.mu.Unlock()
	centroid.timeout = 50 * time.Millisecond
	started := time.Now()
	_, err := centroid.Judge(context.Background(), "id-1", []ShadowMail{{From: "a@b.example", Subject: "Oggetto"}})
	if err == nil || time.Since(started) > 500*time.Millisecond {
		t.Fatalf("judge returned %v after %s", err, time.Since(started))
	}
}
