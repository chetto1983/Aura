// serve_drain.go is the bounded in-flight turn drain for `aura serve` shutdown
// (audit O-06 / mitigation AP-17). It is split out of serve.go (refactor-on-touch,
// CLAUDE.md ≤600 LOC) and owns ONE small, testable primitive: drainWithGrace.
//
// The problem it solves: on a SIGTERM the daemon's work ctx must NOT be cancelled
// the instant the signal fires, or an in-flight turn is hard-killed mid-stream
// instead of reaching its terminal Event/frame (the channels path derives its
// per-turn ctx from the daemon ctx; the HTTP/SSE path drains via http.Server.
// Shutdown). drainWithGrace runs the teardown that joins in-flight turns under a
// BOUNDED grace window: if the turns finish in time it reports Drained, otherwise
// it reports TimedOut so the caller proceeds to the hard-cancel backstop. It never
// blocks past the grace on an overrunning turn — bounded shutdown, never a wedge.
package main

import (
	"context"
	"log/slog"
	"time"
)

// drainResult reports the outcome of a bounded in-flight drain.
type drainResult int

const (
	// drainResultDrained: in-flight turns finished within the grace window.
	drainResultDrained drainResult = iota
	// drainResultTimedOut: the grace elapsed with turns still running — the caller
	// proceeds to the hard-cancel backstop.
	drainResultTimedOut
)

func (d drainResult) String() string {
	switch d {
	case drainResultDrained:
		return "drained"
	case drainResultTimedOut:
		return "timed-out"
	default:
		return "unknown"
	}
}

// drainWithGrace runs drain (the teardown that joins in-flight turns — e.g. the
// channel StopAll + http.Server.Shutdown) in its own goroutine and waits up to
// grace for it to return. It reports drainResultDrained on a clean join, or
// drainResultTimedOut when the grace elapses first — WITHOUT blocking on the
// still-running drain (the spawned goroutine outlives the timeout; the caller's
// hard-cancel backstop unwedges it). A non-positive grace degrades to a single
// non-blocking poll: it never waits, so a misconfigured knob cannot disable the
// backstop path. The drain func must itself be non-leaking once its ctx is
// cancelled (the goroutine is abandoned, not joined, on timeout).
func drainWithGrace(drain func(), grace time.Duration) drainResult {
	done := make(chan struct{})
	go func() {
		drain()
		close(done)
	}()
	if grace <= 0 {
		select {
		case <-done:
			return drainResultDrained
		default:
			return drainResultTimedOut
		}
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-done:
		return drainResultDrained
	case <-timer.C:
		return drainResultTimedOut
	}
}

// drainShutdown is the teardown body the bounded drain runs (O-06/AP-17): it stops
// accepting NEW work and JOINS the in-flight turns. stopChannelSubsystems stops the
// poller (no new channel turns) and StopAll joins each channel's in-flight turn
// WaitGroup; httpSrv.Shutdown stops the HTTP listener and drains the active SSE
// streams up to aguiShutdownTimeout. workCtx parents the per-turn work and is STILL
// LIVE here (it is not signal-derived), so a turn mid-stream can finalize while this
// joins it. The caller wraps this in drainWithGrace and fires workCancel as the
// backstop only after the grace elapses.
func drainShutdown(workCtx context.Context, env *serveEnv) {
	stopChannelSubsystems(workCtx, env.channels, env.setupSrv)

	// Stop the periodic sidecar sweep (M-06 part 2) under its own bounded join: an
	// in-flight tick runs to completion, a hung walk cannot wedge shutdown. Idempotent
	// when the worker was disabled (a non-positive interval launched no goroutine).
	if env.sweeper != nil {
		env.sweeper.Stop()
	}
	if env.assetProcessingWorker != nil {
		env.assetProcessingWorker.Stop()
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), aguiShutdownTimeout)
	defer cancel()
	if err := env.httpSrv.Shutdown(shutCtx); err != nil {
		slog.Warn("aura serve: agui http server shutdown", "err", err)
	}
}
