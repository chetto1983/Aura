package assets

import (
	"context"
	"errors"
	"testing"
	"time"
)

type countingScope struct {
	calls   int
	foundAt int // resolve succeeds from this call on (1-based); 0 = never
	err     error
	// stopAt cancels the wait's context on that call, so a polling test counts calls instead of
	// racing a wall-clock deadline: on a stalled CI runner the ticker and the deadline are both
	// ready and select picks either (measured 2026-11-24, CI run 45355583986, 1 call in 20ms).
	stopAt int
	stop   context.CancelFunc
}

func (s *countingScope) ResolveDocumentScope(_ context.Context, _ string, ids []string) ([]string, error) {
	s.calls++
	if s.stopAt > 0 && s.calls >= s.stopAt {
		s.stop()
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.foundAt > 0 && s.calls >= s.foundAt {
		return ids, nil
	}
	return nil, nil
}

func TestWaitDocumentIndexedReturnsWhenTheIndexHoldsTheDocument(t *testing.T) {
	t.Parallel()
	scope := &countingScope{foundAt: 3}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := waitDocumentIndexed(ctx, scope, "id-1", "doc-1", time.Millisecond); err != nil {
		t.Fatalf("wait must succeed once the index answers, got %v", err)
	}
	if scope.calls != 3 {
		t.Fatalf("wait must poll until found, got %d calls", scope.calls)
	}
}

func TestWaitDocumentIndexedHonoursTheCallerDeadline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := waitDocumentIndexed(ctx, &countingScope{}, "id-1", "doc-1", time.Hour)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("an unindexed document must surface the deadline, got %v", err)
	}
}

func TestWaitDocumentIndexedKeepsPollingAnUnindexedDocument(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scope := &countingScope{stopAt: 3, stop: cancel} // never found
	if err := waitDocumentIndexed(ctx, scope, "id-1", "doc-1", time.Millisecond); !errors.Is(err, context.Canceled) {
		t.Fatalf("an unindexed document must be polled until the caller stops, got %v", err)
	}
	if scope.calls < 3 {
		t.Fatalf("wait must keep polling until the caller stops, got %d calls", scope.calls)
	}
}

func TestWaitDocumentIndexedRetriesTransientResolverErrors(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scope := &countingScope{err: errors.New("index hiccup"), stopAt: 3, stop: cancel}
	if err := waitDocumentIndexed(ctx, scope, "id-1", "doc-1", time.Millisecond); !errors.Is(err, context.Canceled) {
		t.Fatalf("resolver errors must be retried until the caller stops, got %v", err)
	}
	if scope.calls < 3 {
		t.Fatalf("a resolver error must not stop the poll, got %d calls", scope.calls)
	}
}

func TestWaitDocumentIndexedFailsClosedOnMissingInputs(t *testing.T) {
	t.Parallel()
	var nilService *Service
	if err := nilService.WaitDocumentIndexed(context.Background(), "id", "doc"); err == nil {
		t.Fatal("nil service must refuse")
	}
	if err := (&Service{}).WaitDocumentIndexed(context.Background(), "id", "doc"); err == nil {
		t.Fatal("a service without an index must refuse rather than pretend")
	}
	if err := waitDocumentIndexed(context.Background(), &countingScope{foundAt: 1}, "", "doc", time.Millisecond); err == nil {
		t.Fatal("empty identity must refuse")
	}
	if err := waitDocumentIndexed(context.Background(), &countingScope{foundAt: 1}, "id", "", time.Millisecond); err == nil {
		t.Fatal("empty document id must refuse")
	}
}
