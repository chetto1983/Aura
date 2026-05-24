package agent

import (
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
)

// TestFocusTopicExtractionFromLastUserMessage verifies lastUserMessageText finds
// the last user-role message in a mixed sequence and truncates to 500 runes.
func TestFocusTopicExtractionFromLastUserMessage(t *testing.T) {
	messages := []llm.Message{
		{Role: "system", Content: "system overlay"},
		{Role: "user", Content: "user 1"},
		{Role: "assistant", Content: "assistant 1"},
		{Role: "user", Content: "user 2"},
		{Role: "assistant", Content: "assistant 2"},
		{Role: "user", Content: "user 3"},
		{Role: "assistant", Content: "assistant 3"},
		{Role: "user", Content: "user 4"},
		{Role: "assistant", Content: "assistant 4"},
		{Role: "user", Content: "this is the last user message"},
	}

	got := lastUserMessageText(messages)
	if got != "this is the last user message" {
		t.Errorf("lastUserMessageText = %q, want 'this is the last user message'", got)
	}

	longMsg := strings.Repeat("è", 600) // 600 multi-byte runes, truncated to 500.
	messages[len(messages)-1].Content = longMsg
	got = lastUserMessageText(messages)
	if got != string([]rune(longMsg)[:500]) {
		t.Errorf("truncated content mismatch")
	}
	if len([]rune(got)) != 500 {
		t.Errorf("truncated rune count = %d, want 500", len([]rune(got)))
	}

	noUser := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "assistant", Content: "reply"},
	}
	if got := lastUserMessageText(noUser); got != "" {
		t.Errorf("no-user-message result = %q, want empty", got)
	}
}
