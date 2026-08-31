package mcptools

import (
	"context"
	"fmt"
)

const recallContextHeader = "X-Aura-Active-Sources"

type recallSourceKey struct {
	ConversationID string `json:"conversation_id"`
	TurnID         string `json:"turn_id"`
}

// encodeRecallContextHeader is the bounded wire codec for host-derived active
// source keys. The RED stub keeps the contract compiling while tests establish
// the required canonical and over-cap behavior.
func encodeRecallContextHeader([]recallSourceKey) (string, error) {
	return "", fmt.Errorf("recall context codec not implemented")
}

// recallContextHeaderFunc derives active source keys from the current tool call.
// The RED stub intentionally emits nothing until the codec contract is green.
func recallContextHeaderFunc(context.Context) map[string]string {
	return nil
}
