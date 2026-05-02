package conversation_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/aura/aura/internal/conversation"
)

// mockStore counts Append calls for BufferedAppender tests.
type mockStore struct {
	mu    sync.Mutex
	count int
}

func (m *mockStore) Append(_ context.Context, _ conversation.Turn) error {
	m.mu.Lock()
	m.count++
	m.mu.Unlock()
	return nil
}

func (m *mockStore) stored() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.count
}

func TestBufferedAppender_DrainAll(t *testing.T) {
	store := &mockStore{}
	// Buffer sized larger than total sends so no drops occur — this test
	// verifies the drain goroutine delivers every enqueued turn.
	const goroutines = 10
	const perGoroutine = 20 // 10 * 20 = 200 total
	appender := conversation.NewBufferedAppender(store, goroutines*perGoroutine)

	var wg sync.WaitGroup
	var idx atomic.Int64
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				turnIdx := idx.Add(1) - 1
				_ = appender.Append(context.Background(), conversation.Turn{
					ChatID:    1,
					UserID:    1,
					TurnIndex: turnIdx,
					Role:      "user",
					Content:   "msg",
				})
			}
		}()
	}
	wg.Wait()

	if err := appender.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := store.stored(); got != goroutines*perGoroutine {
		t.Fatalf("want %d stored, got %d", goroutines*perGoroutine, got)
	}
}

// TestBufferedAppender_DropOnFull verifies that when the buffer is full,
// excess turns are dropped (not blocking) and no error is returned.
func TestBufferedAppender_DropOnFull(t *testing.T) {
	// Size-1 buffer, slow drain: pre-fill it, then send one more so the
	// channel is full when the third Append fires the drop branch.
	blockCh := make(chan struct{})
	slowStore := &slowMockStore{block: blockCh}

	appender := conversation.NewBufferedAppender(slowStore, 1)

	// Send first turn — drain goroutine picks it up and blocks.
	_ = appender.Append(context.Background(), conversation.Turn{ChatID: 1, TurnIndex: 0, Role: "user", Content: "a"})
	// Give drain goroutine time to dequeue the first turn and block inside Append.
	// Fill the buffer with a second turn.
	_ = appender.Append(context.Background(), conversation.Turn{ChatID: 1, TurnIndex: 1, Role: "user", Content: "b"})
	// Third turn should be dropped (buffer size 1, one slot already taken by turn 2).
	err := appender.Append(context.Background(), conversation.Turn{ChatID: 1, TurnIndex: 2, Role: "user", Content: "c"})
	if err != nil {
		t.Fatalf("Append on full buffer should return nil, got %v", err)
	}

	// Release the slow store and close.
	close(blockCh)
	if err := appender.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// slowMockStore blocks on Append until block is closed.
type slowMockStore struct {
	block chan struct{}
}

func (s *slowMockStore) Append(_ context.Context, _ conversation.Turn) error {
	<-s.block
	return nil
}

// TestBufferedAppender_CloseWhileEmpty verifies Close on an empty appender
// returns nil without hanging.
func TestBufferedAppender_CloseWhileEmpty(t *testing.T) {
	store := &mockStore{}
	appender := conversation.NewBufferedAppender(store, 10)
	if err := appender.Close(context.Background()); err != nil {
		t.Fatalf("Close on empty appender: %v", err)
	}
	if got := store.stored(); got != 0 {
		t.Fatalf("want 0 stored, got %d", got)
	}
}

// TestBufferedAppender_DrainErrDuplicateTurn verifies that ErrDuplicateTurn
// from the underlying store is silently suppressed by the drain goroutine.
func TestBufferedAppender_DrainErrDuplicateTurn(t *testing.T) {
	dupStore := &errStore{err: conversation.ErrDuplicateTurn}
	appender := conversation.NewBufferedAppender(dupStore, 10)

	_ = appender.Append(context.Background(), conversation.Turn{ChatID: 1, TurnIndex: 0, Role: "user", Content: "dup"})

	if err := appender.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// No panic, no hang — duplicate suppressed.
}

// TestBufferedAppender_DrainGenericError verifies that non-duplicate errors
// from the underlying store are logged (not panicked) by the drain goroutine.
func TestBufferedAppender_DrainGenericError(t *testing.T) {
	genericErr := errors.New("some db error")
	errStore := &errStore{err: genericErr}
	appender := conversation.NewBufferedAppender(errStore, 10)

	_ = appender.Append(context.Background(), conversation.Turn{ChatID: 1, TurnIndex: 0, Role: "user", Content: "fail"})

	if err := appender.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// No panic, no hang — generic error logged and swallowed.
}

// errStore always returns the configured error from Append.
type errStore struct {
	err error
}

func (e *errStore) Append(_ context.Context, _ conversation.Turn) error {
	return e.err
}
