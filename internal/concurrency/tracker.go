package concurrency

import (
	"context"
	"sync"
	"time"
)

// InactivityTracker tracks the last activity time of users and evicts those
// who have been inactive longer than the configured threshold.
//
// CONC-03 requirement: Uses map[string]time.Time guarded by sync.RWMutex only.
// The concurrent map iteration type is not used anywhere in this file.
type InactivityTracker struct {
	mu           sync.RWMutex
	lastActivity map[string]time.Time
	threshold    time.Duration
	sweepInterval time.Duration
	onEvict      func(userID string)
	ctx          context.Context
	cancel       context.CancelFunc
}

// newInactivityTracker creates a new InactivityTracker. It does not start the
// background sweeper goroutine -- call Start() separately.
// Package-internal: only UserGate.New creates trackers per D-11.
func newInactivityTracker(threshold, sweepInterval time.Duration, onEvict func(userID string)) *InactivityTracker {
	ctx, cancel := context.WithCancel(context.Background())
	return &InactivityTracker{
		lastActivity:  make(map[string]time.Time),
		threshold:     threshold,
		sweepInterval: sweepInterval,
		onEvict:       onEvict,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start launches the background sweeper goroutine. It returns immediately.
// The sweeper ticks every sweepInterval and evicts users inactive longer
// than threshold. Stops when Stop() is called.
func (t *InactivityTracker) Start() {
	go func() {
		ticker := time.NewTicker(t.sweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-t.ctx.Done():
				return
			case <-ticker.C:
				t.sweep()
			}
		}
	}()
}

// Touch sets the last activity time for userID to now.
// Called by runActor after each inbox entry is processed per D-09.
func (t *InactivityTracker) Touch(userID string) {
	t.mu.Lock()
	t.lastActivity[userID] = time.Now()
	t.mu.Unlock()
}

// Remove deletes userID from the tracking map.
// Idempotent: safe to call if userID is not tracked.
// Called by UserGate.Evict after cancelling the actor context.
func (t *InactivityTracker) Remove(userID string) {
	t.mu.Lock()
	delete(t.lastActivity, userID)
	t.mu.Unlock()
}

// sweep checks all tracked users and evicts those whose last activity exceeds
// the threshold. It collects stale users under read lock, then calls onEvict
// outside the lock (avoids deadlock with Evict which needs write lock on gate.mu).
func (t *InactivityTracker) sweep() {
	now := time.Now()

	// Collect stale users under read lock.
	t.mu.RLock()
	var stale []string
	for userID, last := range t.lastActivity {
		if now.Sub(last) > t.threshold {
			stale = append(stale, userID)
		}
	}
	t.mu.RUnlock()

	// Evict stale users outside the lock.
	for _, userID := range stale {
		t.onEvict(userID) // calls UserGate.Evict, which removes actor + calls Remove
	}
}

// Stop cancels the tracker context, causing the Start goroutine to exit cleanly.
func (t *InactivityTracker) Stop() {
	t.cancel()
}
