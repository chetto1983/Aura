package agui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/encoder"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

// server_run_detach.go is the AURA_AGUI_RUN_DETACH=true tail of POST /agent/run
// (fix-plan 1.3 Tier B, design §1/§2.3): the run detaches from the HTTP fetch onto a
// context.WithoutCancel producer that appends translated, pre-redacted frames into
// the RunSession ring; the HTTP response becomes subscriber #0 through drainSession,
// the resume-capable twin of streamSSE's drain loop. The flag-off path never enters
// this file.

// detachRetryAfterSec is the Retry-After hint on the max-live 503 (§2.4). The
// registry frees a slot only when a whole run reaches terminal, so a sub-second
// retry is never useful; 5s is a polite floor, not a measured SLA.
const detachRetryAfterSec = "5"

// detachedRunContext derives the producer ctx from the fully-decorated request ctx
// (§1.1). Values survive, cancellation does not: the client's disconnect must never
// reach the agent turn (golang-context: WithoutCancel for background work outliving
// requests) — the principal, idempotency operation, reasoning override, and
// thread-lock-held marks are all value-keyed, so they ride. The wallclock timeout is
// belt-and-suspenders over the agent Budget deadline (runner.buildAgent): it only
// exists so a mis-configured budget can never leak a producer goroutine forever.
func detachedRunContext(reqCtx context.Context, maxWallclock time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(reqCtx), maxWallclock)
}

// handleRunDetached is handleRun's detached tail: max-live refusal BEFORE the thread
// lock (§2.4), the same TryLockThread 409 fast-path, ownership of unlock transferred
// into RunRegistry.Start (§1.2 — the producer's terminal cleanup releases it, so the
// lock now spans the run's true duration), SubmitAnswers unchanged, then the
// producer goroutine + subscriber-#0 drain.
func (s *Server) handleRunDetached(w http.ResponseWriter, r *http.Request, ctx context.Context, in types.RunAgentInput, userMsg, modelUserMsg *string) {
	if s.runs.atCapacity() {
		w.Header().Set("Retry-After", detachRetryAfterSec)
		http.Error(w, "too many live runs", http.StatusServiceUnavailable)
		return
	}

	var unlock func()
	if locker, ok := s.run.(threadTryLocker); ok {
		var locked bool
		unlock, locked = locker.TryLockThread(ctx, in.ThreadID)
		if !locked {
			http.Error(w, runner.ErrThreadBusy.Error(), http.StatusConflict)
			return
		}
		// NO defer: on the detached path the lock is released by the session's
		// terminal cleanup (turn end = stream drained = lock free, §1.2). Every
		// pre-Start error return below must release it explicitly.
		ctx = runner.WithThreadLockHeld(ctx)
	}
	release := func() {
		if unlock != nil {
			unlock()
		}
	}

	if len(in.Resume) > 0 {
		answers, err := s.validateResumeEntries(ctx, in.Resume)
		if err != nil {
			release()
			http.Error(w, sanitizeErr(err), resumeErrorStatus(err))
			return
		}
		if _, err := s.run.SubmitAnswers(ctx, answers); err != nil {
			release()
			http.Error(w, sanitizeErr(err), resumeErrorStatus(err))
			return
		}
	}

	runID := "run-" + uuid.NewString()
	dctx, dcancel := detachedRunContext(ctx, s.runs.cfg.maxWallclock)
	sess, err := s.runs.Start(runParams{
		runID:      runID,
		threadID:   in.ThreadID,
		identityID: scopedIdentityID(ctx),
		cancel:     dcancel,
		unlock:     unlock,
	})
	if err != nil {
		// Raced past the atCapacity pre-check (or the registry closed mid-shutdown):
		// same 503 surface, lock released here because Start never took ownership.
		dcancel()
		release()
		w.Header().Set("Retry-After", detachRetryAfterSec)
		http.Error(w, "too many live runs", http.StatusServiceUnavailable)
		return
	}

	// Subscriber #0 registers before the producer starts; fromSeq 0 on a fresh
	// session can never gap, so ok is structural.
	ch, cancelSub, _ := sess.subscribeFrom(0)

	turn := s.run.Turn(dctx, in.ThreadID, modelUserMsg)
	if userMsg != nil && modelUserMsg != nil && *userMsg != *modelUserMsg {
		if split, ok := s.run.(modelUserMessageRunner); ok {
			turn = split.TurnWithModelUserMessage(dctx, in.ThreadID, *userMsg, *modelUserMsg)
		}
	}
	// showReasoning=true is the same cockpit-scoped D-01 override the flag-off tail
	// documents (server_run.go) — the registry is fed ONLY by handleRun.
	go s.runProducer(dctx, dcancel, sess, Translate(in.ThreadID, runID, s.idgen, turn, true))

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	s.drainSession(r.Context(), w, ch, cancelSub)
}

