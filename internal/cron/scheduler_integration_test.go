//go:build db_integration

// Integration tests for the tick loop (D-02/D-04): due-task selection, the
// max-concurrent skip+reschedule when a task is already in-flight, and graceful
// Start(ctx) shutdown. Run via:
//
//	go test -tags db_integration -race -run TestScheduler ./internal/cron -count=1
package cron

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recordingDispatcher counts dispatches and records which tasks ran, so a tick's
// due-selection is observable without the real 10-05 handlers.
type recordingDispatcher struct {
	mu    sync.Mutex
	count atomic.Int32
	ran   map[string]int
	hold  time.Duration // optional artificial work so concurrency is observable
}

func newRecordingDispatcher(hold time.Duration) *recordingDispatcher {
	return &recordingDispatcher{ran: map[string]int{}, hold: hold}
}

func (d *recordingDispatcher) Dispatch(ctx context.Context, task Task, c *Claim) error {
	d.count.Add(1)
	d.mu.Lock()
	d.ran[task.ID]++
	d.mu.Unlock()
	if d.hold > 0 {
		select {
		case <-time.After(d.hold):
		case <-ctx.Done():
		}
	}
	return nil
}

func TestSchedulerTickDispatchesDueTask(t *testing.T) {
	now := time.Now().UTC()
	pool := migratedPool(t)
	store := New(pool)
	disp := newRecordingDispatcher(0)
	s := NewScheduler(pool, store, SchedulerConfig{
		Now:           func() time.Time { return now },
		MaxConcurrent: 2,
		TickInterval:  time.Second,
		Dispatch:      disp,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	task := seedDueTask(t, s, now)

	if err := s.tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if disp.count.Load() != 1 {
		t.Fatalf("dispatch count = %d, want 1 (the single due task)", disp.count.Load())
	}
	disp.mu.Lock()
	ranTask := disp.ran[task.ID]
	disp.mu.Unlock()
	if ranTask != 1 {
		t.Errorf("due task dispatched %d times, want 1", ranTask)
	}
}

func TestSchedulerTickSkipsInFlightAndReschedules(t *testing.T) {
	now := time.Now().UTC()
	pool := migratedPool(t)
	store := New(pool)
	s := NewScheduler(pool, store, SchedulerConfig{
		Now:           func() time.Time { return now },
		MaxConcurrent: 2,
		TickInterval:  time.Second,
		Dispatch:      newRecordingDispatcher(0),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	task := seedDueTask(t, s, now)

	// Simulate the task already in-flight on another worker: hold its advisory lock.
	held, err := s.claim(ctx, task)
	if err != nil {
		t.Fatalf("pre-hold claim: %v", err)
	}
	defer held.release(ctx)

	// The tick's claim loses the lock → D-04 skip + reschedule (next_run_at advanced
	// off the now-due value). No double-execution (SC#1).
	if err := s.tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	reloaded, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if !reloaded.NextRunAt.After(now) {
		t.Errorf("skipped task next_run_at = %s, want rescheduled past now %s (D-04)", reloaded.NextRunAt, now)
	}
}

func TestSchedulerStartGracefulShutdown(t *testing.T) {
	now := time.Now().UTC()
	pool := migratedPool(t)
	store := New(pool)
	s := NewScheduler(pool, store, SchedulerConfig{
		Now:           func() time.Time { return now },
		MaxConcurrent: 2,
		TickInterval:  50 * time.Millisecond,
		Dispatch:      newRecordingDispatcher(0),
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()

	time.Sleep(150 * time.Millisecond) // let a couple ticks run
	cancel()                           // graceful shutdown

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start returned %v, want nil on graceful ctx-cancel", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return on ctx cancel (leaked tick loop)")
	}
}

func TestSchedulerTickBoundedByMaxConcurrent(t *testing.T) {
	now := time.Now().UTC()
	pool := migratedPool(t)
	store := New(pool)
	disp := newRecordingDispatcher(150 * time.Millisecond) // hold so overlap is real
	const cap = 2
	s := NewScheduler(pool, store, SchedulerConfig{
		Now:           func() time.Time { return now },
		MaxConcurrent: cap,
		TickInterval:  time.Second,
		Dispatch:      disp,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Seed more due tasks than the cap; DueTasks(LIMIT=cap) bounds the batch so the
	// tick never holds more than `cap` conns at once (Pitfall 2).
	for i := 0; i < cap+2; i++ {
		seedDueTask(t, s, now)
	}
	if err := s.tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	// One tick claims at most `cap` tasks (LIMIT). The rest stay due for the next tick.
	if got := disp.count.Load(); got > int32(cap) {
		t.Errorf("dispatched %d in one tick, want <= cap %d (max-concurrent bound)", got, cap)
	}
}
