package mcptools

import (
	"context"

	"github.com/chetto1983/aura/internal/activesources"
	"github.com/chetto1983/aura/internal/agent/tools"
)

// recallContextHeaderFunc derives active source keys from the current tool call.
// Both values are host-owned: sessionID is the active conversation and requestID
// is the current turn/run identifier. A partial context emits nothing.
func recallContextHeaderFunc(ctx context.Context) map[string]string {
	conversationID := tools.SessionIDFromContext(ctx)
	turnID := tools.RequestIDFromContext(ctx)
	if conversationID == "" || turnID == "" {
		return nil
	}
	encoded, err := activesources.Encode([]activesources.Source{{
		ConversationID: conversationID,
		TurnID:         turnID,
	}})
	if err != nil {
		return nil
	}
	return map[string]string{activesources.Header: encoded}
}
