package prompt

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestApplyAdaptiveReasoningLeavesConfiguredMaxTokens proves the router passes the
// operator's configured max_tokens through UNCHANGED end-to-end: adaptive reasoning sets
// only the reasoning effort, so a tight cfg.MaxTokens survives on the wire at every tier
// — it is never raised to a per-tier default nor lowered to a per-tier ceiling (the
// 2026-06-14 contract; the old ceiling truncated tool-call arguments).
func TestApplyAdaptiveReasoningLeavesConfiguredMaxTokens(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	reg := testRegistry()
	cfg := testConfig()
	cfg.AdaptiveReasoning = true
	cfg.MaxTokens = 900

	hist := []llm.Message{
		{Role: llm.RoleSystem, Content: "system prefix"},
		{Role: llm.RoleUser, Content: "che tempo fa domani a Caraglio?"},
	}
	for _, tier := range []ReasoningTier{ReasoningTierNone, ReasoningTierLow, ReasoningTierHigh} {
		req := b.BuildWithReasoningTier(hist, reg, "openrouter", cfg, Budget{}, tier)
		if req.MaxTokens != 900 {
			t.Fatalf("tier %q with a 900 cap: MaxTokens = %d, want the configured 900 unchanged", tier, req.MaxTokens)
		}
	}
}

// TestLastGenuineUserContentNoGenuineTurn closes the empty-return arm: a history with no
// real user message (only synthetic nudges, or assistant/system roles) yields "" so the
// router does not classify a synthetic hint as the user's request.
func TestLastGenuineUserContentNoGenuineTurn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		hist []llm.Message
	}{
		{"empty history", nil},
		{
			name: "only synthetic user nudges",
			hist: []llm.Message{
				{Role: llm.RoleSystem, Content: "system prefix"},
				{Role: llm.RoleUser, Content: "<budget>used=1 remaining=2</budget>"},
				{Role: llm.RoleUser, Content: "<workspace>D:/ws/run-1</workspace>"},
				{Role: llm.RoleUser, Content: "Completion check FAILED: missing deliverable"},
			},
		},
		{
			name: "no user role at all",
			hist: []llm.Message{
				{Role: llm.RoleSystem, Content: "system prefix"},
				{Role: llm.RoleAssistant, Content: "hi there"},
			},
		},
		{
			name: "blank user content skipped",
			hist: []llm.Message{
				{Role: llm.RoleUser, Content: "   "},
				{Role: llm.RoleUser, Content: ""},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := LastGenuineUserContent(tc.hist); got != "" {
				t.Fatalf("LastGenuineUserContent = %q, want empty", got)
			}
		})
	}
}

// TestLastGenuineUserContentReturnsNewestReal complements the skip cases: among a mix of
// real and synthetic turns it returns the NEWEST genuine user message, not the first.
func TestLastGenuineUserContentReturnsNewestReal(t *testing.T) {
	t.Parallel()
	hist := []llm.Message{
		{Role: llm.RoleSystem, Content: "system prefix"},
		{Role: llm.RoleUser, Content: "prima domanda"},
		{Role: llm.RoleAssistant, Content: "risposta"},
		{Role: llm.RoleUser, Content: "seconda domanda vera"},
		{Role: llm.RoleUser, Content: "<today>2026-06-13</today>"},
	}
	if got := LastGenuineUserContent(hist); got != "seconda domanda vera" {
		t.Fatalf("LastGenuineUserContent = %q, want newest genuine turn", got)
	}
}
