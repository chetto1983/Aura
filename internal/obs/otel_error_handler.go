package obs

import (
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
)

// defaultOTelErrorWindow is the rate-limit window for the global OTel error
// handler: a persistently-failing exporter (e.g. a collector that went down
// mid-run) emits an error per export attempt, so we log at most once per window
// and fold the rest into a suppressed count surfaced on the next emission.
const defaultOTelErrorWindow = time.Minute

// rateLimitedOTelHandler is the process-global otel.ErrorHandler. The OTel SDK
// reports export failures, batch-queue-full drops, and malformed-span errors
// through this single handler; a no-op (the prior behavior) silently swallowed
// every one, so a real collector's trace loss was invisible (O-03). This logs
// each error via slog but rate-limits to one emission per window so a broken
// exporter cannot flood the logs — the next emission carries the count of errors
// suppressed since the last one. The clock is injectable for deterministic tests.
type rateLimitedOTelHandler struct {
	logger *slog.Logger
	window time.Duration
	now    func() time.Time

	mu         sync.Mutex
	lastLog    time.Time
	suppressed int
	started    bool
}

// newRateLimitedOTelHandler builds the handler. A nil logger falls back to
// slog.Default() (resolved at log time, so it tracks SetDefault); a nil clock
// defaults to time.Now; a non-positive window defaults to defaultOTelErrorWindow.
func newRateLimitedOTelHandler(logger *slog.Logger, window time.Duration, now func() time.Time) *rateLimitedOTelHandler {
	if window <= 0 {
		window = defaultOTelErrorWindow
	}
	if now == nil {
		now = time.Now
	}
	return &rateLimitedOTelHandler{logger: logger, window: window, now: now}
}

// Handle implements otel.ErrorHandler. The first error in a window logs
// immediately; subsequent errors within the window increment a suppressed
// counter without logging; the first error after the window elapses logs and
// reports how many were suppressed in the interim.
func (h *rateLimitedOTelHandler) Handle(err error) {
	if err == nil {
		return
	}

	h.mu.Lock()
	now := h.now()
	if h.started && now.Sub(h.lastLog) < h.window {
		h.suppressed++
		h.mu.Unlock()
		return
	}
	suppressed := h.suppressed
	h.suppressed = 0
	h.lastLog = now
	h.started = true
	h.mu.Unlock()

	logger := h.logger
	if logger == nil {
		logger = slog.Default()
	}
	attrs := []any{"error", err.Error()}
	if suppressed > 0 {
		attrs = append(attrs, "suppressed", suppressed)
	}
	logger.Warn("otel error", attrs...)
}

// installOTelErrorHandler wires the rate-limited handler as the process-global
// OTel error handler. Called from Init's shared boot path so it covers every
// exporter mode (otlp/stdout/none), not just otlp.
func installOTelErrorHandler(logger *slog.Logger) {
	otel.SetErrorHandler(newRateLimitedOTelHandler(logger, defaultOTelErrorWindow, nil))
}
