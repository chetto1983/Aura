package agui

import (
	"context"
	"iter"
	"log/slog"
	"sync/atomic"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/chetto1983/aura/internal/reasoningtrace"
)

// fanoutBuffer is the per-subscriber channel capacity. A slow subscriber that fills
// its buffer has subsequent events DROPPED (with a WARN) rather than back-pressuring
// the producer — the agent Loop must never stall on a slow consumer (RESEARCH Pattern
// 4). The AURA_AGUI_* env-tunable cap is exposed via config in a later plan; this is
// the named default, mirroring openai_compat's chunkBuffer convention.
const fanoutBuffer = 64

// Fanout distributes a single translated AG-UI event stream to N concurrent
// in-process subscribers. It wraps the Translate output (iter.Seq2[events.Event,
// error]) and, for each event, sends to every subscriber via a cap-64 buffered,
// drop-on-full, never-block pump (the openai_compat Stream analog plus a default-drop
// arm). The single producer goroutine started by Run is the SOLE sender on every
// subscriber channel and closes them all on exit (ctx-cancel OR source end), so the
// stream is goleak-clean (Pitfall 4).
//
// Fanout is NOT on the HTTP path (RESEARCH diagram line 134): server.go does not route
// through it. It is the in-process seam the Phase-13 Telegram channel consumes.
//
// Errors from the wrapped source are already mapped to a RUN_ERROR event by Translate
// (the translated stream is pure events.Event), so Fanout never inspects the error
// slot — it just forwards whatever Translate yields.
type Fanout struct {
	source  iter.Seq2[events.Event, error]
	subs    []chan events.Event
	started atomic.Bool
}

// NewFanout wraps a translated event stream. Subscribe before Run to register a
// consumer; Run then drives the source once and fans out to all registered subscribers.
func NewFanout(source iter.Seq2[events.Event, error]) *Fanout {
	return &Fanout{source: source}
}

// Subscribe registers a new consumer and returns its receive-only channel. The channel
// is cap-64 buffered and is closed by the producer goroutine when the source ends or
// ctx is cancelled. Subscribe MUST be called before Run: a subscriber registered after
// the producer has snapshotted f.subs is never fed (and racing the snapshot is a data
// race), so Subscribe-after-Run panics loudly rather than failing silently (WR-06).
func (f *Fanout) Subscribe() <-chan events.Event {
	if f.started.Load() {
		panic("agui: Fanout.Subscribe called after Run — subscribe all consumers before starting the producer")
	}
	ch := make(chan events.Event, fanoutBuffer)
	f.subs = append(f.subs, ch)
	return ch
}

// Run starts the single producer goroutine that ranges the wrapped source once and
// fans each event to every subscriber. It returns immediately; the goroutine exits on
// ctx-cancel or source end, closing every subscriber channel on the way out (sole
// sender closes — golang-concurrency principle #4). Run is non-blocking and must be
// called AT MOST ONCE: a second Run would launch a second producer that double-closes
// the subscriber channels (panic: close of closed channel). The started guard makes the
// misuse loud at the call site instead (WR-06).
func (f *Fanout) Run(ctx context.Context) {
	if !f.started.CompareAndSwap(false, true) {
		panic("agui: Fanout.Run called twice — a Fanout drives its source exactly once")
	}
	subs := f.subs
	go func() {
		defer closeAll(subs)
		for ev := range f.source {
			if ctx.Err() != nil {
				return
			}
			reasoningtrace.Record("agui_fanout_source_event", map[string]any{
				"event_type":        string(ev.Type()),
				"subscribers_count": len(subs),
				"event_json":        eventJSONString(ev),
			})
			for i, sub := range subs {
				if !send(ctx, sub, ev, i) {
					return // ctx cancelled — stop the whole producer
				}
			}
		}
	}()
}

// send performs the fan-out send: deliver if the subscriber has room, abort the producer
// on ctx-cancel. A run-lifecycle frame (RUN_STARTED/RUN_FINISHED/RUN_ERROR) that cannot
// fit falls back to a blocking send (still abortable on ctx-cancel) so the terminal frame
// is never dropped — a consumer waiting on RUN_FINISHED would otherwise hang (WR-01). A
// non-lifecycle delta that cannot fit is DROPPED (the subscriber is slow) with a WARN, so
// the agent Loop never stalls on a slow consumer. Returns false only on ctx-cancel so the
// producer unwinds and closes channels.
func send(ctx context.Context, sub chan events.Event, ev events.Event, subscriberIndex int) bool {
	if isLifecycleFrame(ev.Type()) {
		select {
		case sub <- ev:
			reasoningtrace.Record("agui_fanout_delivered", map[string]any{
				"subscriber_index": subscriberIndex,
				"event_type":       string(ev.Type()),
			})
			return true
		case <-ctx.Done():
			return false
		}
	}
	select {
	case sub <- ev:
		reasoningtrace.Record("agui_fanout_delivered", map[string]any{
			"subscriber_index": subscriberIndex,
			"event_type":       string(ev.Type()),
		})
		return true
	case <-ctx.Done():
		return false
	default:
		reasoningtrace.Record("agui_fanout_dropped", map[string]any{
			"subscriber_index": subscriberIndex,
			"event_type":       string(ev.Type()),
			"event_json":       eventJSONString(ev),
		})
		slog.Warn("agui fanout: subscriber slow, dropping event", "type", ev.Type())
		return true
	}
}

func eventJSONString(ev events.Event) string {
	if ev == nil {
		return ""
	}
	b, err := ev.ToJSON()
	if err != nil {
		return err.Error()
	}
	return string(b)
}

// closeAll closes every subscriber channel. The producer goroutine is the sole sender
// and the sole caller (via defer), so it owns the close (a receiver-side close would
// panic on a late send).
func closeAll(subs []chan events.Event) {
	for _, sub := range subs {
		close(sub)
	}
}
