// Package reasoninglearn is the async self-improvement worker for the reasoning
// classifier (spike 053). It observes the classifier's uncertain (low-margin)
// turns, labels them with an LLM oracle off the hot path, and saves the
// (embedding, tier) example so the classifier converges toward the oracle's
// accuracy over time — without ever adding latency to a user turn.
package reasoninglearn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"

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

const (
	defaultMarginFloor = 0.05
	defaultQueue       = 64
)

// Learner implements prompt.Learner. One bounded background goroutine drains
// observations; Observe never blocks the caller.
type Learner struct {
	oracle  Oracle
	saver   Saver
	refresh func()
	floor   float64

	ch     chan observation
	seen   sync.Map // content-hash -> struct{}; one label per unique text, ever
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once
}

type observation struct {
	text string
	vec  []float64
}

// New starts the worker. Returns nil if oracle or saver is missing (the
// classifier then simply has no learner attached).
func New(cfg Config) *Learner {
	if cfg.Oracle == nil || cfg.Saver == nil {
		return nil
	}
	floor := cfg.MarginFloor
	if floor <= 0 {
		floor = defaultMarginFloor
	}
	q := cfg.Queue
	if q <= 0 {
		q = defaultQueue
	}
	ctx, cancel := context.WithCancel(context.Background())
	l := &Learner{
		oracle:  cfg.Oracle,
		saver:   cfg.Saver,
		refresh: cfg.Refresh,
		floor:   floor,
		ch:      make(chan observation, q),
		ctx:     ctx,
		cancel:  cancel,
	}
	l.wg.Add(1)
	go l.run()
	return l
}

// Observe enqueues an uncertain classification for async labeling. It is
// non-blocking: above the margin floor, already-seen, or queue-full inputs are
// dropped silently so the turn's hot path is never delayed.
func (l *Learner) Observe(text string, vec []float64, margin float64) {
	if l == nil || margin >= l.floor || strings.TrimSpace(text) == "" {
		return
	}
	if _, dup := l.seen.Load(hashText(text)); dup {
		return
	}
	cp := make([]float64, len(vec))
	copy(cp, vec)
	select {
	case l.ch <- observation{text: text, vec: cp}:
	default: // queue full — drop, never block the turn
	}
}

func (l *Learner) run() {
	defer l.wg.Done()
	for {
		select {
		case <-l.ctx.Done():
			return
		case o := <-l.ch:
			l.process(o)
		}
	}
}

func (l *Learner) process(o observation) {
	h := hashText(o.text)
	if _, dup := l.seen.LoadOrStore(h, struct{}{}); dup {
		return
	}
	tier, ok := l.oracle.Label(l.ctx, o.text)
	if !ok || !tier.Valid() {
		l.seen.Delete(h) // let a later attempt retry a transient oracle failure
		return
	}
	if err := l.saver.Save(l.ctx, o.text, o.vec, tier); err != nil {
		l.seen.Delete(h)
		return
	}
	if l.refresh != nil {
		l.refresh()
	}
}

// Close stops the worker and waits for the in-flight observation to finish
// (goleak-clean). Safe to call multiple times.
func (l *Learner) Close() {
	if l == nil {
		return
	}
	l.once.Do(l.cancel)
	l.wg.Wait()
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
