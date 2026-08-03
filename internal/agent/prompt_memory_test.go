package agent

import (
	"strings"
	"testing"
)

func TestSystemPromptUsesAutomaticMemoryBeforeToolDiscovery(t *testing.T) {
	for _, want := range []string{
		"Aura supplies a bounded current index",
		"When that block answers the current request, answer directly from it",
		"do not call tool_search or open deep memory recall",
		"use deep memory recall before answering or asking",
	} {
		if !strings.Contains(SystemPrompt, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
	for _, stale := range []string{
		"nothing pins it into your context automatically",
		"Recall is pull-on-demand",
	} {
		if strings.Contains(SystemPrompt, stale) {
			t.Errorf("system prompt retains superseded memory rule %q", stale)
		}
	}
}