// runProducer drives the translated stream into the session ring. Redaction moves to
// buffer-insert time here (§6.3, amendment #90 point 7): the ring stores
// post-redactEvent frames so the original stream and every resumed stream serve
// byte-identical bytes — a resume can never widen what the live stream showed. A
// source error maps to a sanitized terminal RUN_ERROR (streamSSE's producer parity).
// finish() is the single terminal: it stamps finishedAt, closes every subscriber,
// and fires the cleanup chain (byThread removal + thread unlock).
//
// append's false return (fan aborted on a done ctx) deliberately does NOT stop this
// loop: the ring write precedes the fan, and a CANCELLED run's remaining frames —
// above all the translator's terminal RUN_ERROR — must still reach the ring so a
// resume replays them (§4.4). This cannot wedge: once ctx is done every lifecycle
// send is immediately abortable, so each append is non-blocking, and the stream
// itself ends because the turn ctx (a child of ours) is what unwound the agent.
func (s *Server) runProducer(ctx context.Context, cancel context.CancelFunc, sess *RunSession, stream iter.Seq2[events.Event, error]) {
	defer cancel() // releases the wallclock timer once the turn is done
	defer sess.finish()
	terminal := false
	for ev, err := range stream {
		if err != nil {
			sess.append(ctx, redactEvent(events.NewRunErrorEvent(sanitizeErr(err))))
			return
		}
		if ev == nil {
			continue
		}
		if isTerminalRunEvent(ev) {
			terminal = true
		}
		sess.append(ctx, redactEvent(ev))
	}
	if terminal {
		return
	}
	// A stream that ends with no terminal frame is what CANCELLING produces: the turn ctx
	// unwinds the agent, the iterator simply stops, and nothing announces an ending. Measured
	// twice on a live stack (2026-09-06): after POST /agent/runs/{id}/cancel returned 202 the
	// subscriber saw TOOL_CALL_START, TOOL_CALL_ARGS, heartbeats — and then nothing, forever.
	// The operator pressed stop, the agent DID stop, and the cockpit kept spinning because its
	// state machine ends a run on RUN_FINISHED or RUN_ERROR and neither ever arrived.
	//
	// So the producer states the ending itself. RUN_ERROR rather than RUN_FINISHED: the turn
	// did not complete, and a client that renders a cancelled run as finished would claim the
	// work was done.
	reason := "run cancelled"
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		reason = sanitizeErr(err)
	}
	// WithoutCancel is the other half of the fix, and without it the first half does nothing.
	// append writes the ring and THEN fans out to live subscribers, and the fan aborts on a done
	// ctx — which this ctx, by construction, always is here. The frame would reach a later
	// resume and never the subscriber actually waiting for it, so the cockpit would still hang
	// on the one path that matters. The announcement of an ending must outlive the cancellation
	// that caused it.
	sess.append(context.WithoutCancel(ctx), redactEvent(events.NewRunErrorEvent(reason)))
}

// isTerminalRunEvent reports whether ev is the frame a client ends a run on. Both types count:
// a run that errored is as finished, to a subscriber, as one that succeeded.
func isTerminalRunEvent(ev events.Event) bool {
	switch ev.Type() {
	case events.EventTypeRunFinished, events.EventTypeRunError:
		return true
	default:
		return false
	}
}

// drainSession is the resume-capable twin of streamSSE's drain loop: same
// single-writer invariant (the heartbeat and the frame write are mutually-exclusive
// select arms in the sole writer goroutine — a heartbeat can only land BETWEEN
// frames, never inside one), same Tier A idle heartbeat, but frames come from a
// RunSession subscription and carry the run-scoped `id: <seq>` line (amendment #90
// point 2). Heartbeat comments carry NO id — they must not advance the client's
// Last-Event-ID. ctx is the VIEWER's lifetime (the request ctx): its cancel detaches
// only this subscriber, never the run. Route-agnostic by design (§8): the resume
// handler reuses it verbatim, and streamPost can adopt it later.
func (s *Server) drainSession(ctx context.Context, w http.ResponseWriter, ch <-chan seqEvent, cancelSub func()) {
	defer cancelSub()
	enc := encoder.NewEventEncoder()
	flusher, _ := w.(http.Flusher)

	var heartbeatC <-chan time.Time
	if s.heartbeatInterval > 0 {
		ticker := time.NewTicker(s.heartbeatInterval)
		defer ticker.Stop()
		heartbeatC = ticker.C
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeatC:
			if _, err := w.Write(sseHeartbeatComment); err != nil {
				return // viewer gone
			}
			if flusher != nil {
				flusher.Flush()
			}
		case sev, ok := <-ch:
			if !ok {
				return // terminal — tail fully replayed, close cleanly (§3.3)
			}
			data, err := enc.EncodeEvent(ctx, sev.Ev, "application/json")
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Warn("agui run detach: encode frame", "err", err, "type", sev.Ev.Type())
				continue
			}
			if err := writeSeqFrame(w, sev.Seq, string(sev.Ev.Type()), data); err != nil {
				return // viewer gone — the producer keeps running detached
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// writeSeqFrame writes one SSE frame as `event:` + `id: <seq>` + `data:` + blank
// line. The frame is hand-assembled rather than delegated to the SDK's SSEWriter
// DELIBERATELY: sse.createSSEFrame emits its OWN `id: <TYPE>_<timestampMs>` line for
// every event carrying a Timestamp — and every constructor-built translated event
// does (events.NewBaseEvent stamps one) — so a prepended seq line would be
// overridden by the SDK's id (the LAST id field in a block wins per the SSE spec),
// breaking the Last-Event-ID contract. The data payload uses the SDK's own
// EventEncoder + newline escaping, so the JSON bytes match the SDK writer's exactly;
// only the id VALUE differs, which is precisely what amendment #90 point 2 ratifies.
func writeSeqFrame(w io.Writer, seq int64, eventType string, data []byte) error {
	escaped := strings.ReplaceAll(string(data), "\n", "\\n")
	escaped = strings.ReplaceAll(escaped, "\r", "\\r")
	_, err := fmt.Fprintf(w, "event: %s\nid: %d\ndata: %s\n\n", eventType, seq, escaped)
	return err
}
