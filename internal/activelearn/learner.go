package activelearn

import (
	"context"
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/neostore"
)

// Learner is the label-agnostic async self-improvement worker. One bounded
// background goroutine drains observations; Observe never blocks the caller.
type Learner struct {
	oracle  Oracle
	refresh func()
	floor   float64

	ch     chan observation
	seen   SeenSet // content-hash only; bounded/expiring policy is wired by New
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once
}

type observation struct {
	text string
	vec  []float64
}

// New starts the worker. Returns nil if the oracle is missing (the consumer then
// simply has no learner attached and observes nothing).
func New(cfg Config) *Learner {
	if cfg.Oracle == nil {
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
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: cancel is retained on Learner and called by Close.
	l := &Learner{
		oracle:  cfg.Oracle,
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
	if _, dup := l.seen.Load(neostore.HashText(text)); dup {
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
	h := neostore.HashText(o.text)
	if _, dup := l.seen.LoadOrStore(h, struct{}{}); dup {
		return
	}
	if !l.oracle.LabelAndSave(l.ctx, o.text, o.vec) {
		l.seen.Delete(h) // let a later attempt retry a transient oracle/save failure
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
