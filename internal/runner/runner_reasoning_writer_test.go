package runner

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

// gatedReasoningStore holds every write until release is closed and records the order in
// which writes and deletes reach the graph.
type gatedReasoningStore struct {
	release  chan struct{}
	entered  chan struct{}
	writeErr error

	mu       sync.Mutex
	events   []string
	deadline []bool
	canceled []bool
}

func newGatedReasoningStore() *gatedReasoningStore {
	return &gatedReasoningStore{release: make(chan struct{}), entered: make(chan struct{}, 256)}
}

func (s *gatedReasoningStore) UpsertReasoningTrace(ctx context.Context, trace arcadedb.ReasoningTrace) error {
	s.entered <- struct{}{}
	<-s.release
	_, hasDeadline := ctx.Deadline()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, "write "+trace.ConversationID)
	s.deadline = append(s.deadline, hasDeadline)
	s.canceled = append(s.canceled, ctx.Err() != nil)
	return s.writeErr
}

func (s *gatedReasoningStore) DeleteReasoningBySource(
	_ context.Context, selector arcadedb.ReasoningDeleteSelector,
) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, "delete "+selector.ConversationID)
	return 1, nil
}

func (s *gatedReasoningStore) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...)
}

func traceFor(identityID, conversationID string) arcadedb.ReasoningTrace {
	return arcadedb.ReasoningTrace{IdentityID: identityID, ConversationID: conversationID, TraceID: uuid.NewString()}
}

