package openai_compat

import (
	"context"
	"net/http"
	"time"

	"github.com/openai/openai-go/v3/option"
)

// StreamIdleContext shares the byte-level watchdog with other SDK stream adapters.
func StreamIdleContext(ctx context.Context, window time.Duration) (context.Context, func() bool, func()) {
	streamCtx, cancel := context.WithCancel(ctx)
	control := &idleRequestControl{window: window, cancel: cancel}
	streamCtx = context.WithValue(streamCtx, idleRequestContextKey{}, control)
	return streamCtx, control.firedIdle, func() {
		control.stop()
		cancel()
	}
}

// StreamIdleMiddleware applies the shared inactivity watchdog to SDK response reads.
func StreamIdleMiddleware(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	return idleResponseMiddleware(req, next)
}
