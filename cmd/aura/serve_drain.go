// serve_drain.go is the bounded in-flight turn drain for `aura serve` shutdown
// (audit O-06 / mitigation AP-17). It is split out of serve.go (refactor-on-touch,
// CLAUDE.md ≤600 LOC) and owns ONE small, testable primitive: drainWithGrace.
//
// The problem it solves: on a SIGTERM the daemon's work ctx must NOT be cancelled
// the instant the signal fires, or an in-flight turn is hard-killed mid-stream
// instead of persisting its records and reaching its terminal Event/frame (the
// channels path derives its per-turn ctx from the daemon ctx; on the HTTP/SSE path
// agui.Server.BeginShutdown, the drain's first step, cancels a web turn, and
// http.Server.Shutdown waits for its stream to end). drainWithGrace runs the
// teardown that joins in-flight turns under a BOUNDED grace window: if the turns
// finish in time it reports Drained, otherwise it reports TimedOut so the caller
// proceeds to the hard-cancel backstop. It never blocks past the grace on an
// overrunning turn — bounded shutdown, never a wedge.
package main

import (
	"context"
	"log/slog"
	"net/http"
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
// accepting NEW work and JOINS the in-flight turns. It first cancels every web turn:
// agui.Server.BeginShutdown ends the observer streams and cancels each request-scoped turn,
// and RunRegistry.Close cancels each detached one (the cockpit's run) and joins the reaper.
// Their streams keep draining to their terminal frames. That comes before the channel join,
// which can run past the grace: a web turn cancelled after it would be cut by the process
// exit with its records unwritten (measured on the lab VM 2026-10-13, when a Telegram turn
// held a detached cockpit turn for 12 s). stopChannelSubsystems then stops the poller (no
// new channel turns) and StopAll joins each channel's in-flight turn WaitGroup. The HTTP
// drain at the end waits up to aguiShutdownTimeout for the web streams, and
// shutdownHTTPServer closes whatever is still open after that. workCtx parents the
// channel turns and is STILL LIVE here (it is not signal-derived), so a turn
// mid-stream can finalize while this joins it. The caller wraps this in
// drainWithGrace and fires workCancel as the backstop only after the grace elapses.
func drainShutdown(workCtx context.Context, env *serveEnv) {
	if env.beginWebShutdown != nil {
		env.beginWebShutdown()
	}
	// Fix-plan 1.3 Tier B (amendment #90 §1.1 bound 3c): detached producers ride
	// context.WithoutCancel, so nothing else in this teardown reaches them; without this
	// walk a detached turn would run to its wallclock cap past process intent. Nil when
	// the flag is off (registry never constructed).
	if env.runRegistry != nil {
		env.runRegistry.Close()
	}
	stopChannelSubsystems(workCtx, env.channels, env.setupSrv)

	// Stop the periodic sidecar sweep (M-06 part 2) under its own bounded join: an
	// in-flight tick runs to completion, a hung walk cannot wedge shutdown. Idempotent
	// when the worker was disabled (a non-positive interval launched no goroutine).
	if env.sweeper != nil {
		env.sweeper.Stop()
	}
	if env.approvalExpirySweeper != nil {
		env.approvalExpirySweeper.Stop()
	}
	if env.messageDraftRecovery != nil {
		env.messageDraftRecovery.Stop()
	}
	if env.steerQueueSweeper != nil {
		env.steerQueueSweeper.Stop()
	}
	if env.assetProcessingWorker != nil {
		env.assetProcessingWorker.Stop()
	}
	if env.delegationWorker != nil {
		env.delegationWorker.Stop()
	}
	// Stop the crash-orphan reconciler under its own bounded join (D-01d): an in-flight
	// reconcile tick runs to completion, a hung anti-join cannot wedge shutdown. Idempotent
	// when the worker was disabled (a nil store / non-positive interval launched no goroutine).
	if env.reconciler != nil {
		env.reconciler.Stop()
	}
	if env.deleteReconciler != nil {
		env.deleteReconciler.Stop()
	}
	// Same bounded join for the first-party MCP grant keeper: an in-flight mint finishes,
	// a hung one cannot wedge shutdown. Nil-safe when the deployment never wired one.
	env.firstPartyGrants.Stop()
	env.hostUpdates.Stop()

	shutdownHTTPServer("agui", env.httpSrv, aguiShutdownTimeout)
}

// shutdownHTTPServer gives srv up to timeout to finish its requests, then closes whatever is
// still open. Every stream ends or has its turn cancelled when Shutdown starts
// (RegisterOnShutdown), so a request still running at the timeout is a defect: it is logged
// as an error, and Close ends it rather than leaving its connection to the process exit.
func shutdownHTTPServer(name string, srv *http.Server, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := srv.Shutdown(ctx)
	if err == nil {
		return
	}
	slog.Error("aura serve: http server did not shut down cleanly, closing it", "server", name, "timeout", timeout, "err", err)
	if err := srv.Close(); err != nil {
		slog.Warn("aura serve: http server close", "server", name, "err", err)
	}
}
