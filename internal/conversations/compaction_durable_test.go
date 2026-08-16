package conversations

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// fakeCompactionCache is the durable half in memory, and it counts writes: the point of
// the feature is that the summarizer and the writer both go quiet once the watermark is
// current, so "was it called" is the assertion, not just "was the text right".
type fakeCompactionCache struct {
	stored  Compaction
	present bool
	loadErr error
	saveErr error
	saves   int
	saved   Compaction
}

func (f *fakeCompactionCache) LoadCompaction(_ context.Context, _, _ string) (Compaction, bool, error) {
	if f.loadErr != nil {
		return Compaction{}, false, f.loadErr
	}
	return f.stored, f.present, nil
}

func (f *fakeCompactionCache) SaveCompaction(_ context.Context, _, _ string, c Compaction) error {
	f.saves++
	f.saved = c
	return f.saveErr
}

func historyTurns(seqs ...int) []Turn {
	out := make([]Turn, 0, len(seqs))
	for _, seq := range seqs {
		role := llm.RoleUser
		if seq%2 == 0 {
			role = llm.RoleAssistant
		}
		out = append(out, Turn{Seq: seq, Role: role, Content: "turn content " + string(rune('a'+seq%26))})
	}
	return out
}

// The whole economic argument: a conversation whose watermark is current pays nothing.
// Before this, every turn over budget paid an auxiliary LLM call to re-summarize the same
// history, and the regenerated wording moved the prompt prefix each time.
func TestCompactionSummaryReusesTheStoredSummaryWithoutCallingTheModel(t *testing.T) {
	sum := &fakeSummarizer{summary: "fresh"}
	cache := &fakeCompactionCache{present: true, stored: Compaction{
		Summary: "stored summary", CoversThroughSeq: 12,
	}}

	got, ok := compactionSummary(context.Background(), sum, cache, "conv", "", historyTurns(8, 10, 12))

	if !ok || got != "stored summary" {
		t.Fatalf("summary = (%q, %v), want the stored text", got, ok)
	}
	if sum.calls != 0 {
		t.Fatalf("summarizer called %d times, want 0 when the watermark already covers the slice", sum.calls)
	}
	if cache.saves != 0 {
		t.Fatalf("cache written %d times, want 0 when nothing changed", cache.saves)
	}
}

// The iterative update: only what came after the watermark is re-read, and the previous
// summary rides along as source material so an earlier distillation is not lost.
func TestCompactionSummaryFoldsOnlyTheNewTurnsIntoTheStoredSummary(t *testing.T) {
	sum := &fakeSummarizer{summary: "merged summary"}
	cache := &fakeCompactionCache{present: true, stored: Compaction{
		Summary: "stored summary", CoversThroughSeq: 10,
	}}

	got, ok := compactionSummary(context.Background(), sum, cache, "conv", "", historyTurns(8, 10, 12, 14))

	if !ok || got != "merged summary" {
		t.Fatalf("summary = (%q, %v)", got, ok)
	}
	if sum.calls != 1 {
		t.Fatalf("summarizer called %d times, want exactly 1", sum.calls)
	}
	if len(sum.gotRounds) != 3 {
		t.Fatalf("summarizer got %d messages, want the carried summary plus the two new turns", len(sum.gotRounds))
	}
	if !strings.Contains(sum.gotRounds[0].Content, "stored summary") {
		t.Fatalf("first message = %q, want the previous summary carried in", sum.gotRounds[0].Content)
	}
	if cache.saved.CoversThroughSeq != 14 || cache.saved.SourceTurns != 2 {
		t.Fatalf("saved = %+v, want the watermark advanced to 14 over 2 new turns", cache.saved)
	}
}

func TestCompactionSummaryWithoutACacheBehavesAsBefore(t *testing.T) {
	sum := &fakeSummarizer{summary: "fresh"}

	got, ok := compactionSummary(context.Background(), sum, nil, "conv", "", historyTurns(2, 4))

	if !ok || got != "fresh" {
		t.Fatalf("summary = (%q, %v)", got, ok)
	}
	if sum.calls != 1 || len(sum.gotRounds) != 2 {
		t.Fatalf("summarizer calls=%d rounds=%d, want one call over the whole slice", sum.calls, len(sum.gotRounds))
	}
}

