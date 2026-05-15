package cronadapter

import (
	"context"

	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/cron"
)

// NewHubDispatcher returns a cron.Dispatcher that routes KindAgentJob tasks
// through the Hub (which calls InboundAdapter.Normalize then the AgentLoop)
// and delegates all other task kinds to fallback.
func NewHubDispatcher(hub *chat.Hub, fallback cron.Dispatcher) cron.Dispatcher {
	d := &hubDispatcher{hub: hub, fallback: fallback}
	return d.dispatch
}

type hubDispatcher struct {
	hub      *chat.Hub
	fallback cron.Dispatcher
}

func (d *hubDispatcher) dispatch(ctx context.Context, task *cron.Task) error {
	if task.Kind == cron.KindAgentJob {
		_, err := d.hub.Receive(ctx, chat.ChannelCron, task)
		return err
	}
	return d.fallback(ctx, task)
}
