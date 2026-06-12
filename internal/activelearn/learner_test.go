package activelearn

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeOracle is a label-agnostic test double: it records the texts it was asked
// to label+save, optionally fails the first failUntil calls (transient-recovery),
// and counts how many times LabelAndSave fired. A nil-vec or empty result is the
// consumer's concern — here saved==ok models "labeled and persisted".
type fakeOracle struct {
	mu        sync.Mutex
	saved     []string
	calls     atomic.Int64
	failUntil int64 // return saved=false for the first failUntil calls
	block     chan struct{}
}

func (o *fakeOracle) LabelAndSave(_ context.Context, text string, _ []float64) bool {
	n := o.calls.Add(1)
	if o.block != nil {
		<-o.block
	}
	if n <= o.failUntil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.saved = append(o.saved, text)
	return true
}

func (o *fakeOracle) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.saved)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}

// TestNew_NilWhenNoOracle: a Learner with no Oracle is nil, and a nil Learner's
// methods are safe no-ops (the consumer simply observes nothing).
func TestNew_NilWhenNoOracle(t *testing.T) {
	if New(Config{}) != nil {
		t.Error("New without an oracle should be nil")
	}
	var l *Learner
	l.Observe("x", nil, 0) // nil-receiver no-op
	l.Close()              // nil-receiver no-op
}

// TestLearner_LabelsSavesAndRefreshesUncertain: an uncertain (below-floor) turn
// is labeled+saved by the oracle off the hot path, and Refresh fires after a save.
func TestLearner_LabelsSavesAndRefreshesUncertain(t *testing.T) {
	oracle := &fakeOracle{}
	var refreshed atomic.Int64
	l := New(Config{Oracle: oracle, Refresh: func() { refreshed.Add(1) }, MarginFloor: 0.05})
	defer l.Close()

	l.Observe("qual e la capitale dell'Italia?", []float64{0.1, 0.2}, 0.01) // uncertain
	waitFor(t, func() bool { return oracle.count() == 1 })
	if got := oracle.saved[0]; got != "qual e la capitale dell'Italia?" {
		t.Errorf("saved %q", got)
	}
	waitFor(t, func() bool { return refreshed.Load() == 1 })
}

// TestLearner_MarginGatedAndDedup: a confident (above-floor) turn is dropped
// before the queue, and the SAME uncertain text observed twice is labeled once.
func TestLearner_MarginGatedAndDedup(t *testing.T) {
	oracle := &fakeOracle{}
	l := New(Config{Oracle: oracle, MarginFloor: 0.05})
	defer l.Close()

	l.Observe("confident", []float64{1, 0}, 0.20) // above floor → never enqueued
	l.Observe("dup", []float64{0, 1}, 0.01)
	l.Observe("dup", []float64{0, 1}, 0.02) // same text → deduped
	waitFor(t, func() bool { return oracle.count() == 1 })
	time.Sleep(50 * time.Millisecond) // allow any erroneous extra to land
	if oracle.count() != 1 {
		t.Fatalf("want 1 save (confident skipped, dup deduped), got %d", oracle.count())
	}
	if oracle.calls.Load() != 1 {
		t.Errorf("oracle called %d times, want 1 (confident never reaches the worker)", oracle.calls.Load())
	}
}

// TestLearner_TransientFailureRetries: when LabelAndSave reports not-saved the
// content hash is removed from the seen-set, so a later observation of the same
// text retries and eventually commits (the seen.Delete-on-failure path).
func TestLearner_TransientFailureRetries(t *testing.T) {
	oracle := &fakeOracle{failUntil: 1} // first label+save fails, then recovers
	l := New(Config{Oracle: oracle, MarginFloor: 0.05})
	defer l.Close()

	l.Observe("retry me", []float64{0, 1}, 0.01)
	waitFor(t, func() bool { return oracle.calls.Load() == 1 })
	waitFor(t, func() bool { return oracle.count() == 0 }) // first attempt did not save
	// The seen-set entry was deleted on failure → a second observation retries.
	waitFor(t, func() bool {
		l.Observe("retry me", []float64{0, 1}, 0.01)
		return oracle.count() == 1
	})
}

// TestLearner_DropOnFullNeverBlocks: with a queue of 1 and the worker parked
// inside the oracle, a flood of distinct uncertain observations must NOT block
// the caller — surplus observations are dropped, not enqueued, so the hot path is
// never delayed (T-08.2-11 denial-of-service guard).
func TestLearner_DropOnFullNeverBlocks(t *testing.T) {
	block := make(chan struct{})
	oracle := &fakeOracle{block: block}
	l := New(Config{Oracle: oracle, MarginFloor: 0.05, Queue: 1})
	defer func() {
		close(block) // release the parked worker so Close (wg.Wait) returns
		l.Close()
	}()

	done := make(chan struct{})
	go func() {
		// Flood far beyond queue capacity. Each Observe must return immediately;
		// if Observe blocked on a full channel this goroutine would never finish.
		for i := 0; i < 1000; i++ {
			l.Observe("flood-"+string(rune('a'+i%26))+string(rune('a'+i/26)), []float64{0, 1}, 0.01)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Observe blocked under a queue-full flood — drop-on-full regression")
	}
}

// TestLearner_CloseIsIdempotent: Close cancels + waits once and is safe to call
// repeatedly (goleak in TestMain proves the worker is actually reaped).
func TestLearner_CloseIsIdempotent(t *testing.T) {
	l := New(Config{Oracle: &fakeOracle{}})
	l.Close()
	l.Close() // second Close is a no-op (sync.Once), must not panic or hang
}
