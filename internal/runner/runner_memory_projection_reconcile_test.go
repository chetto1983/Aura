package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

type projectionIdentityRoster struct {
	mu     sync.Mutex
	ids    []string
	err    error
	calls  int
	called chan struct{}
}

func (r *projectionIdentityRoster) IdentityIDs(context.Context) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.called != nil && r.calls == 1 {
		close(r.called)
	}
	return append([]string(nil), r.ids...), r.err
}

func projectionSinkTurn(sink *reconciliationProjectionSink) (arcadedb.ConversationTurnProjection, bool) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, turn := range sink.turns {
		return turn, true
	}
	return arcadedb.ConversationTurnProjection{}, false
}

func waitForProjection(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("projection did not converge before timeout")
}

func crashRecoveryHarness(t *testing.T) (*DeleteReconciler, *ConversationProjector, *postCommitProjectionSource, *reconciliationProjectionSink, context.Context) {
	t.Helper()
	ctx := identityctx.WithIdentityID(context.Background(), "identity-a")
	source := &postCommitProjectionSource{}
	store := &postCommitConversationStore{fakeConvStore: newFakeConvStore(), source: source}
	if err := store.AppendTurn(ctx, conversations.AppendTurnParams{
		ConversationID: "conversation-1", Role: llm.RoleUser, Content: "committed before crash",
	}); err != nil {
		t.Fatalf("seed committed source turn: %v", err)
	}
	sink := newReconciliationProjectionSink()
	projector := NewConversationProjector(source, sink, 1)
	t.Cleanup(func() { _ = projector.Close(context.Background()) })
	reconciler := NewDeleteReconciler(&Runner{Conv: store}, time.Hour)
	reconciler.SetConversationProjection(projector, &projectionIdentityRoster{ids: []string{"identity-a"}})
	return reconciler, projector, source, sink, ctx
}

func TestConversationProjectionCrashRecovery(t *testing.T) {
	reconciler, _, source, sink, _ := crashRecoveryHarness(t)
	for attempt := range 2 {
		if err := reconciler.ReconcileConversationProjection(context.Background()); err != nil {
			t.Fatalf("reconcile attempt %d: %v", attempt+1, err)
		}
	}
	if _, ok := projectionSinkTurn(sink); !ok {
		t.Fatal("restart reconciliation left the committed source turn missing")
	}
	sink.mu.Lock()
	projected := len(sink.turns)
	sink.mu.Unlock()
	if projected != 1 {
		t.Fatalf("repeated replay produced %d derived turns, want 1", projected)
	}
	source.mu.Lock()
	limits := append([]int(nil), source.limits...)
	source.mu.Unlock()
	for _, limit := range limits {
		if limit != 1 {
			t.Fatalf("source page limit = %d, want configured bound 1", limit)
		}
	}
}

func TestConversationProjectionBootReconcile(t *testing.T) {
	_, projector, source, sink, _ := crashRecoveryHarness(t)
	roster := &projectionIdentityRoster{ids: []string{"identity-a"}}
	reconciler := NewDeleteReconciler(&Runner{Conv: &postCommitConversationStore{
		fakeConvStore: newFakeConvStore(), source: source,
	}}, time.Hour)
	reconciler.SetConversationProjection(projector, roster)
	reconciler.Start(context.Background())
	t.Cleanup(reconciler.Stop)
	waitForProjection(t, time.Second, func() bool {
		_, ok := projectionSinkTurn(sink)
		return ok
	})
	roster.mu.Lock()
	calls := roster.calls
	roster.mu.Unlock()
	if calls != 1 {
		t.Fatalf("boot reconciliation roster calls = %d, want 1", calls)
	}
}

func TestConversationProjectionPeriodicReconcile(t *testing.T) {
	ctx := identityctx.WithIdentityID(context.Background(), "identity-a")
	source := &postCommitProjectionSource{}
	store := &postCommitConversationStore{fakeConvStore: newFakeConvStore(), source: source}
	sink := newReconciliationProjectionSink()
	projector := NewConversationProjector(source, sink, 1)
	t.Cleanup(func() { _ = projector.Close(context.Background()) })
	roster := &projectionIdentityRoster{ids: []string{"identity-a"}, called: make(chan struct{})}
	reconciler := NewDeleteReconciler(&Runner{Conv: store}, 10*time.Millisecond)
	reconciler.SetConversationProjection(projector, roster)
	reconciler.Start(context.Background())
	select {
	case <-roster.called:
	case <-time.After(time.Second):
		t.Fatal("boot reconciliation never ran")
	}
	if err := store.AppendTurn(ctx, conversations.AppendTurnParams{
		ConversationID: "conversation-1", Role: llm.RoleUser, Content: "periodic original",
	}); err != nil {
		t.Fatalf("append periodic source turn: %v", err)
	}
	waitForProjection(t, time.Second, func() bool {
		turn, ok := projectionSinkTurn(sink)
		return ok && turn.Content == "periodic original"
	})
	source.replace("periodic edited")
	waitForProjection(t, time.Second, func() bool {
		turn, ok := projectionSinkTurn(sink)
		return ok && turn.Content == "periodic edited"
	})
	source.clear()
	waitForProjection(t, time.Second, func() bool {
		_, ok := projectionSinkTurn(sink)
		return !ok
	})
	started := time.Now()
	reconciler.Stop()
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("reconciler shutdown took %s", elapsed)
	}

	failingSource := &postCommitProjectionSource{err: errors.New("postgres paging unavailable")}
	failingProjector := NewConversationProjector(failingSource, newReconciliationProjectionSink(), 1)
	t.Cleanup(func() { _ = failingProjector.Close(context.Background()) })
	failing := NewDeleteReconciler(&Runner{Conv: store}, time.Hour)
	failing.SetConversationProjection(failingProjector, &projectionIdentityRoster{ids: []string{"identity-a"}})
	if err := failing.ReconcileConversationProjection(context.Background()); err == nil {
		t.Fatal("authoritative paging failure was not observable")
	}
}
