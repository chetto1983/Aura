package summarizer

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aura/aura/internal/conversation"
)

// TurnArchive is the read side of ArchiveStore needed by the Runner.
type TurnArchive interface {
	ListByChat(ctx context.Context, chatID int64, limit int) ([]conversation.Turn, error)
}

// ScorerI is the scoring interface (avoids collision with the concrete LLMScorer).
type ScorerI interface {
	Score(ctx context.Context, turns []conversation.Turn) ([]Candidate, error)
}

// DeduperI is the dedup interface.
type DeduperI interface {
	Deduplicate(ctx context.Context, c Candidate) (Decision, error)
}

// Extraction is the result of one MaybeExtract run.
type Extraction struct {
	ChatID    int64
	Decisions []Decision
}

// RunnerConfig holds the operational parameters for the Runner.
type RunnerConfig struct {
	Enabled       bool
	TurnInterval  int // extract every N turns (0 = disabled)
	LookbackTurns int // how many recent turns to pass to scorer
	CooldownSecs  int // minimum seconds between extractions per chat (0 = no cooldown)
}

// Runner checks after each turn whether a summarization extraction should run
// and, if so, scores + dedups the last LookbackTurns for the given chat.
// In slice 12e it only logs decisions; applying them is slice 12f's job.
type Runner struct {
	cfg     RunnerConfig
	archive TurnArchive
	scorer  ScorerI
	deduper DeduperI
	logger  *slog.Logger

	mu        sync.Mutex
	lastRunAt map[int64]time.Time
}

// NewRunner constructs a Runner.
func NewRunner(cfg RunnerConfig, archive TurnArchive, scorer ScorerI, deduper DeduperI) *Runner {
	return &Runner{
		cfg:       cfg,
		archive:   archive,
		scorer:    scorer,
		deduper:   deduper,
		logger:    slog.Default(),
		lastRunAt: make(map[int64]time.Time),
	}
}

// SetLastRunAt overrides the cooldown timestamp for chatID. Used in tests.
func (r *Runner) SetLastRunAt(chatID int64, t time.Time) {
	r.mu.Lock()
	r.lastRunAt[chatID] = t
	r.mu.Unlock()
}

// MaybeExtract runs salience scoring + dedup for chatID if the interval and
// cooldown conditions are met. Returns (false, nil, nil) when skipped.
func (r *Runner) MaybeExtract(ctx context.Context, chatID int64) (bool, *Extraction, error) {
	if !r.cfg.Enabled || r.cfg.TurnInterval <= 0 {
		return false, nil, nil
	}

	// Count turns for this chat to check the interval.
	turns, err := r.archive.ListByChat(ctx, chatID, 100000)
	if err != nil {
		return false, nil, fmt.Errorf("runner list turns: %w", err)
	}
	if len(turns) == 0 || len(turns)%r.cfg.TurnInterval != 0 {
		return false, nil, nil
	}

	// Check cooldown.
	if r.cfg.CooldownSecs > 0 {
		r.mu.Lock()
		last := r.lastRunAt[chatID]
		r.mu.Unlock()
		if !last.IsZero() && time.Since(last) < time.Duration(r.cfg.CooldownSecs)*time.Second {
			return false, nil, nil
		}
	}

	// Record start time before the LLM call.
	r.mu.Lock()
	r.lastRunAt[chatID] = time.Now()
	r.mu.Unlock()

	// Pull the lookback window (most recent N turns, reversed to chronological).
	lookback := r.cfg.LookbackTurns
	if lookback <= 0 {
		lookback = 10
	}
	recentTurns, err := r.archive.ListByChat(ctx, chatID, lookback)
	if err != nil {
		return false, nil, fmt.Errorf("runner lookback: %w", err)
	}
	// ListByChat returns newest-first; reverse to chronological for scorer.
	for i, j := 0, len(recentTurns)-1; i < j; i, j = i+1, j-1 {
		recentTurns[i], recentTurns[j] = recentTurns[j], recentTurns[i]
	}

	candidates, err := r.scorer.Score(ctx, recentTurns)
	if err != nil {
		return false, nil, fmt.Errorf("runner score: %w", err)
	}

	decisions := make([]Decision, 0, len(candidates))
	for _, c := range candidates {
		dec, err := r.deduper.Deduplicate(ctx, c)
		if err != nil {
			r.logger.Warn("runner dedup failed", "fact", c.Fact, "error", err)
			continue
		}
		decisions = append(decisions, dec)
		// Slice 12e: log only; apply paths land in 12f.
		r.logger.Info("summarizer decision",
			"chat_id", chatID,
			"action", dec.Action,
			"fact", dec.Candidate.Fact,
			"score", dec.Candidate.Score,
			"similarity", dec.Similarity,
			"target_slug", dec.TargetSlug,
		)
	}

	return true, &Extraction{ChatID: chatID, Decisions: decisions}, nil
}
