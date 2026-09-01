package runner

import (
	"context"
	"sync"
	"testing"
	"time"
)

type recordingReasoningRetention struct {
	mu       sync.Mutex
	calls    []reasoningRetentionCall
	started  chan struct{}
	released chan struct{}
}

type reasoningRetentionCall struct {
	identityID string
	now        time.Time
	limit      int
}

func (s *recordingReasoningRetention) DeleteExpiredReasoning(
	ctx context.Context,
	identityID string,
	now time.Time,
	limit int,
) (int, error) {
	s.mu.Lock()
	s.calls = append(s.calls, reasoningRetentionCall{identityID: identityID, now: now, limit: limit})
	s.mu.Unlock()
	if s.started != nil {
		select {
		case <-s.started:
		default:
			close(s.started)
		}
	}
	if s.released != nil {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-s.released:
		}
	}
	return 1, nil
}

func (s *recordingReasoningRetention) snapshot() []reasoningRetentionCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]reasoningRetentionCall(nil), s.calls...)
}

func TestReasoningRetentionWorker(t *testing.T) {
	store := &recordingReasoningRetention{}
	roster := &projectionIdentityRoster{ids: []string{"identity-b", "identity-a", "identity-a"}}
	reconciler := NewDeleteReconciler(nil, time.Hour)
	reconciler.SetReasoningRetention(store, roster, 7)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if err := reconciler.ReconcileReasoningRetention(context.Background(), now); err != nil {
		t.Fatalf("ReconcileReasoningRetention: %v", err)
	}
	calls := store.snapshot()
	if len(calls) != 2 || calls[0].identityID != "identity-a" || calls[1].identityID != "identity-b" {
		t.Fatalf("retention calls = %+v, want sorted unique identities", calls)
	}
	for _, call := range calls {
		if call.limit != 7 || !call.now.Equal(now) {
			t.Fatalf("retention call = %+v, want fixed now and batch 7", call)
		}
	}
}

func TestReasoningRetentionClose(t *testing.T) {
	store := &recordingReasoningRetention{started: make(chan struct{}), released: make(chan struct{})}
	reconciler := NewDeleteReconciler(nil, time.Millisecond)
	reconciler.SetReasoningRetention(store, &projectionIdentityRoster{ids: []string{"identity-a"}}, 1)
	reconciler.Start(context.Background())
	select {
	case <-store.started:
	case <-time.After(time.Second):
		t.Fatal("reasoning retention worker did not start")
	}
	done := make(chan struct{})
	go func() {
		reconciler.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		close(store.released)
		t.Fatal("reasoning retention close did not join the worker")
	}
}
