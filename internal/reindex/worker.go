package reindex

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Reindexer is the worker's dependency on the wiki-vector reindex
// implementation. It MUST match search.WikiPageReindexer; declared here
// as a structural interface to avoid an import cycle.
type Reindexer interface {
	ReindexWikiPage(ctx context.Context, slug string) error
}

// Worker owns a buffered job channel and a single drain goroutine. It is
// the canonical Submitter implementation. Lifecycle (D-13, Pitfalls #2/#4/#8):
//   - Dedicated context.Context owned by the Worker.
//   - Stop() cancels the context (cancels in-flight ReindexWikiPage HTTP I/O)
//     and waits on <-done before returning.
//   - The jobs channel is NEVER closed from outside the drain goroutine —
//     GC reclaims it after the last producer releases its reference.
//   - stopped atomic.Bool gates Submit so post-Stop sends are counted
//     distinctly in droppedAfterStop instead of silently disappearing.
type Worker struct {
	jobs      chan Job
	reindexer Reindexer
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}

	stopped          atomic.Bool
	droppedTotal     atomic.Int64
	droppedAfterStop atomic.Int64

	mu          sync.RWMutex
	lastSuccess time.Time
	lastError   string

	logger *slog.Logger
}

// NewWorker constructs a Worker and starts its drain goroutine.
// Returns nil if reindexer is nil (consistent with project's
// "constructor-returns-nil-on-missing-dep" pattern, internal/tools/auth.go:36-41).
func NewWorker(reindexer Reindexer, cfg Config) *Worker {
	if reindexer == nil {
		return nil
	}
	size := cfg.QueueSize
	if size <= 0 {
		size = 100
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &Worker{
		jobs:      make(chan Job, size),
		reindexer: reindexer,
		ctx:       ctx,
		cancel:    cancel,
		done:      make(chan struct{}),
		logger:    slog.Default(),
	}
	go w.drain()
	return w
}

// Submit non-blocking enqueues a job. Returns false if the queue is full
// or the worker has been Stopped. Pitfall #8: post-stop drops are counted
// in a distinct counter (droppedAfterStop) for operational visibility.
func (w *Worker) Submit(j Job) bool {
	if w.stopped.Load() {
		w.droppedAfterStop.Add(1)
		w.logger.Warn("reindex_dropped_after_stop",
			slog.String("slug", j.Slug),
			slog.String("op", j.Op.String()))
		return false
	}
	select {
	case w.jobs <- j:
		return true
	default:
		w.droppedTotal.Add(1)
		w.logger.Warn("reindex_dropped_total",
			slog.String("slug", j.Slug),
			slog.String("op", j.Op.String()))
		return false
	}
}

// Stop cancels the worker's context (aborting any in-flight Reindex) and
// waits for the drain goroutine to exit. After Stop returns, subsequent
// Submit calls are counted in droppedAfterStop. Stop is idempotent.
func (w *Worker) Stop() {
	if !w.stopped.CompareAndSwap(false, true) {
		// Already stopped — wait for the previous Stop's goroutine to finish.
		// w.done is closed by drain(), so this returns immediately.
		<-w.done
		return
	}
	w.cancel()  // Pitfall #2: cancels in-flight ReindexWikiPage HTTP calls
	<-w.done    // wait for drain to exit
	// NEVER close(w.jobs) — Pitfall #4
}

// Health returns an operational snapshot.
func (w *Worker) Health() Health {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return Health{
		QueueDepth:       len(w.jobs),
		Dropped:          w.droppedTotal.Load(),
		DroppedAfterStop: w.droppedAfterStop.Load(),
		LastSuccess:      w.lastSuccess,
		LastError:        w.lastError,
	}
}

// drain is the single goroutine consuming from the jobs channel.
// It exits when the worker's context is cancelled (Stop was called).
// It owns the done channel lifecycle: defer close(w.done) signals Stop() to unblock.
func (w *Worker) drain() {
	defer close(w.done)
	for {
		select {
		case <-w.ctx.Done():
			return
		case j := <-w.jobs:
			w.process(j)
		}
	}
}

// process invokes the reindexer and updates health state under the write lock.
func (w *Worker) process(j Job) {
	err := w.reindexer.ReindexWikiPage(w.ctx, j.Slug)
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		w.lastError = err.Error()
		w.logger.Warn("reindex_failed",
			slog.String("slug", j.Slug),
			slog.String("op", j.Op.String()),
			slog.Any("error", err))
		return
	}
	w.lastSuccess = time.Now()
	w.lastError = ""
}

