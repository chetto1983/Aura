package sandbox

import (
	"context"
	"time"
)

// reapLoop is the idle-TTL eviction goroutine (D-03). It is bound to the ctx
// passed to NewSessionManager; Close cancels it and the deferred done-close lets
// Close return (goleak-clean). The ticker period is virtual under testing/synctest
// so the TTL test is deterministic.
func (m *SessionManager) reapLoop(ctx context.Context, every time.Duration) {
	defer close(m.done)
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.reapOnce(ctx)
		}
	}
}

// reapOnce sweeps the session map: any session idle longer than ttl is stopped +
// removed (docker), MarkTerminated (registry), deleted from the map, and the live
// count decremented. The lastUsed read is lock-free (atomic); the eviction holds
// capMu only to mutate count + the map deletion is store-level atomic.
func (m *SessionManager) reapOnce(ctx context.Context) {
	cutoff := m.now().Add(-m.ttl).UnixNano()
	var expired []*session
	m.sessions.Range(func(_, v any) bool {
		s := v.(*session)
		if s.lastUsed.Load() < cutoff {
			expired = append(expired, s)
		}
		return true
	})
	for _, s := range expired {
		m.evict(ctx, s)
	}
}

// evict tears down one idle session. It acquires the per-session lock so it never
// races an in-flight exec; a session locked by an active Acquire is simply skipped
// this sweep (TryLock) and revisited next tick once Released.
func (m *SessionManager) evict(ctx context.Context, s *session) {
	if !s.mu.TryLock() {
		return // in use — revisit next sweep
	}
	defer s.mu.Unlock()

	// Re-check under the lock: a late Acquire may have refreshed lastUsed between
	// the Range snapshot and here.
	if s.lastUsed.Load() >= m.now().Add(-m.ttl).UnixNano() {
		return
	}
	_ = m.docker.stop(ctx, s.containerID)
	_ = m.docker.remove(ctx, s.containerID)
	_ = m.store.MarkTerminated(ctx, s.id)

	m.capMu.Lock()
	if _, ok := m.sessions.LoadAndDelete(s.convID); ok {
		m.count--
	}
	m.capMu.Unlock()
}