// A summarizer that fails must not throw away a summary that is already on disk: it still
// describes the turns it covers, and L2.5 remains free to drop whatever it must.
func TestCompactionSummaryKeepsTheStoredSummaryWhenTheModelFails(t *testing.T) {
	sum := &fakeSummarizer{err: errors.New("upstream down")}
	cache := &fakeCompactionCache{present: true, stored: Compaction{
		Summary: "stored summary", CoversThroughSeq: 4,
	}}

	got, ok := compactionSummary(context.Background(), sum, cache, "conv", "", historyTurns(4, 6))

	if !ok || got != "stored summary" {
		t.Fatalf("summary = (%q, %v), want the stored text to survive a failed refresh", got, ok)
	}
}

func TestCompactionSummaryFailsClosedWithNothingStored(t *testing.T) {
	sum := &fakeSummarizer{err: errors.New("upstream down")}
	cache := &fakeCompactionCache{}

	if got, ok := compactionSummary(context.Background(), sum, cache, "conv", "", historyTurns(2)); ok {
		t.Fatalf("summary = %q, want no compaction so the ladder falls through to L2.5", got)
	}
}

// An unreadable cache is a cost, never a correctness problem: it degrades to exactly the
// behaviour that shipped before durability existed.
func TestCompactionSummarySurvivesAnUnreadableCache(t *testing.T) {
	sum := &fakeSummarizer{summary: "fresh"}
	cache := &fakeCompactionCache{loadErr: errors.New("db unavailable")}

	got, ok := compactionSummary(context.Background(), sum, cache, "conv", "", historyTurns(2, 4))

	if !ok || got != "fresh" || sum.calls != 1 {
		t.Fatalf("summary = (%q, %v) after %d calls, want a normal fresh summarization", got, ok, sum.calls)
	}
}

// The fixed cost of a request -- the tool manifest -- is already on the wire whatever the
// ladder does, so the trigger spends the percentage on the history that is LEFT. Without
// this the number means "a share of the part I happen to count", which on this deployment
// was half the request.
func TestEarlyCompactionTokensSubtractsTheFixedOverhead(t *testing.T) {
	cfg := ContextConfig{
		ContextWindow: 100_000, CompactionTriggerPercent: 30,
		FixedOverheadTokens: 19_000, Summarizer: &fakeSummarizer{},
	}
	if got := cfg.earlyCompactionTokens(); got != 11_000 {
		t.Fatalf("earlyCompactionTokens() = %d, want 30%% of the window minus the manifest", got)
	}

	// An overhead that has already eaten the allowance disables the trigger rather than
	// compacting on every turn to chase a budget compaction cannot recover.
	cfg.FixedOverheadTokens = 40_000
	if got := cfg.earlyCompactionTokens(); got != 0 {
		t.Fatalf("earlyCompactionTokens() = %d, want 0 when the fixed cost exceeds the budget", got)
	}
}

func TestEarlyCompactionTokensIsAShareOfTheWindow(t *testing.T) {
	for name, tc := range map[string]struct {
		window, percent int
		summarizer      Summarizer
		want            int
	}{
		"half":              {200_000, 50, &fakeSummarizer{}, 100_000},
		"quarter":           {200_000, 25, &fakeSummarizer{}, 50_000},
		"disabled at zero":  {200_000, 0, &fakeSummarizer{}, 0},
		"disabled at 100":   {200_000, 100, &fakeSummarizer{}, 0},
		"no summarizer":     {200_000, 50, nil, 0},
		"degenerate window": {0, 50, &fakeSummarizer{}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := ContextConfig{
				ContextWindow: tc.window, CompactionTriggerPercent: tc.percent, Summarizer: tc.summarizer,
			}
			if got := cfg.earlyCompactionTokens(); got != tc.want {
				t.Fatalf("earlyCompactionTokens() = %d, want %d", got, tc.want)
			}
		})
	}
}
