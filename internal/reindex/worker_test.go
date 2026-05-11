package reindex

import (
	"context"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// fakeReindexer implements search.WikiPageReindexer without importing internal/search
// to keep the test self-contained (it satisfies the interface structurally).
type fakeReindexer struct {
	calls      atomic.Int64
	blockUntil chan struct{} // when non-nil, ReindexWikiPage blocks on this until ctx.Done() or chan close
	returnErr  error
}

func (f *fakeReindexer) ReindexWikiPage(ctx context.Context, slug string) error {
	f.calls.Add(1)
	if f.blockUntil != nil {
		select {
		case <-f.blockUntil:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.returnErr
}

func TestSubmitter_InterfaceSatisfied(t *testing.T) {
	var _ Submitter = (*Worker)(nil) // compile-time check
}

func TestWorker_DropNewest(t *testing.T) {
	r := &fakeReindexer{blockUntil: make(chan struct{})} // block forever to fill queue
	defer close(r.blockUntil)
	w := NewWorker(r, Config{QueueSize: 2})
	// First submit starts processing inside drain (blocks). Next two fill the buffer.
	if !w.Submit(Job{Slug: "a"}) {
		t.Fatal("submit a should succeed")
	}
	if !w.Submit(Job{Slug: "b"}) {
		t.Fatal("submit b should succeed")
	}
	if !w.Submit(Job{Slug: "c"}) {
		t.Fatal("submit c should succeed")
	}
	// Buffer now full. Next submit should drop and increment counter.
	if w.Submit(Job{Slug: "d"}) {
		t.Fatal("submit d should drop")
	}
	if got := w.Health().Dropped; got < 1 {
		t.Fatalf("Dropped=%d, want >=1", got)
	}
	w.Stop()
}

func TestWorker_DropAfterStop(t *testing.T) {
	r := &fakeReindexer{}
	w := NewWorker(r, DefaultConfig())
	w.Stop()
	if w.Submit(Job{Slug: "x"}) {
		t.Fatal("submit after stop should drop")
	}
	if got := w.Health().DroppedAfterStop; got < 1 {
		t.Fatalf("DroppedAfterStop=%d, want >=1", got)
	}
}

func TestWorker_StopCancelsInflight(t *testing.T) {
	r := &fakeReindexer{blockUntil: make(chan struct{})}
	defer close(r.blockUntil)
	w := NewWorker(r, DefaultConfig())
	w.Submit(Job{Slug: "a"})
	// Give drain a moment to pick up the job.
	time.Sleep(10 * time.Millisecond)
	start := time.Now()
	w.Stop()
	elapsed := time.Since(start)
	if elapsed > 200*time.Millisecond {
		t.Fatalf("Stop took %v, want < 200ms (must cancel in-flight ctx, not block on blockUntil)", elapsed)
	}
}

func TestWorker_NoGoroutineLeak(t *testing.T) {
	base := runtime.NumGoroutine()
	for i := 0; i < 100; i++ {
		r := &fakeReindexer{}
		w := NewWorker(r, DefaultConfig())
		w.Submit(Job{Slug: "x"})
		w.Stop()
	}
	// Give scheduler a moment.
	time.Sleep(50 * time.Millisecond)
	delta := runtime.NumGoroutine() - base
	if delta > 5 { // small allowance for go test internals
		t.Fatalf("goroutine leak: delta=%d after 100 cycles", delta)
	}
}

func TestWorker_ErrorPropagatedToHealth(t *testing.T) {
	r := &fakeReindexer{returnErr: errors.New("boom")}
	w := NewWorker(r, DefaultConfig())
	w.Submit(Job{Slug: "x"})
	// Give drain time to process.
	time.Sleep(50 * time.Millisecond)
	if got := w.Health().LastError; got == "" {
		t.Fatalf("LastError = empty, want non-empty after failed reindex")
	}
	w.Stop()
}
