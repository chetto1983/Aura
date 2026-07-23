package agui

import (
	"context"
	"iter"
	"log/slog"
	"net/http"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
)

// sseHeartbeatComment is the idle-keepalive frame streamSSE writes on each heartbeat
// tick: a bare SSE comment line (leading ':') terminated by the blank line that closes
// every SSE frame. It carries no event/data fields, so it is protocol-invisible to the
// SDK's frame parser AND to the cockpit's sseAdapter (`line.startsWith(':')` skip,
// web/src/chat/sseAdapter.ts) — its only purpose is to keep bytes flowing on an
// otherwise-quiet connection so an intermediary (proxy/LB/browser) does not time it out.
var sseHeartbeatComment = []byte(":hb\n\n")

// server_sse.go carries the per-connection SSE pump — the streamSSE producer/drain pair, the
// backpressure-aware pumpSend, the lifecycle-frame classifier, and the buffer-cap resolver. It
// is split out of server.go (routing + handlers) so that file stays comfortably under the
// 600-LOC ceiling (refactor-on-touch, CLAUDE.md) when the 37E effort governance lands there.

// streamSSE pumps the translated event stream to the client over SSE through a
// cap-N buffered channel: the producer goroutine ranges the stream (the sole sender,
// drop+WARN on a full buffer so it never blocks the Loop, T-12-09) while the handler
// goroutine drains the channel onto the wire via the SDK writer. On client disconnect
// (ctx.Done) both unwind — the producer stops yielding, the channel closes, the drain
// loop returns (goleak-clean, Pitfall 4). A translated RUN_ERROR is sanitized at the
// translator boundary already; sanitizeErr is the belt-and-suspenders for the pump's
// own error frame.
func (s *Server) streamSSE(ctx context.Context, w http.ResponseWriter, stream iter.Seq2[events.Event, error]) {
	writer := sse.NewSSEWriter()
	out := make(chan events.Event, s.bufferCap())
	go func() {
		defer close(out)
		for ev, err := range stream {
			if err != nil {
				s.pumpSend(ctx, out, events.NewRunErrorEvent(sanitizeErr(err)))
				return
			}
			if !s.pumpSend(ctx, out, ev) {
				return
			}
		}
	}()

	flusher, _ := w.(http.Flusher)

	// heartbeatC stays nil (never fires; select simply never picks a nil case) when the
	// heartbeat is disabled — no ticker is allocated on that hot path (fix-plan 1.3
	// Tier A requirement: allocation-free when disabled).
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
			// Idle-connection defense: a long quiet stretch (model thinking, a slow tool
			// call) with no real frame to write would otherwise let an intermediary kill
			// the connection. This branch and the event branch below are mutually
			// exclusive cases of the SAME select in the drain loop's sole writer goroutine,
			// so a heartbeat write can only ever land BETWEEN two WriteEventWithType calls —
			// never inside one — by construction (never a torn/split SSE frame).
			if _, err := w.Write(sseHeartbeatComment); err != nil {
				return // client gone
			}
			if flusher != nil {
				flusher.Flush()
			}
		case ev, ok := <-out:
			if !ok {
				return
			}
			ev = redactEvent(ev)
			if err := writer.WriteEventWithType(ctx, w, ev, string(ev.Type())); err != nil {
				return // client gone — let the producer drain via ctx (Pitfall 4)
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// pumpOutcome is the result of one pumpDeliver attempt. Only pumpAbort means "stop
// the producer"; the other outcomes all mean "keep going" with different bookkeeping
// at the call site (drop metric + WARN, subscriber removal on gone).
type pumpOutcome int

const (
	pumpDelivered pumpOutcome = iota
	pumpDropped               // non-lifecycle frame refused by a full channel (T-12-09)
	pumpAbort                 // ctx cancelled — the producer must unwind
	pumpGone                  // the subscriber unsubscribed mid-send (RunSession only)
)

// pumpDeliver is the ONE shared drop-policy helper behind both the per-connection SSE
// pump (pumpSend) and the detached-run RunSession fan-out (fix-plan 1.3 Tier B):
// deliver if there is room; a lifecycle frame (per isLifecycleFrame) that cannot fit
// falls back to a blocking send — still abortable on ctx-cancel or the subscriber's
// gone signal — so a terminal frame is never dropped (an AG-UI consumer waits on
// RUN_FINISHED; silently dropping it is a protocol violation, not graceful
// degradation, WR-01); a non-lifecycle delta that cannot fit is refused (pumpDropped)
// so the Loop never stalls on a slow consumer (T-12-09). gone is nil on the pumpSend
// path (a nil channel case never fires — the heartbeatC trick); RunSession passes its
// per-subscriber unsubscribe signal because its producer ctx — unlike the request ctx
// here — does NOT cancel on client disconnect, so without gone an abandoned full
// channel would wedge a blocking lifecycle send until the wallclock cap. Generic over
// the element type so chan events.Event and chan seqEvent share the single policy
// (no second copy of the classification table).
func pumpDeliver[T any](ctx context.Context, gone <-chan struct{}, out chan<- T, item T, lifecycle bool) pumpOutcome {
	if lifecycle {
		select {
		case out <- item:
			return pumpDelivered
		case <-ctx.Done():
			return pumpAbort
		case <-gone:
			return pumpGone
		}
	}
	select {
	case out <- item:
		return pumpDelivered
	case <-ctx.Done():
		return pumpAbort
	case <-gone:
		return pumpGone
	default:
		return pumpDropped
	}
}

// pumpSend delivers one event to the SSE channel via the shared pumpDeliver policy:
// deliver if there is room, abort on ctx-cancel, blocking (abortable) send for a
// lifecycle frame, drop + WARN + metric for a non-lifecycle delta that cannot fit.
// Returns false only on ctx-cancel so the producer unwinds and closes the channel.
func (s *Server) pumpSend(ctx context.Context, out chan events.Event, ev events.Event) bool {
	switch pumpDeliver(ctx, nil, out, ev, isLifecycleFrame(ev.Type())) {
	case pumpAbort:
		return false
	case pumpDropped:
		recordSSEDropped()
		slog.Warn("agui server: SSE client slow, dropping event", "type", ev.Type())
	}
	return true
}

// isLifecycleFrame reports whether an event is a protocol boundary frame that cannot
// be dropped under backpressure. Dropping START/END/RESULT/CUSTOM/SNAPSHOT frames can
// leave delivered deltas without their protocol parent, causing events.ValidateSequence
// to reject the surviving sub-sequence. Shared by the SSE pump and in-process fanout.
func isLifecycleFrame(t events.EventType) bool {
	switch t {
	case events.EventTypeRunStarted,
		events.EventTypeRunFinished,
		events.EventTypeRunError,
		events.EventTypeTextMessageStart,
		events.EventTypeTextMessageEnd,
		events.EventTypeToolCallStart,
		events.EventTypeToolCallEnd,
		events.EventTypeToolCallResult,
		events.EventTypeReasoningStart,
		events.EventTypeReasoningMessageStart,
		events.EventTypeReasoningMessageEnd,
		events.EventTypeReasoningEnd,
		events.EventTypeCustom,
		events.EventTypeStateDelta,
		events.EventTypeStateSnapshot:
		return true
	default:
		return false
	}
}

// bufferCap resolves the per-connection SSE channel cap, falling back to the fanout
// default when the config knob is non-positive.
func (s *Server) bufferCap() int {
	if s.cfg.BufferCap > 0 {
		return s.cfg.BufferCap
	}
	return fanoutBuffer
}

// heartbeatIntervalFromConfig resolves AURA_AGUI_SSE_HEARTBEAT_SEC to the ticker
// Duration streamSSE uses. Unlike bufferCap, a non-positive value means DISABLED, not
// "use a default" — an operator setting 0 wants no heartbeat traffic at all.
func heartbeatIntervalFromConfig(sec int) time.Duration {
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}
