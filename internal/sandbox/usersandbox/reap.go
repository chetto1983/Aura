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
// rest of the idle set. A nil router, or one with no backend, is a safe no-op (0 suspended):
// the composition root deliberately builds backend-less routers when the Docker client cannot be
// composed, and a reaper tick against one of those must not dereference a nil backend below.
//
// A non-positive AURA_SANDBOX_IDLE_TTL_SEC DISABLES the sweep, matching every other `<=0 turns
// it off` knob in the codebase (AURA_MCP_PING_INTERVAL_SEC, AURA_AGUI_SSE_HEARTBEAT_SEC,
// AURA_REASONING_PERSIST_MAX_RUNES). Without this guard a TTL of 0 makes `now.Sub(last) >= 0`
// true for EVERY tracked box, so the knob an operator would reach for to mean "never suspend"
// suspends everything on every tick instead — the single-user appliance case, where the box
// should simply stay warm and there is no co-tenant to reclaim memory from.
func (r *SandboxRouter) SuspendIdle(ctx context.Context, now time.Time) (int, error) {
	if r == nil || r.backend == nil {
		return 0, nil
	}
	ttl := time.Duration(r.cfg.IdleTTLSec) * time.Second
	if ttl <= 0 {
		return 0, nil
	}

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
