// child_staleness.go implements D-03's termination model: a worker is reaped for
// silence, not for age.
//
// PRD Amendment #154 measured the wall-clock cap this file replaces as the WRONG
// instrument: four workers on executable goals finished in 5.15/5.51/7.58/7.80s
// against a 120s cap -- a 23x margin. Across three runs the cap fired exactly ONCE,
// and what it caught was an upstream `context deadline exceeded` stall, not slow
// work, while the worker most worth interrupting (70.31s calling tools that could
// never succeed) sailed under it and returned ok. Both reference implementations
// (LibreChat, hermes) reap on inactivity instead, and Aura already owns the loop
// where that tick belongs: runChild's `for ev, err := range worker.Run(ic)`
// (swarm.go). This file is that tick, extracted to its own concern per this
// package's existing brief.go/report.go/swarm_depth.go split.
//
// Mirrors internal/llm/openai_compat/stream_idle.go's idleWatchdog (B-08) -- the
// same resettable-inactivity-timer shape already proven in this tree for a stalled
// LLM stream, generalized here to a stalled WORKER (any event, not just stream
// bytes): a time.AfterFunc timer, Reset on progress, an atomic "already fired" flag
// so a tripped deadline is never re-armed and Stop is safe after a fire.
package swarm

import (
	"context"
	"sync/atomic"
	"time"
)

// childStaleness is the per-worker inactivity deadline. idle<=0 disables it
// entirely (the shipped AURA_ASKUSER_PAUSE_TTL_SEC <=0-disables precedent):
// Progress and Stop become no-ops and no timer goroutine is ever created.
type childStaleness struct {
	timer   *time.Timer
	idle    time.Duration
	stalled atomic.Bool
}

// newChildStaleness arms a timer that calls cancel once idle elapses with no
// Progress() call. cancel is the worker's OWN context.CancelFunc (runChild derives
// a child context specifically for this), so a reap affects only that one worker --
// never its siblings (D-02's isolation, carried into the new bound).
func newChildStaleness(cancel context.CancelFunc, idle time.Duration) *childStaleness {
	cs := &childStaleness{idle: idle}
	if idle <= 0 {
		return cs
	}
	cs.timer = time.AfterFunc(idle, func() {
		cs.stalled.Store(true)
		cancel()
	})
	return cs
}

// Progress resets the inactivity deadline. runChild calls this on every non-nil
// worker event -- a worker that streams is a worker that is alive, whatever it is
// streaming. A no-op when idle<=0 disabled the timer, or once the deadline has
// already fired (never re-arm a tripped deadline, mirroring idleWatchdog.reset's
// same guard).
//
// RED (TDD): naive stub -- Reset is intentionally NOT called, so
// TestChildStalenessResetKeepsAlive fails for a real reason (repeated progress
// does not keep the worker alive) rather than a build error. GREEN restores the
// real Reset call.
func (cs *childStaleness) Progress() {
	if cs == nil || cs.timer == nil || cs.stalled.Load() {
		return
	}
}

// Stop releases the timer without firing cancel -- the normal-completion path
// (deferred in runChild) so a finished worker leaves no timer/goroutine behind.
// nil-safe; safe to call after a fire (Stop on an already-fired AfterFunc timer is
// a documented no-op, per time.Timer's own contract).
func (cs *childStaleness) Stop() {
	if cs == nil || cs.timer == nil {
		return
	}
	cs.timer.Stop()
}

// Stalled reports whether THIS timer's own silence -- not the caller, not a
// sibling's failure, not an upstream budget trip -- is what cancelled the
// worker's context. runChild uses this (rather than a generic ctx.Err() check) to
// normalize a reap to a uniform {failed, "stalled: ..."} report without
// mislabeling an externally-cancelled worker (parent turn cancelled, budget trip)
// as stalled.
func (cs *childStaleness) Stalled() bool {
	return cs != nil && cs.stalled.Load()
}
