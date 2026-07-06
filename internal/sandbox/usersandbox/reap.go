// reap.go provides SuspendIdle — the live handlers.SandboxReaper implementation (the 37-03
// consumer-declared seam): the idle-suspend sweep the scheduler's KindSandboxReap task drives
// (D-08). It reads the mutex-guarded lastUsed map the router bumps on each Route, selects every
// box idle past cfg.IdleTTLSec, Suspends it, and clears its tracking entry so a later Route
// transparently auto-resumes it (the resume is INLINE in Route — never scheduled). It adds NO
// goroutine or ticker: the sweep runs on the scheduler tick, so goleak stays green
// (T-37-05-LEAK).

package usersandbox

import (
	"context"
	"fmt"
	"time"
)

// SuspendIdle suspends every tracked box whose idle age (now - lastUsed) has reached the
// configured idle TTL and returns the count suspended, satisfying handlers.SandboxReaper. It
// snapshots the idle set UNDER the router mutex, then Suspends OUTSIDE the lock (a moby stop is
// slow and must never block a concurrent Route). A suspended box's tracking entry is cleared so
// the next Route re-resolves and transparently auto-resumes it (D-08), and it is not re-swept
// while stopped. A Suspend error is terminal for this sweep — the count so far is returned for
// the handler's summary; the dispatcher records the error and the next tick re-evaluates the
// rest of the idle set. A nil router is a safe no-op (0 suspended).
func (r *SandboxRouter) SuspendIdle(ctx context.Context, now time.Time) (int, error) {
	if r == nil {
		return 0, nil
	}
	ttl := time.Duration(r.cfg.IdleTTLSec) * time.Second

	type idleBox struct {
		id string
		h  BoxHandle
	}
	r.mu.Lock()
	var idle []idleBox
	for id, last := range r.lastUsed {
		if now.Sub(last) >= ttl {
			idle = append(idle, idleBox{id: id, h: r.handles[id]})
		}
	}
	r.mu.Unlock()

	suspended := 0
	for _, box := range idle {
		if err := r.backend.Suspend(ctx, box.h); err != nil {
			return suspended, fmt.Errorf("usersandbox: suspend idle box %q: %w", box.id, err)
		}
		r.mu.Lock()
		delete(r.lastUsed, box.id)
		delete(r.handles, box.id)
		r.mu.Unlock()
		suspended++
	}
	return suspended, nil
}
