package cronadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/cron"
)

// InboundAdapter normalizes a *cron.Task into a chat.InboundMessage for
// the ChannelCron pipeline. Registered with the cron Hub at boot.
type InboundAdapter struct{}

var _ chat.InboundAdapter = InboundAdapter{}

// New returns an InboundAdapter ready for Hub registration.
func New() *InboundAdapter { return &InboundAdapter{} }

// Channel satisfies chat.InboundAdapter.
func (InboundAdapter) Channel() chat.Channel { return chat.ChannelCron }

// Normalize converts a *cron.Task into an InboundMessage.
func (InboundAdapter) Normalize(_ context.Context, raw any) (chat.InboundMessage, error) {
	task, ok := raw.(*cron.Task)
	if !ok {
		return chat.InboundMessage{}, fmt.Errorf("cronadapter: expected *cron.Task, got %T", raw)
	}
	principalID := task.RecipientID
	if principalID == "" {
		principalID = "cron"
	}
	return chat.InboundMessage{
		Channel:     chat.ChannelCron,
		PrincipalID: principalID,
		ThreadID:    "cron:" + task.Name,
		Text:        task.Payload,
		Mode:        chat.DeliveryModeSilent,
		CreatedAt:   time.Now().UTC(),
		ChannelData: map[string]any{
			"task_id": task.ID,
			"kind":    string(task.Kind),
		},
	}, nil
}
