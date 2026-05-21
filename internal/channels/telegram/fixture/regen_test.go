package fixture

import (
	"os"
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
	tele "gopkg.in/telebot.v4"
)

// TestRegenSnapshots is a one-shot helper that rewrites the committed
// snapshots. Gated behind AURA_REGEN_SNAPSHOTS=1 so it never runs in CI
// — otherwise byte-parity would silently rubber-stamp every change.
//
// Run locally after a deliberate change to Outbound's wire format:
//
//	AURA_REGEN_SNAPSHOTS=1 go test -count=1 -run TestRegenSnapshots \
//	  ./internal/channels/telegram/fixture/
//
// Then `git diff testdata/` to verify the drift is intentional.
func TestRegenSnapshots(t *testing.T) {
	if os.Getenv("AURA_REGEN_SNAPSHOTS") != "1" {
		t.Skip("set AURA_REGEN_SNAPSHOTS=1 to rewrite committed snapshots")
	}
	cases := []struct {
		name        string
		tokens      []llm.Token
		placeholder *tele.Message
		opts        []Option
	}{
		{
			name: "simple_reply",
			tokens: []llm.Token{
				{Content: "**bold** " + strings.Repeat("x", streamingMinThreshold)},
				{Done: true, Usage: llm.TokenUsage{TotalTokens: 7}},
			},
			placeholder: &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}},
		},
		{
			name: "with_cot",
			tokens: []llm.Token{
				{Reasoning: "Considering options carefully so the answer is concise."},
				{Reasoning: " The user wants a short reply."},
				{Content: strings.Repeat("ok ", streamingMinThreshold)},
				{Done: true, Usage: llm.TokenUsage{TotalTokens: 12, ReasoningTokens: 5}},
			},
			placeholder: &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}},
		},
		{
			name: "with_tool_call_and_entity_table",
			tokens: []llm.Token{
				{Content: "| Name  | Value |\n|-------|-------|\n| Alpha | 1     |\n| Beta  | 2     |\n| Gamma | 3     |"},
				{Done: true, ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "search_files"}}, Usage: llm.TokenUsage{TotalTokens: 20}},
			},
			placeholder: &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}},
		},
		{
			name: "fallback_entity_edit_to_plain_text",
			tokens: []llm.Token{
				{Content: "**bold** " + strings.Repeat("x", streamingMinThreshold)},
				{Done: true, Usage: llm.TokenUsage{TotalTokens: 7}},
			},
			placeholder: &tele.Message{ID: 1, Chat: &tele.Chat{ID: 123}},
			opts:        []Option{WithEntityEditError(1)},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			Capture(t, tc.name, tc.tokens, tc.placeholder, tc.opts...)
		})
	}
}
