package toolselectlearn

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/activelearn"
	"github.com/chetto1983/aura/internal/semindex"
)

// observeMargin is the synthetic margin the detector enqueues a FLAGGED mis-route
// with. activelearn.Observe only enqueues observations below its margin floor
// (default 0.05); the tool-selection signal is binary (flagged or not), so a flagged
// turn is handed in with margin 0 (definitely "uncertain") and an unflagged turn is
// never enqueued at all (the intake worker short-circuits before touching activelearn).
const observeMargin = 0.0

// intakeQueue bounds the raw-signal handoff channel between the turn goroutine and the
// intake worker. A turn never blocks on it: a full queue drops the observation (the
// same request can be re-flagged on a later turn). Matched to activelearn's default
// queue depth so neither stage is the artificial bottleneck.
const intakeQueue = 64

// Config wires a Learner. Detector inputs (Ranker + Embedder) and the two-tier
// labeling I/O (Ranker for the free tier, Teacher for the escalation tier, Saver for
// persistence) are supplied by the composition root. A nil Embedder or Saver yields a
// nil Learner (the runner then simply observes nothing).
type Config struct {
	Embedder       semindex.Embedder // embeds the flagged request for the saved exemplar
	Ranker         Ranker            // free ranker: detection top-1 + the free-tier confident label
	Teacher        Teacher           // DeepSeek escalation oracle (kill-switched); nil => free-only
	Saver          Saver             // persists confirmed (query -> tool) to :ToolSelectionExample
	Refresh        func()            // re-folds the per-tool centroids after a save (e.g. ranker.RefreshLearned)
	MarginFloor    float64           // activelearn queue margin floor; 0 => default
	Queue          int               // activelearn queue depth; 0 => default
	SeenMaxEntries int
	SeenTTL        time.Duration
	Metrics        func(activelearn.LearningMetric)
}

// rawSignal is the opaque (request, usedTool) handoff from the turn goroutine to the
// intake worker. Mis-route detection (which embeds via the ranker) and the exemplar
// embed both run on the worker, NEVER on the turn goroutine (CR-01).
type rawSignal struct {
	request  string
	usedTool string
}

// Learner is the tool-selection self-improvement worker. It mirrors
// reasoninglearn.Learner: a thin wrapper over the shared internal/activelearn core,
// supplying only the tool-selection-specific I/O (the mis-route detector, the
// granite embedder, the two-tier oracle, the Neo4j saver).
//
// Observe(request, usedTool) is the runner's post-turn capture entry. It is a
// NON-BLOCKING handoff (CR-01): it only enqueues the raw signal onto a bounded,
// drop-on-full channel and returns immediately, so the synchronous, lock-held turn
// path never waits on embed/network I/O. A dedicated intake goroutine then runs BOTH
// the mis-route detection (which embeds) AND the exemplar embed off the turn, deriving
// its context from the learner's lifetime so Close cancels any in-flight embed.
type Learner struct {
	inner    *activelearn.Learner
	embedder semindex.Embedder
	ranker   Ranker

	in     chan rawSignal
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once
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
		Refresh:        cfg.Refresh,
		MarginFloor:    cfg.MarginFloor,
		Queue:          cfg.Queue,
		SeenMaxEntries: cfg.SeenMaxEntries,
		SeenTTL:        cfg.SeenTTL,
		Metrics:        cfg.Metrics,
	})
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: cancel is retained on Learner and called by Close.
	l := &Learner{
		inner:    inner,
		embedder: cfg.Embedder,
		ranker:   cfg.Ranker,
		in:       make(chan rawSignal, intakeQueue),
		ctx:      ctx,
		cancel:   cancel,
	}
	l.wg.Add(1)
	go l.runIntake()
	return l
}

// Observe is the runner's post-tool-execution capture site (Open-Q #3). It runs on the
// synchronous, lock-held turn path, so it MUST NOT block: it only hands the raw
// (request, usedTool) signal to the intake worker over a bounded channel and returns.
// A nil learner and a full queue both drop silently (best-effort — the same request can
// be re-flagged on a later turn). NO embed, ranking, or network I/O happens here
// (CR-01: the detect+embed work runs on the intake worker, off the turn goroutine).
func (l *Learner) Observe(request, usedTool string) {
	if l == nil || l.inner == nil {
		return
	}
	if strings.TrimSpace(request) == "" || usedTool == "" {
		return
	}
	select {
	case l.in <- rawSignal{request: request, usedTool: usedTool}:
	default: // intake queue full — drop, never block the turn
	}
}

// runIntake is the dedicated off-turn worker. It drains raw (request, usedTool)
// signals and, for each, runs the embedding work the turn goroutine must not: the
// mis-route detection (isMisroute → ranker, which embeds) and, when flagged, the
// exemplar embed, before handing the labeled-but-unsaved observation to the shared
// activelearn core (which then labels + persists async). All embeds derive l.ctx so
// Close cancels any in-flight round-trip and the worker is reaped goleak-clean.
func (l *Learner) runIntake() {
	defer l.wg.Done()
	for {
		select {
		case <-l.ctx.Done():
			return
		case sig := <-l.in:
			l.detectAndEnqueue(sig)
		}
	}
}

// detectAndEnqueue runs the embedding capture for one raw signal entirely off the turn
// goroutine. An unflagged turn or an embed failure returns silently (best-effort).
func (l *Learner) detectAndEnqueue(sig rawSignal) {
	if !isMisroute(l.ctx, sig.request, sig.usedTool, l.ranker) {
		return
	}
	// Embed the flagged request for the saved exemplar. Bounded by l.ctx (cancelled by
	// Close), so a hung sidecar cannot wedge the worker past shutdown. A failure drops
	// the observation — the same request can be re-flagged on a later turn.
	vecs, err := l.embedder.Embed(l.ctx, []string{sig.request})
	if err != nil || len(vecs) != 1 || len(vecs[0]) == 0 {
		return
	}
	l.inner.Observe(sig.request, vecs[0], observeMargin)
}

// Close stops both the intake worker and the inner activelearn worker and waits for
// in-flight work to finish (goleak-clean). It cancels the shared lifetime ctx (aborting
// any in-flight embed), then joins the intake worker, then closes the inner core. Safe
// to call multiple times and on a nil Learner.
func (l *Learner) Close() {
	if l == nil {
		return
	}
	l.once.Do(l.cancel)
	l.wg.Wait()
	l.inner.Close()
}
