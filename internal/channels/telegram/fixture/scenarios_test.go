package fixture

import (
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
	tele "gopkg.in/telebot.v4"
)

// streamingMinThreshold mirrors the constant in internal/telegram/streaming.go
// so scenario token counts produce the same flush decisions.
const streamingMinThreshold = 30

// TestCaptureSimpleReply records the common case: a single content token that
// exceeds the flush threshold, no CoT, no tool calls. The placeholder is
// edited in-place rather than a new message being sent.
func TestCaptureSimpleReply(t *testing.T) {
	content := "**bold** " + strings.Repeat("x", streamingMinThreshold)
	tokens := []llm.Token{
		{Content: content},
		{Done: true, Usage: llm.TokenUsage{TotalTokens: 7}},
	}
	placeholder := &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}}

	result := Capture(t, "simple_reply", tokens, placeholder)

	if result.Response.Content != content {
		t.Fatalf("Response.Content = %q, want %q", result.Response.Content, content)
	}
	if !result.Delivered {
		t.Fatal("Delivered = false, want true")
	}
	var edits int
	for _, c := range result.Calls {
		if c.Method == "editMessageText" {
			edits++
		}
	}
	// Expect at least 2 edits: initial placeholder edit + final clean edit.
	if edits < 2 {
		t.Fatalf("editMessageText calls = %d, want >= 2", edits)
	}
}

// TestCaptureWithCoT records a streaming run where reasoning tokens arrive
// before content. The fixture captures both the live 🧠-prefixed edits and
// the final clean-answer edit that drops the CoT header.
func TestCaptureWithCoT(t *testing.T) {
	answer := strings.Repeat("ok ", streamingMinThreshold)
	tokens := []llm.Token{
		{Reasoning: "Considering options carefully so the answer is concise."},
		{Reasoning: " The user wants a short reply."},
		{Content: answer},
		{Done: true, Usage: llm.TokenUsage{TotalTokens: 12, ReasoningTokens: 5}},
	}
	placeholder := &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}}

	result := Capture(t, "with_cot", tokens, placeholder)

	if !result.Delivered {
		t.Fatal("Delivered = false, want true after CoT + content")
	}
	if !strings.Contains(result.Response.Reasoning, "Considering options") {
		t.Fatalf("Reasoning = %q, want CoT text", result.Response.Reasoning)
	}
	var sawCoT bool
	for _, c := range result.Calls {
		if c.Method != "editMessageText" {
			continue
		}
		if body, ok := c.Body["text"].(string); ok && strings.Contains(body, "🧠") {
			sawCoT = true
		}
	}
	if !sawCoT {
		t.Fatal("no editMessageText carried the live CoT prefix (🧠)")
	}
}

// TestCaptureWithToolCallAndEntityTable records a run where the model returns
// a Markdown table (rendered to Telegram entities) and then signals a tool
// call on the Done token. Delivered must be false because tool calls suppress
// the final delivery flag; the intermediate entity-table edits are still
// captured so US-401 can verify the rendering is byte-identical after the port.
func TestCaptureWithToolCallAndEntityTable(t *testing.T) {
	tableContent := "| Name  | Value |\n|-------|-------|\n| Alpha | 1     |\n| Beta  | 2     |\n| Gamma | 3     |"
	tokens := []llm.Token{
		{Content: tableContent},
		{Done: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_files"}}, Usage: llm.TokenUsage{TotalTokens: 20}},
	}
	placeholder := &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}}

	result := Capture(t, "with_tool_call_and_entity_table", tokens, placeholder)

	if result.Delivered {
		t.Fatal("Delivered = true, want false for tool-call response")
	}
	if !result.Response.HasToolCalls || len(result.Response.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %+v, want 1 call", result.Response.ToolCalls)
	}
	// At least one editMessageText must have been made for the table content
	// (the initial placeholder edit) before the tool-call Done token arrived.
	var edits int
	for _, c := range result.Calls {
		if c.Method == "editMessageText" {
			edits++
		}
	}
	if edits < 1 {
		t.Fatalf("editMessageText calls = %d, want >= 1 (table edit before tool call)", edits)
	}
}
