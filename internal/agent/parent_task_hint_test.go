package agent

import (
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestParentTaskHintFromMessagesLatestUserCapped(t *testing.T) {
	long := strings.Repeat("x", parentTaskHintMaxChars+20)
	got := parentTaskHintFromMessages([]llm.Message{
		{Role: "user", Content: "older question"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "  " + long + "  "},
	})
	if got != strings.Repeat("x", parentTaskHintMaxChars) {
		t.Fatalf("hint len/content = %d/%q", len(got), got)
	}
}

func TestParentTaskHintFromMessagesEmptyWhenNoUser(t *testing.T) {
	got := parentTaskHintFromMessages([]llm.Message{{Role: "assistant", Content: "hello"}})
	if got != "" {
		t.Fatalf("hint = %q, want empty", got)
	}
}
