package openai_compat

import (
	"context"
	"io"
	"sync/atomic"
	"time"
)

// idleWatchdog aborts a stalled stream (B-08). It arms a timer for the idle window;
// each reset (called when bytes arrive) restarts it. If the window elapses with no
// reset, the watchdog records that it fired and cancels the request ctx, unblocking
// the body Read so the parse goroutine returns. The caller checks firedIdle() to emit
// the retryable ErrStreamIdleTimeout instead of the raw context.Canceled.
//
// reset is driven by an idleResettingReader on EVERY Read returning bytes — including
// SSE keep-alive comments (`: OPENROUTER PROCESSING`) — so a long reasoning phase that
// streams pings does not trip it; only a genuinely dead connection does.
type idleWatchdog struct {
	timer  *time.Timer
	window time.Duration
	fired  atomic.Bool
}

// startIdleWatchdog arms a watchdog that calls cancel once the idle window elapses
// without a reset.
func startIdleWatchdog(window time.Duration, cancel context.CancelFunc) *idleWatchdog {
	w := &idleWatchdog{window: window}
	w.timer = time.AfterFunc(window, func() {
		w.fired.Store(true)
		cancel()
	})
	return w
}

// reset restarts the idle window. After the watchdog has fired it is a no-op (the
// cancel already happened — never re-arm a tripped watchdog). nil-safe.
func (w *idleWatchdog) reset() {
	if w == nil || w.fired.Load() {
		return
	}
	w.timer.Reset(w.window)
}

// stop halts the watchdog on stream teardown. nil-safe; safe after a fire.
func (w *idleWatchdog) stop() {
	if w != nil {
		w.timer.Stop()
	}
}

// firedIdle reports whether the watchdog tripped (the stream stalled). nil-safe.
func (w *idleWatchdog) firedIdle() bool {
	return w != nil && w.fired.Load()
}

// idleResettingReader resets w on each Read that returns bytes, so the watchdog
// measures the gap between successive arrivals from the wire (data OR keep-alive),
// not the gap between parsed/emitted chunks.
type idleResettingReader struct {
	r io.Reader
	w *idleWatchdog
}

func (rd *idleResettingReader) Read(p []byte) (int, error) {
	n, err := rd.r.Read(p)
	if n > 0 {
		rd.w.reset()
	}
	return n, err
}
