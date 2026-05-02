package summarizer_test

import (
	"context"
	"testing"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/scheduler"
)

// countingScorer counts Score calls and returns fixed candidates.
type countingScorer struct {
	calls      int
	candidates []summarizer.Candidate
}

func (s *countingScorer) Score(_ context.Context, _ []conversation.Turn) ([]summarizer.Candidate, error) {
	s.calls++
	return s.candidates, nil
}

// noopDeduper always returns ActionNew.
type noopDeduper struct{}

func (d *noopDeduper) Deduplicate(_ context.Context, c summarizer.Candidate) (summarizer.Decision, error) {
	return summarizer.Decision{Candidate: c, Action: summarizer.ActionNew}, nil
}

func newRunnerTestArchive(t *testing.T) *conversation.ArchiveStore {
	t.Helper()
	db := scheduler.NewTestDB(t)
	store, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatalf("NewArchiveStore: %v", err)
	}
	return store
}

func seedTurns(t *testing.T, store *conversation.ArchiveStore, chatID int64, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if err := store.Append(context.Background(), conversation.Turn{
			ChatID:    chatID,
			UserID:    1,
			TurnIndex: int64(i),
			Role:      role,
			Content:   "turn content",
		}); err != nil {
			t.Fatalf("seed turn %d: %v", i, err)
		}
	}
}

func TestRunner_TriggersAfterInterval(t *testing.T) {
	archive := newRunnerTestArchive(t)
	scorer := &countingScorer{candidates: []summarizer.Candidate{
		{Fact: "test fact", Score: 0.9, Category: "fact"},
	}}
	deduper := &noopDeduper{}

	cfg := summarizer.RunnerConfig{
		Enabled:        true,
		TurnInterval:   5,
		LookbackTurns:  10,
		CooldownSecs:   60,
	}
	runner := summarizer.NewRunner(cfg, archive, scorer, deduper)

	// Seed 5 turns — interval is 5, so turn_count%5==0 should trigger.
	seedTurns(t, archive, 42, 5)

	triggered, extraction, err := runner.MaybeExtract(context.Background(), 42)
	if err != nil {
		t.Fatalf("MaybeExtract: %v", err)
	}
	if !triggered {
		t.Fatal("want triggered=true after 5 turns")
	}
	if extraction == nil || len(extraction.Decisions) == 0 {
		t.Fatal("want non-empty extraction")
	}
	if scorer.calls != 1 {
		t.Fatalf("want scorer called once, got %d", scorer.calls)
	}
}

func TestRunner_CooldownBlocksRetrigger(t *testing.T) {
	archive := newRunnerTestArchive(t)
	scorer := &countingScorer{candidates: []summarizer.Candidate{
		{Fact: "fact", Score: 0.9, Category: "fact"},
	}}
	deduper := &noopDeduper{}

	cfg := summarizer.RunnerConfig{
		Enabled:       true,
		TurnInterval:  5,
		LookbackTurns: 10,
		CooldownSecs:  60,
	}
	runner := summarizer.NewRunner(cfg, archive, scorer, deduper)
	seedTurns(t, archive, 99, 5)

	// First call should trigger.
	triggered, _, err := runner.MaybeExtract(context.Background(), 99)
	if err != nil {
		t.Fatalf("first MaybeExtract: %v", err)
	}
	if !triggered {
		t.Fatal("want first call to trigger")
	}

	// Immediate second call: cooldown active, should NOT trigger.
	triggered, _, err = runner.MaybeExtract(context.Background(), 99)
	if err != nil {
		t.Fatalf("second MaybeExtract: %v", err)
	}
	if triggered {
		t.Fatal("want second call blocked by cooldown")
	}
	if scorer.calls != 1 {
		t.Fatalf("want scorer called once total, got %d", scorer.calls)
	}
}

func TestRunner_DisabledConfigIsNoop(t *testing.T) {
	archive := newRunnerTestArchive(t)
	scorer := &countingScorer{}
	deduper := &noopDeduper{}

	cfg := summarizer.RunnerConfig{
		Enabled:       false,
		TurnInterval:  5,
		LookbackTurns: 10,
		CooldownSecs:  60,
	}
	runner := summarizer.NewRunner(cfg, archive, scorer, deduper)
	seedTurns(t, archive, 7, 5)

	triggered, extraction, err := runner.MaybeExtract(context.Background(), 7)
	if err != nil {
		t.Fatalf("MaybeExtract: %v", err)
	}
	if triggered {
		t.Fatal("want triggered=false when disabled")
	}
	if extraction != nil {
		t.Fatal("want nil extraction when disabled")
	}
	if scorer.calls != 0 {
		t.Fatalf("want scorer not called, got %d", scorer.calls)
	}
}

func TestRunner_NotTriggeredBelowInterval(t *testing.T) {
	archive := newRunnerTestArchive(t)
	scorer := &countingScorer{}
	deduper := &noopDeduper{}

	cfg := summarizer.RunnerConfig{
		Enabled:       true,
		TurnInterval:  5,
		LookbackTurns: 10,
		CooldownSecs:  60,
	}
	runner := summarizer.NewRunner(cfg, archive, scorer, deduper)
	// Only 3 turns — not at the interval boundary.
	seedTurns(t, archive, 55, 3)

	triggered, _, err := runner.MaybeExtract(context.Background(), 55)
	if err != nil {
		t.Fatalf("MaybeExtract: %v", err)
	}
	if triggered {
		t.Fatal("want triggered=false with only 3 turns (interval=5)")
	}
}

// Ensure CooldownSecs=0 means no cooldown (always triggers when interval met).
func TestRunner_ZeroCooldownAlwaysTriggers(t *testing.T) {
	archive := newRunnerTestArchive(t)
	scorer := &countingScorer{candidates: []summarizer.Candidate{
		{Fact: "fact", Score: 0.9, Category: "fact"},
	}}
	deduper := &noopDeduper{}

	cfg := summarizer.RunnerConfig{
		Enabled:       true,
		TurnInterval:  5,
		LookbackTurns: 10,
		CooldownSecs:  0,
	}
	runner := summarizer.NewRunner(cfg, archive, scorer, deduper)

	// Use a real cooldown-bypassing approach: CooldownSecs=0 means no cooldown.
	// Seed 10 turns so we hit the interval twice.
	seedTurns(t, archive, 33, 10)

	// Set lastRunAt to far in the past to ensure cooldown doesn't block.
	runner.SetLastRunAt(33, time.Now().Add(-200*time.Second))

	triggered, _, err := runner.MaybeExtract(context.Background(), 33)
	if err != nil {
		t.Fatalf("MaybeExtract: %v", err)
	}
	if !triggered {
		t.Fatal("want triggered=true after cooldown window passed")
	}
}
