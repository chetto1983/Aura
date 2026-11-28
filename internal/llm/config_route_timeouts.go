package llm

import "time"

// The call and stream bounds when neither the file nor the env sets them. OpenRouter gets room
// for one provider fallback: measured on the lab VM 2026-11-28, it sent no byte for 78 s while a
// provider timed out (504), and its fallback answered 5 s later; a 60 s watchdog closed the
// stream before that answer, and the retry went back to the slow provider.
const (
	defaultTotalTimeoutSec                = 120
	defaultStreamIdleTimeoutSec           = 60
	openRouterDefaultTotalTimeoutSec      = 180
	openRouterDefaultStreamIdleTimeoutSec = 90
)

// CallTimeout bounds one model call, stream included: TotalTimeoutSec when set, otherwise the
// route's default, resolved now so a hot route switch takes the new route's bound.
func (c Config) CallTimeout() time.Duration {
	if c.TotalTimeoutSec > 0 {
		return time.Duration(c.TotalTimeoutSec) * time.Second
	}
	if c.isOpenRouterRoute() {
		return openRouterDefaultTotalTimeoutSec * time.Second
	}
	return defaultTotalTimeoutSec * time.Second
}

// StreamIdleTimeout is the longest gap with no byte on an open stream before it is abandoned as
// stalled (B-08): StreamIdleTimeoutSec when configured (0 disables the watchdog), otherwise the
// route's default.
func (c Config) StreamIdleTimeout() time.Duration {
	if c.StreamIdleTimeoutConfigured || c.StreamIdleTimeoutSec > 0 {
		return time.Duration(c.StreamIdleTimeoutSec) * time.Second
	}
	if c.isOpenRouterRoute() {
		return openRouterDefaultStreamIdleTimeoutSec * time.Second
	}
	return defaultStreamIdleTimeoutSec * time.Second
}

func (c Config) isOpenRouterRoute() bool {
	return ReasoningTarget(c.Provider, c.BaseURL) == ReasoningTargetOpenRouter
}
