// Package activelearn is the label-agnostic async self-improvement mechanism
// extracted from the shipped reasoning learner (spike 053). It observes a
// consumer's uncertain (low-margin) turns, hands each off the hot path to a
// consumer-supplied Oracle that labels AND persists it, then fires an optional
// refresh — so a consumer's classifier converges toward its oracle's accuracy
// over time without ever adding latency to a user turn.
//
// The mechanism is the genuinely-shared part (bounded queue + sha256 content-hash
// dedup + sync.Map seen-set + margin gate + non-blocking drop-on-full + one
// bounded goroutine + goleak-clean Close). The observation is OPAQUE: the core
// imports neither a label type nor any consumer package. Each consumer (the
// reasoning-tier learner, the future tool-selection learner) supplies an Oracle
// that owns its own label assignment and storage. This honors REUSABLE-CODE for
// the mechanism while the divergent stores/oracles stay specialized (D-05).
package activelearn

import (
	"context"
	"time"
)

// Oracle labels AND persists one observed turn, off the hot path. It owns the
// whole opaque "assign a label + save the example" step so the core never sees a
// label type — the reasoning learner's oracle labels a ReasoningTier and saves to
// :ReasoningExample; a tool-selection oracle would label a tool name and save to
// :ToolSelectionExample. LabelAndSave reports whether the example was committed:
//   - true  → the example is persisted; the core keeps the content hash in its
//     seen-set so the same text is never relabeled.
//   - false → labeling or saving did not commit (e.g. a transient oracle/store
//     failure, or the oracle declined the label); the core removes the content
//     hash so a later observation of the same text can retry.
//
// LabelAndSave runs on the single background worker, so it may block on I/O
// (the LLM teacher, Neo4j) without ever touching the caller's turn.
type Oracle interface {
	LabelAndSave(ctx context.Context, text string, vec []float64) (saved bool)
}

// Config wires a Learner. Oracle is required; the rest have defaults.
type Config struct {
	Oracle         Oracle
	Refresh        func() // called after a successful LabelAndSave (e.g. classifier.Refresh)
	MarginFloor    float64
	Queue          int
	SeenMaxEntries int
	SeenTTL        time.Duration
	Now            func() time.Time
	Metrics        func(LearningMetric)
}

// LearningMetric is a bounded-label capacity event emitted by the learner.
type LearningMetric struct {
	Operation  string
	Outcome    string
	State      string
	ErrorClass string
	Size       int
	OldestAge  time.Duration
}

const (
	defaultMarginFloor = 0.05
	defaultQueue       = 64
)
