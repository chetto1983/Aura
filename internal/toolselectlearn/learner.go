package toolselectlearn

import (
	"context"

	"github.com/chetto1983/aura/internal/activelearn"
	"github.com/chetto1983/aura/internal/semindex"
)

// observeMargin is the synthetic margin the detector enqueues a FLAGGED mis-route
// with. activelearn.Observe only enqueues observations below its margin floor
// (default 0.05); the tool-selection signal is binary (flagged or not), so a flagged
// turn is handed in with margin 0 (definitely "uncertain") and an unflagged turn is
// never enqueued at all (Observe short-circuits before touching activelearn).
const observeMargin = 0.0

// Config wires a Learner. Detector inputs (Ranker + Embedder) and the two-tier
// labeling I/O (Ranker for the free tier, Teacher for the escalation tier, Saver for
// persistence) are supplied by the composition root. A nil Embedder or Saver yields a
// nil Learner (the runner then simply observes nothing).
type Config struct {
	Embedder    semindex.Embedder // embeds the flagged request for the saved exemplar
	Ranker      Ranker            // free ranker: detection top-1 + the free-tier confident label
	Teacher     Teacher           // DeepSeek escalation oracle (kill-switched); nil => free-only
	Saver       Saver             // persists confirmed (query -> tool) to :ToolSelectionExample
	Refresh     func()            // re-folds the per-tool centroids after a save (e.g. ranker.RefreshLearned)
	MarginFloor float64           // activelearn queue margin floor; 0 => default
	Queue       int               // activelearn queue depth; 0 => default
}

// Learner is the tool-selection self-improvement worker. It mirrors
// reasoninglearn.Learner: a thin wrapper over the shared internal/activelearn core,
// supplying only the tool-selection-specific I/O (the mis-route detector, the
// granite embedder, the two-tier oracle, the Neo4j saver). Observe(request, usedTool)
// is the runner's post-turn capture entry — non-blocking, off the hot path.
type Learner struct {
	inner    *activelearn.Learner
	embedder semindex.Embedder
	ranker   Ranker
}

// New starts the worker. Returns nil when the Embedder or Saver is missing (the
// runner then has no learner attached and Observe is a no-op): both are required —
// the embedder turns a flagged request into the exemplar vector, the saver persists
// it. The Teacher may be nil (free-tier-only labeling).
func New(cfg Config) *Learner {
	if cfg.Embedder == nil || cfg.Saver == nil {
		return nil
	}
	inner := activelearn.New(activelearn.Config{
		Oracle: twoTierOracle{
			ranker:  cfg.Ranker,
			teacher: cfg.Teacher,
			saver:   cfg.Saver,
		},
		Refresh:     cfg.Refresh,
		MarginFloor: cfg.MarginFloor,
		Queue:       cfg.Queue,
	})
	return &Learner{inner: inner, embedder: cfg.Embedder, ranker: cfg.Ranker}
}

// Observe is the runner's post-tool-execution capture site (Open-Q #3). It detects a
// mis-route (shell/fs fallback OR used != ranker top-1, symmetric sameTool) and, when
// flagged, embeds the request and enqueues it for async two-tier labeling. It is
// NON-BLOCKING and must NOT abort the turn: an unflagged turn, a nil learner, or an
// embed failure all return silently. The embed happens here (off the user-facing hot
// path — this runs post-turn in persistEvent's best-effort branch) so the activelearn
// core stays embed-free and label-agnostic.
func (l *Learner) Observe(request, usedTool string) {
	if l == nil || l.inner == nil {
		return
	}
	if !isMisroute(request, usedTool, l.ranker) {
		return
	}
	// Embed the flagged request for the saved exemplar. The embed is bounded by the
	// caller's ctx upstream; here we use a background ctx since Observe is fire-and-
	// forget and must not couple the turn's lifetime to the embed. A failure drops the
	// observation (best-effort) — the same request can be re-flagged on a later turn.
	vecs, err := l.embedder.Embed(context.Background(), []string{request})
	if err != nil || len(vecs) != 1 || len(vecs[0]) == 0 {
		return
	}
	l.inner.Observe(request, vecs[0], observeMargin)
}

// Close stops the worker and waits for the in-flight observation to finish
// (goleak-clean). Safe to call multiple times and on a nil Learner.
func (l *Learner) Close() {
	if l == nil {
		return
	}
	l.inner.Close()
}
