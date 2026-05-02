package conversation_test

import (
	"context"
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
