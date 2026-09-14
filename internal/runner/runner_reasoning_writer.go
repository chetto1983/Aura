package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/redact"
)

var (
	errReasoningTraceWriterClosed = errors.New("reasoning trace writer is closed")
	errReasoningTraceQueueFull    = errors.New("reasoning trace queue is full")
)

const (
	reasoningTraceQueueCapacity = 64
	// reasoningTraceWriteTimeout covers the summary's embedding and the graph transaction.
	// Both used to share 5 s inside turn completion, and a 2,048-token summary alone took
	// 5.7 s to embed on the appliance (measured 2026-09-14, prd.md §10).
	reasoningTraceWriteTimeout = 30 * time.Second
)

// ReasoningTraceStore is the identity-scoped graph a ReasoningTraceWriter fronts.
type ReasoningTraceStore interface {
	ReasoningGraphSink
	ReasoningDeletionStore
}

// ReasoningTraceWriter takes trace persistence out of turn completion: a trace is queued
// and one ordered worker embeds and stores it. It is also the deletion path, because a
// queued trace written after its source was deleted would strand derived reasoning the
// delete lifecycle exists to remove; a delete therefore waits for the traces it covers.
//
// The worker is lazy and exits whenever the queue drains, so an idle writer holds no
// goroutine.
type ReasoningTraceWriter struct {
	store ReasoningTraceStore

	mu        sync.Mutex
	pending   []queuedReasoningTrace
	unwritten map[reasoningTraceSource]int
	working   bool
	closed    bool
	changed   chan struct{}
}

// queuedReasoningTrace keeps the offering turn's context values (its trace span among
// them) but not its cancellation: the turn has already returned when the write runs.
type queuedReasoningTrace struct {
	ctx   context.Context
	trace arcadedb.ReasoningTrace
}

// reasoningTraceSource keys the traces a delete must wait for: queued or being written.
type reasoningTraceSource struct {
	identityID     string
	conversationID string
}

// NewReasoningTraceWriter builds a writer in front of store.
func NewReasoningTraceWriter(store ReasoningTraceStore) *ReasoningTraceWriter {
	return &ReasoningTraceWriter{
		store:     store,
		unwritten: make(map[reasoningTraceSource]int),
		changed:   make(chan struct{}),
	}
}

// UpsertReasoningTrace queues trace and returns without waiting for the graph. The write
// runs under its own deadline, so the caller's cancellation and deadline do not bound it.
func (w *ReasoningTraceWriter) UpsertReasoningTrace(ctx context.Context, trace arcadedb.ReasoningTrace) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errReasoningTraceWriterClosed
	}
	if len(w.pending) >= reasoningTraceQueueCapacity {
		return errReasoningTraceQueueFull
	}
	w.pending = append(w.pending, queuedReasoningTrace{ctx: context.WithoutCancel(ctx), trace: trace})
	w.unwritten[sourceOf(trace)]++
	if !w.working {
		w.working = true
		go w.run()
	}
	return nil
}

// DeleteReasoningBySource waits until no trace the selector covers is queued or being
// written, then deletes. A selector without a conversation covers the whole identity.
func (w *ReasoningTraceWriter) DeleteReasoningBySource(
	ctx context.Context,
	selector arcadedb.ReasoningDeleteSelector,
) (int, error) {
	for {
		w.mu.Lock()
		covered, changed := w.coversUnwrittenLocked(selector), w.changed
		w.mu.Unlock()
		if !covered {
			return w.store.DeleteReasoningBySource(ctx, selector)
		}
		select {
		case <-ctx.Done():
			return 0, fmt.Errorf("wait for queued reasoning traces: %w", ctx.Err())
		case <-changed:
		}
	}
}

// Close stops admission and waits for every queued trace to be written.
func (w *ReasoningTraceWriter) Close(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	for {
		w.mu.Lock()
		working, changed := w.working, w.changed
		w.mu.Unlock()
		if !working {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("close reasoning trace writer: %w", ctx.Err())
		case <-changed:
		}
	}
}

func (w *ReasoningTraceWriter) run() {
	for {
		w.mu.Lock()
		if len(w.pending) == 0 {
			w.working = false
			w.signalLocked()
			w.mu.Unlock()
			return
		}
		queued := w.pending[0]
		w.pending[0] = queuedReasoningTrace{}
		w.pending = w.pending[1:]
		w.mu.Unlock()

		ctx, cancel := context.WithTimeout(queued.ctx, reasoningTraceWriteTimeout)
		err := w.store.UpsertReasoningTrace(ctx, queued.trace)
		cancel()
		if err != nil {
			slog.Warn("reasoning graph delivery failed after answer commit", "err", redact.Line(err.Error()))
		}

		w.mu.Lock()
		source := sourceOf(queued.trace)
		if w.unwritten[source]--; w.unwritten[source] == 0 {
			delete(w.unwritten, source)
		}
		w.signalLocked()
		w.mu.Unlock()
	}
}

func (w *ReasoningTraceWriter) coversUnwrittenLocked(selector arcadedb.ReasoningDeleteSelector) bool {
	for source := range w.unwritten {
		if source.identityID == selector.IdentityID &&
			(selector.ConversationID == "" || source.conversationID == selector.ConversationID) {
			return true
		}
	}
	return false
}

func (w *ReasoningTraceWriter) signalLocked() {
	close(w.changed)
	w.changed = make(chan struct{})
}

func sourceOf(trace arcadedb.ReasoningTrace) reasoningTraceSource {
	return reasoningTraceSource{identityID: trace.IdentityID, conversationID: trace.ConversationID}
}
