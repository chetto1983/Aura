package prompt

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestMaxTokensCappingBranches drives the cappedTokens/configuredOrDefault arms
// that the existing policy tests only graze. The tier->maxTokens mapping is the
// load-bearing knob that keeps a None turn from burning a 4096-token ceiling and
// keeps a High turn within the operator's configured cap, so each branch is
// behavior-bearing on the wire request.
func TestMaxTokensCappingBranches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		tier          ReasoningTier
		configuredMax int
		want          int
	}{
		// configuredMax == 0 => the tier target is used verbatim (the per-tier
		// default), and High falls back to the hard 4096 default.
		{"none default target when no cap", ReasoningTierNone, 0, noReasoningMaxTokens},
		{"low default target when no cap", ReasoningTierLow, 0, smallReasoningMaxTokens},
		{"high hard default when no cap", ReasoningTierHigh, 0, 4096},
		// A generous cap leaves the per-tier target untouched (target <= cap).
		{"none target under a generous cap", ReasoningTierNone, 100000, noReasoningMaxTokens},
		{"low target under a generous cap", ReasoningTierLow, 100000, smallReasoningMaxTokens},
		// A tight cap clamps the per-tier target down (target > cap branch).
		{"none target clamped by tight cap", ReasoningTierNone, 128, 128},
		{"low target clamped by tight cap", ReasoningTierLow, 1024, 1024},
		// High honors a positive configured cap directly (configuredOrDefault > 0).
		{"high honors configured cap", ReasoningTierHigh, 8000, 8000},
		// Negative configured max is treated as unset by both helpers.
		{"none negative cap treated as unset", ReasoningTierNone, -5, noReasoningMaxTokens},
		{"high negative cap treated as unset", ReasoningTierHigh, -5, 4096},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.tier.maxTokens(tc.configuredMax, false); got != tc.want {
				t.Fatalf("%s.maxTokens(%d) = %d, want %d", tc.tier, tc.configuredMax, got, tc.want)
			}
		})
	}
}

// TestMaxTokensToolFloor covers the tool-capable floor branch: with tools present,
// every tier returns the full configured budget (the operator's positive cap is still
// honored), never the 512/2048 visible-answer ceiling that truncated tool-call args.
func TestMaxTokensToolFloor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		tier          ReasoningTier
		configuredMax int
		want          int
	}{
		{"none tier full default budget", ReasoningTierNone, 0, 4096},
		{"low tier full default budget", ReasoningTierLow, 0, 4096},
		{"none tier honors a generous configured cap", ReasoningTierNone, 16000, 16000},
		{"low tier still honors a tight operator cap", ReasoningTierLow, 1000, 1000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.tier.maxTokens(tc.configuredMax, true); got != tc.want {
				t.Fatalf("%s.maxTokens(%d, hasTools) = %d, want %d", tc.tier, tc.configuredMax, got, tc.want)
			}
		})
	}
}

// TestApplyAdaptiveReasoningClampsLowToTightCap proves the cappedTokens "clamp"
// arm survives end-to-end through ApplyAdaptiveReasoning: a Low tier with a
// configured cap below the small-reasoning target must emit the cap on the wire,
// not the larger per-tier default.
func TestApplyAdaptiveReasoningClampsLowToTightCap(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	reg := testRegistry()
	cfg := testConfig()
	cfg.AdaptiveReasoning = true
	cfg.MaxTokens = 900 // below smallReasoningMaxTokens (2048)

	hist := []llm.Message{
		{Role: llm.RoleSystem, Content: "system prefix"},
		{Role: llm.RoleUser, Content: "che tempo fa domani a Caraglio?"},
	}
	req := b.BuildWithReasoningTier(hist, reg, "openrouter", cfg, Budget{}, ReasoningTierLow)
	if req.MaxTokens != 900 {
		t.Fatalf("low tier with a 900 cap: MaxTokens = %d, want clamped to 900", req.MaxTokens)
	}
	if req.Reasoning.Effort != llm.ReasoningEffortLow {
		t.Fatalf("Reasoning.Effort = %q, want low", req.Reasoning.Effort)
	}
}

// TestLastGenuineUserContentNoGenuineTurn closes the empty-return arm: a history
// with no real user message (only synthetic nudges, or assistant/system roles)
// yields "" so the router does not classify a synthetic hint as the user's
// request.
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := LastGenuineUserContent(tc.hist); got != "" {
				t.Fatalf("LastGenuineUserContent = %q, want empty", got)
			}
		})
	}
}

// TestLastGenuineUserContentReturnsNewestReal complements the skip cases: among a
// mix of real and synthetic turns it returns the NEWEST genuine user message, not
// the first.
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
