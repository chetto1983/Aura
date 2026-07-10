package agui

import (
	"context"
	"iter"
	"log/slog"
	"net/http"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
)

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
	for {
		select {
		case <-ctx.Done():
			return
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

// pumpSend delivers one event to the SSE channel without ever blocking the producer
// indefinitely: deliver if there is room, abort on ctx-cancel. A run-lifecycle frame
// (RUN_STARTED/RUN_FINISHED/RUN_ERROR) that cannot fit falls back to a blocking send
// (still abortable on ctx-cancel) so the terminal frame is never dropped — an AG-UI
// consumer waits on RUN_FINISHED, and silently dropping it is a protocol violation, not
// graceful degradation (WR-01). A non-lifecycle delta that cannot fit is DROPPED with a
// WARN (T-12-09: the Loop must never stall on a slow client). Returns false only on
// ctx-cancel so the producer unwinds and closes the channel.
func (s *Server) pumpSend(ctx context.Context, out chan events.Event, ev events.Event) bool {
	if isLifecycleFrame(ev.Type()) {
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	default:
		recordSSEDropped()
		slog.Warn("agui server: SSE client slow, dropping event", "type", ev.Type())
		return true
	}
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
