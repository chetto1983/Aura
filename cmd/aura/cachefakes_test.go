package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

func TestRebuildMessagesInvalidToolCallsErrors(t *testing.T) {
	_, err := rebuildMessages([]conversations.AppendTurnParams{{
		Role:      llm.RoleAssistant,
		ToolCalls: json.RawMessage(`not-json`),
	}})
	if err == nil {
		t.Fatal("rebuildMessages with invalid tool_calls: want error, got nil")
	}
	if !strings.Contains(err.Error(), "decode tool_calls") {
		t.Fatalf("error should name tool_calls decode, got %v", err)
	}
}
