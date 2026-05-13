// Package silentadapter provides the no-user-output OutboundAdapter pair
// for the heartbeat and cron channels. Both run agent turns whose only
// side-effects should be on memory/wiki — never a user notification, never
// a Telegram edit. The adapter logs the final stats so an operator can
// inspect job runtime via structured logs without subscribing to events.
//
// Wave 3.0 Step 3. Per PRD §2.1 + §11.4 DeliveryModeSilent.
package silentadapter

import (
	"context"
	"log/slog"

	"github.com/aura/aura/internal/chathub"
)

// Outbound is the shared silent-mode delivery adapter. Two pre-built
// constructors specialise it for the heartbeat and cron channels — same
// behavior, different channel registration so each can be wired
// independently if a future slice splits their telemetry.
type Outbound struct {
	channel chathub.Channel
	logger  *slog.Logger
}

// Config tunes the Outbound. A nil Logger falls back to slog.Default().
type Config struct {
	Logger *slog.Logger
}

// NewHeartbeat returns an Outbound bound to chathub.ChannelHeartbeat.
func NewHeartbeat(cfg Config) *Outbound {
	return build(chathub.ChannelHeartbeat, cfg)
}

// NewCron returns an Outbound bound to chathub.ChannelCron.
func NewCron(cfg Config) *Outbound {
	return build(chathub.ChannelCron, cfg)
}

func build(ch chathub.Channel, cfg Config) *Outbound {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Outbound{channel: ch, logger: logger.With("channel", string(ch))}
}

// Channel satisfies chathub.OutboundAdapter.
func (o *Outbound) Channel() chathub.Channel { return o.channel }

// Mode is always DeliveryModeSilent.
func (*Outbound) Mode() chathub.DeliveryMode { return chathub.DeliveryModeSilent }

// Deliver consumes events. Most are silent no-ops; EventError is logged at
// Warn (so a failed heartbeat does not vanish into the void) and EventDone
// is logged at Info with the final stats so operators can spot regressions
// in scheduled-job latency / cost.
func (o *Outbound) Deliver(_ context.Context, ev chathub.OutboundEvent) error {
	switch ev.Type {
	case chathub.EventError:
		errText, _ := ev.Payload["error"].(string)
		o.logger.Warn("silent agent run error",
			"run_id", ev.RunID,
			"thread_id", ev.ThreadID,
			"error", errText,
		)
	case chathub.EventDone:
		args := []any{
			"run_id", ev.RunID,
			"thread_id", ev.ThreadID,
		}
		if status, ok := ev.Payload["status"].(string); ok {
			args = append(args, "status", status)
		}
		o.logger.Info("silent agent run complete", args...)
	case chathub.EventUsage:
		args := []any{"run_id", ev.RunID}
		for _, k := range []string{"llm_calls", "tool_calls", "tokens_total", "cost_usd", "terminal_tool"} {
			if v, ok := ev.Payload[k]; ok {
				args = append(args, k, v)
			}
		}
		o.logger.Info("silent agent run usage", args...)
	}
	return nil
}
