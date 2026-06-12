// Package reasoninglearn is the async self-improvement worker for the reasoning
// classifier (spike 053). It observes the classifier's uncertain (low-margin)
// turns, labels them with an LLM oracle off the hot path, and saves the
// (embedding, tier) example so the classifier converges toward the oracle's
// accuracy over time — without ever adding latency to a user turn.
//
// The queue/dedup/margin/drop-on-full/goleak-clean mechanism lives in the shared
// label-agnostic internal/activelearn core; this package supplies only the
// reasoning-specific I/O (the ReasoningTier Oracle + the Neo4j Saver) and keeps
// its existing prompt.Learner-satisfying Observe and its New(cfg) signature so
// the composition root and the live E2E are unchanged (D-05, behavior-preserving).
package reasoninglearn

import (
	"context"

	"github.com/chetto1983/aura/internal/activelearn"
	"github.com/chetto1983/aura/internal/agent/prompt"
)

// Oracle labels a turn with a reasoning tier (the LLM router used as teacher).
type Oracle interface {
	Label(ctx context.Context, text string) (prompt.ReasoningTier, bool)
}

// Saver persists one oracle-labeled example (idempotent by content hash).
type Saver interface {
	Save(ctx context.Context, text string, vec []float64, tier prompt.ReasoningTier) error
}

// Config wires a Learner. Oracle and Saver are required; the rest have defaults.
type Config struct {
	Oracle      Oracle
	Saver       Saver
	Refresh     func() // called after a successful save (e.g. classifier.Refresh)
	MarginFloor float64
	Queue       int
}

// Learner implements prompt.Learner. It delegates the bounded-queue/dedup/margin/
// drop-on-full/goleak-clean mechanism to internal/activelearn, supplying a
// reasoning-specific oracle that labels a ReasoningTier and persists it. Observe
// never blocks the caller.
type Learner struct {
	inner *activelearn.Learner
}

// reasoningOracle adapts the reasoning Oracle+Saver into the label-agnostic
// activelearn.Oracle: it labels the text with a tier, validates it, and persists
// the example, reporting saved=true only when both committed. A label miss, an
// invalid tier, or a save error returns saved=false so activelearn deletes the
// content hash and a later attempt can retry the transient failure.
type reasoningOracle struct {
	oracle Oracle
	saver  Saver
}

func (o reasoningOracle) LabelAndSave(ctx context.Context, text string, vec []float64) bool {
	tier, ok := o.oracle.Label(ctx, text)
	if !ok || !tier.Valid() {
		return false
	}
	return o.saver.Save(ctx, text, vec, tier) == nil
}

// New starts the worker. Returns nil if oracle or saver is missing (the
// classifier then simply has no learner attached).
func New(cfg Config) *Learner {
	if cfg.Oracle == nil || cfg.Saver == nil {
		return nil
	}
	inner := activelearn.New(activelearn.Config{
		Oracle:      reasoningOracle{oracle: cfg.Oracle, saver: cfg.Saver},
		Refresh:     cfg.Refresh,
		MarginFloor: cfg.MarginFloor,
		Queue:       cfg.Queue,
	})
	return &Learner{inner: inner}
}

// Observe enqueues an uncertain classification for async labeling. It is
// non-blocking: above the margin floor, already-seen, or queue-full inputs are
// dropped silently so the turn's hot path is never delayed.
func (l *Learner) Observe(text string, vec []float64, margin float64) {
	if l == nil {
		return
	}
	l.inner.Observe(text, vec, margin)
}

// Close stops the worker and waits for the in-flight observation to finish
// (goleak-clean). Safe to call multiple times.
func (l *Learner) Close() {
	if l == nil {
		return
	}
	l.inner.Close()
}
