package webadapter

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aura/aura/internal/chat"
)

// InboundRequest is the raw web payload passed from the API bridge to the
// shared chat Hub. It keeps HTTP/API parsing outside the chat core while still
// exercising the same InboundAdapter path as Telegram.
type InboundRequest struct {
	ID          string
	UserID      string
	ThreadID    string
	Message     string
	Attachments []chat.AttachmentRef
	Mode        chat.DeliveryMode
	CreatedAt   time.Time
}

// Inbound implements chat.InboundAdapter for buffered web chat.
type Inbound struct{}

var _ chat.InboundAdapter = (*Inbound)(nil)

// New returns the web inbound adapter for Hub registration.
func New() *Inbound { return &Inbound{} }

func (*Inbound) Channel() chat.Channel { return chat.ChannelWeb }

func (*Inbound) Normalize(_ context.Context, raw any) (chat.InboundMessage, error) {
	req, ok := normalizeInboundRequest(raw)
	if !ok {
		return chat.InboundMessage{}, fmt.Errorf("webadapter: expected InboundRequest, got %T", raw)
	}
	userID := strings.TrimSpace(req.UserID)
	if userID == "" {
		userID = "anonymous"
	}
	mode := req.Mode
	if mode == "" {
		mode = chat.DeliveryModeDeferred
	}
	createdAt := req.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	return chat.InboundMessage{
		ID:          strings.TrimSpace(req.ID),
		Channel:     chat.ChannelWeb,
		PrincipalID: userID,
		ThreadID:    ThreadID(userID, req.ThreadID),
		Text:        strings.TrimSpace(req.Message),
		Attachments: append([]chat.AttachmentRef(nil), req.Attachments...),
		Mode:        mode,
		CreatedAt:   createdAt,
	}, nil
}

func normalizeInboundRequest(raw any) (InboundRequest, bool) {
	switch v := raw.(type) {
	case InboundRequest:
		return v, true
	case *InboundRequest:
		if v == nil {
			return InboundRequest{}, false
		}
		return *v, true
	default:
		return InboundRequest{}, false
	}
}
