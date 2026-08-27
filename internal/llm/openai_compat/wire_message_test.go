package openai_compat

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// A content-less TOOL message (a tool that returned an empty result) must serialize an
// explicit "content" field. llm.Message.Content is omitempty, so before the wireMessage
// projection the tool message went out WITHOUT content and strict llama.cpp rejected the
// whole request with 400 "All non-assistant messages must contain 'content'" (OpenRouter
// tolerated the omission, hiding the bug). Regression for the live Telegram outage.
func TestToSDKMessagesGuaranteesContentForNonAssistant(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "ciao"},
		{Role: llm.RoleTool, ToolCallID: "call_1", Content: ""},                                // empty tool result — the trigger
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "call_1", Type: "function"}}}, // assistant w/ tool_calls, empty content
	}
	wire, err := toSDKMessages(msgs, nil)
	if err != nil {
		t.Fatalf("toSDKMessages: %v", err)
	}
	if len(wire) != 4 {
		t.Fatalf("len(wire)=%d, want 4", len(wire))
	}

	must := func(idx int, wantContent bool, label string) string {
		b, err := json.Marshal(wire[idx])
		if err != nil {
			t.Fatalf("marshal %s: %v", label, err)
		}
		has := strings.Contains(string(b), `"content"`)
		if has != wantContent {
			t.Fatalf("%s: content-present=%v, want %v — %s", label, has, wantContent, b)
		}
		return string(b)
	}

	// Every non-assistant message carries a content field, even the empty tool result.
	must(0, true, "system")
	must(1, true, "user")
	toolJSON := must(2, true, "empty tool")
	if !strings.Contains(toolJSON, `"content":""`) {
		t.Fatalf("empty tool result must serialize content as \"\", got %s", toolJSON)
	}
	// The assistant-with-tool-calls message keeps OMITTING empty content (spec-optional).
	must(3, false, "assistant with tool_calls")
}

// A non-empty assistant message still serializes its content.
func TestToSDKMessagesKeepsAssistantContent(t *testing.T) {
	wire, err := toSDKMessages([]llm.Message{{Role: llm.RoleAssistant, Content: "hi there"}}, nil)
	if err != nil {
		t.Fatalf("toSDKMessages: %v", err)
	}
	b, _ := json.Marshal(wire[0])
	if !strings.Contains(string(b), `"content":"hi there"`) {
		t.Fatalf("assistant content must survive, got %s", b)
	}
}