// closeWriter releases the store and drains the writer, so no worker outlives the test.
func closeWriter(t *testing.T, writer *ReasoningTraceWriter, store *gatedReasoningStore) {
	t.Helper()
	select {
	case <-store.release:
	default:
		close(store.release)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := writer.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// within fails the test instead of hanging it when fn blocks.
func within(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s blocked", what)
	}
}

func TestReasoningTraceWriterDoesNotHoldTheCallerForTheGraphWrite(t *testing.T) {
	store := newGatedReasoningStore()
	writer := NewReasoningTraceWriter(store)
	within(t, "queueing a trace while the graph write is pending", func() {
		if err := writer.UpsertReasoningTrace(t.Context(), traceFor("id", "conv")); err != nil {
			t.Errorf("UpsertReasoningTrace: %v", err)
		}
	})
	closeWriter(t, writer, store)
	if got := strings.Join(store.recorded(), ","); got != "write conv" {
		t.Fatalf("graph events = %q, want the queued trace written on close", got)
	}
}

func TestReasoningTraceWriterDeleteWaitsForItsConversationsTraces(t *testing.T) {
	store := newGatedReasoningStore()
	writer := NewReasoningTraceWriter(store)
	for range 2 {
		if err := writer.UpsertReasoningTrace(t.Context(), traceFor("id", "conv")); err != nil {
			t.Fatalf("UpsertReasoningTrace: %v", err)
		}
	}
	deleted := make(chan error, 1)
	go func() {
		_, err := writer.DeleteReasoningBySource(t.Context(),
			arcadedb.ReasoningDeleteSelector{IdentityID: "id", ConversationID: "conv"})
		deleted <- err
	}()
	// A delete that did not wait reaches the graph at once, before the held writes.
	time.Sleep(50 * time.Millisecond)
	close(store.release)
	if err := <-deleted; err != nil {
		t.Fatalf("DeleteReasoningBySource: %v", err)
	}
	closeWriter(t, writer, store)
	if got := strings.Join(store.recorded(), ","); got != "write conv,write conv,delete conv" {
		t.Fatalf("graph events = %q, want both traces written before the delete", got)
	}
}

func TestReasoningTraceWriterDeleteWaitsOnlyForMatchingSources(t *testing.T) {
	store := newGatedReasoningStore()
	writer := NewReasoningTraceWriter(store)
	defer closeWriter(t, writer, store)
	if err := writer.UpsertReasoningTrace(t.Context(), traceFor("id", "busy")); err != nil {
		t.Fatalf("UpsertReasoningTrace: %v", err)
	}
	for _, selector := range []arcadedb.ReasoningDeleteSelector{
		{IdentityID: "id", ConversationID: "other"},
		{IdentityID: "someone-else"},
	} {
		within(t, "deleting an unrelated source", func() {
			if _, err := writer.DeleteReasoningBySource(t.Context(), selector); err != nil {
				t.Errorf("DeleteReasoningBySource(%+v): %v", selector, err)
			}
		})
	}
	// An identity-wide selector covers the busy conversation, so it must wait.
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := writer.DeleteReasoningBySource(ctx, arcadedb.ReasoningDeleteSelector{IdentityID: "id"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("identity-wide delete = %v, want it to wait for the busy conversation", err)
	}
}

func TestReasoningTraceWriterDeleteGivesUpWithItsContext(t *testing.T) {
	store := newGatedReasoningStore()
	writer := NewReasoningTraceWriter(store)
	if err := writer.UpsertReasoningTrace(t.Context(), traceFor("id", "conv")); err != nil {
		t.Fatalf("UpsertReasoningTrace: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	_, err := writer.DeleteReasoningBySource(ctx, arcadedb.ReasoningDeleteSelector{IdentityID: "id", ConversationID: "conv"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("delete error = %v, want the context deadline", err)
	}
	closeWriter(t, writer, store)
	if got := strings.Join(store.recorded(), ","); got != "write conv" {
		t.Fatalf("graph events = %q, want no delete after giving up", got)
	}
}

func TestReasoningTraceWriterWritesUnderItsOwnDeadline(t *testing.T) {
	store := newGatedReasoningStore()
	writer := NewReasoningTraceWriter(store)
	callerCtx, cancel := context.WithCancel(t.Context())
	if err := writer.UpsertReasoningTrace(callerCtx, traceFor("id", "conv")); err != nil {
		t.Fatalf("UpsertReasoningTrace: %v", err)
	}
	cancel()
	closeWriter(t, writer, store)
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.deadline) != 1 || !store.deadline[0] || store.canceled[0] {
		t.Fatalf("write deadline/canceled = %v/%v, want a live context with its own deadline",
			store.deadline, store.canceled)
	}
}

func TestReasoningTraceWriterKeepsWritingAfterAFailure(t *testing.T) {
	store := newGatedReasoningStore()
	store.writeErr = errors.New("graph unavailable")
	writer := NewReasoningTraceWriter(store)
	close(store.release)
	for _, conversation := range []string{"first", "second"} {
		if err := writer.UpsertReasoningTrace(t.Context(), traceFor("id", conversation)); err != nil {
			t.Fatalf("UpsertReasoningTrace: %v", err)
		}
	}
	closeWriter(t, writer, store)
	if got := strings.Join(store.recorded(), ","); got != "write first,write second" {
		t.Fatalf("graph events = %q, want the second trace attempted after the first failed", got)
	}
}

func TestReasoningTraceWriterBoundsItsQueueAndRefusesAfterClose(t *testing.T) {
	store := newGatedReasoningStore()
	writer := NewReasoningTraceWriter(store)
	// One trace is taken by the worker and held at the store; the rest fill the queue.
	accepted := 0
	for range reasoningTraceQueueCapacity + 2 {
		if err := writer.UpsertReasoningTrace(t.Context(), traceFor("id", "conv")); err != nil {
			if !errors.Is(err, errReasoningTraceQueueFull) {
				t.Fatalf("overflow error = %v", err)
			}
			break
		}
		accepted++
		if accepted == 1 {
			<-store.entered
		}
	}
	if accepted != reasoningTraceQueueCapacity+1 {
		t.Fatalf("accepted %d traces, want %d queued plus the one being written",
			accepted, reasoningTraceQueueCapacity+1)
	}
	closeWriter(t, writer, store)
	if len(store.recorded()) != accepted {
		t.Fatalf("wrote %d of %d accepted traces before close returned", len(store.recorded()), accepted)
	}
	if err := writer.UpsertReasoningTrace(t.Context(), traceFor("id", "conv")); !errors.Is(err, errReasoningTraceWriterClosed) {
		t.Fatalf("upsert after close = %v, want the closed error", err)
	}
}

// The turn's answer must be complete while its trace is still waiting on the graph.
func TestRunnerCompletesTheTurnWhileItsTraceIsQueued(t *testing.T) {
	r, base := newReasoningTestRunner(t, 65536, true)
	store := newGatedReasoningStore()
	writer := NewReasoningTraceWriter(store)
	r.reasoningGraphSink = writer
	convID := newConvID(t)
	ctx := identityctx.WithIdentityID(t.Context(), uuid.NewString())
	tr := &turnTracker{convID: convID, llmRuntime: r.llmSnapshot(ctx)}
	runID := uuid.Must(uuid.NewV7())
	now := time.Now().UTC()
	within(t, "completing the turn", func() {
		for _, ev := range []*agent.Event{
			reasoningGraphEvent(runID, now, "Check the queue."),
			reasoningGraphFinalEvent(runID, now.Add(time.Second), "done"),
		} {
			if err := r.persistEvent(ctx, tr, ev); err != nil {
				t.Errorf("persistEvent: %v", err)
				return
			}
		}
	})
	if got := lastTurn(t, base, convID).Content; got != "done" {
		t.Fatalf("committed answer = %q", got)
	}
	closeWriter(t, writer, store)
	if got := strings.Join(store.recorded(), ","); got != "write "+convID {
		t.Fatalf("graph events = %q, want the trace written after the turn", got)
	}
}
