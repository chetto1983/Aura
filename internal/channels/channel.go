package channels

import "context"

// Channel is the narrow lifecycle contract every daemon channel implements
// (Telegram now; WhatsApp/Discord future). It mirrors the codebase's narrow-
// interface idiom (internal/agent/tools.Tool) and the fail-soft daemon-subsystem
// posture of the scheduler + AG-UI server mounted in bootServe.
//
// CRITICAL (research §1): Start takes NO fanout subscriber. The PRD's older
// Start(ctx, sub) sketch is wrong for this codebase — fanout is built PER TURN
// inside the channel (Subscribe-before-Run, one Fanout per runner.Turn), never
// once at channel start. The channel holds the *runner.Runner and builds a fresh
// Fanout inside each turn handler. Keep Start(ctx) clean; the per-turn wiring is
// internal to the channel.
type Channel interface {
	// Name keys AURA_CHANNEL_<upper(Name)>_ENABLED — e.g. "telegram".
	Name() string
	// Start launches the channel (e.g. the polling goroutine) and returns once
	// started, NOT on each turn. A failed Start is aggregated by the Registry,
	// never fatal to the daemon (fail-soft).
	Start(ctx context.Context) error
	// Stop drains the channel gracefully (goleak-clean). Idempotent: a Stop on a
	// never-started channel is a no-op.
	Stop(ctx context.Context) error
	// IsHealthy is the health probe surface (e.g. /setup/status).
	IsHealthy() bool
}
