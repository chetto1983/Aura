package prompt

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestAdaptiveReasoningTierApplication(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	reg := testRegistry()
	cfg := testConfig()
	cfg.AdaptiveReasoning = true

	// Adaptive reasoning never touches max_tokens (the 2026-06-14 contract: capping it
	// by tier truncated tool-call arguments mid-JSON — the 203-turn disaster), so every
	// tier leaves the configured budget (cfg.MaxTokens = 4096) unchanged. The tier
	// distinction lives entirely in Reasoning.Effort (asserted below).
	cases := []struct {
		name       string
		tier       ReasoningTier
		wantEffort llm.ReasoningEffort
		wantExcl   bool
		wantTokens int
	}{
		{
			name:       "none_keeps_tools_visible",
			tier:       ReasoningTierNone,
			wantEffort: llm.ReasoningEffortNone,
			wantExcl:   true,
			wantTokens: 4096,
		},
		{
			name:       "low_reasoning",
			tier:       ReasoningTierLow,
			wantEffort: llm.ReasoningEffortLow,
			wantExcl:   true,
			wantTokens: 4096,
		},
		{
			name:       "high_reasoning",
			tier:       ReasoningTierHigh,
			wantEffort: llm.ReasoningEffortHigh,
			wantExcl:   true,
			wantTokens: 4096,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hist := []llm.Message{
				{Role: llm.RoleSystem, Content: "system prefix"},
				{Role: llm.RoleUser, Content: "che tempo fa domani a Caraglio?"},
			}
			before, _ := json.Marshal(hist[0])

			req := b.BuildWithReasoningTier(hist, reg, "openrouter", cfg, Budget{}, tc.tier)

			after, _ := json.Marshal(req.Messages[0])
			if string(before) != string(after) {
				t.Fatalf("messages[0] changed: before=%s after=%s", before, after)
			}
			if req.MaxTokens != tc.wantTokens {
				t.Fatalf("MaxTokens = %d, want %d", req.MaxTokens, tc.wantTokens)
			}
			if req.Reasoning.Effort != tc.wantEffort {
				t.Fatalf("Reasoning.Effort = %q, want %q", req.Reasoning.Effort, tc.wantEffort)
			}
			if req.Reasoning.Exclude == nil || *req.Reasoning.Exclude != tc.wantExcl {
				t.Fatalf("Reasoning.Exclude = %v, want %v", req.Reasoning.Exclude, tc.wantExcl)
			}
			if req.ToolChoice == "none" {
				t.Fatal("adaptive reasoning must not force ToolChoice=none; deferred tools must stay discoverable")
			}
			if len(req.Tools) == 0 {
				t.Fatal("adaptive reasoning must keep tools in the main request")
			}
		})
	}
}

// TestAdaptiveReasoningNeverTouchesMaxTokens is the contract: adaptive reasoning sets
// ONLY the reasoning effort (off/low/high) — it never changes max_tokens. Capping the
// output budget by tier truncated tool-call arguments mid-JSON (the 203-turn disaster,
// 2026-06-14), so the operator-configured cfg.MaxTokens survives unchanged for every
// tier, with or without tools.
func TestAdaptiveReasoningNeverTouchesMaxTokens(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.AdaptiveReasoning = true
	cfg.MaxTokens = 4096
	for _, withTools := range []bool{true, false} {
		for _, tier := range []ReasoningTier{ReasoningTierNone, ReasoningTierLow, ReasoningTierHigh} {
			req := &llm.Request{MaxTokens: cfg.MaxTokens}
			if withTools {
				req.Tools = []llm.ToolDef{{}}
			}
			ApplyAdaptiveReasoning(req, cfg.Provider, cfg, tier)
			if req.MaxTokens != cfg.MaxTokens {
				t.Fatalf("tier %q tools=%v: MaxTokens = %d, want unchanged %d", tier, withTools, req.MaxTokens, cfg.MaxTokens)
			}
		}
	}
}

// TestAdaptiveReasoningShowReasoningUnexcludes proves the AURA_SHOW_REASONING master
// switch flips exclude off so the provider streams the real CoT for display. Without
// this the live window is dead: exclude:true yields zero reasoning deltas (verified).
func TestAdaptiveReasoningShowReasoningUnexcludes(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	reg := testRegistry()
	cfg := testConfig()
	cfg.AdaptiveReasoning = true
	cfg.ShowReasoning = true

	hist := []llm.Message{
		{Role: llm.RoleSystem, Content: "system prefix"},
		{Role: llm.RoleUser, Content: "scrivi uno script di scraping di la stampa"},
	}
	for _, tier := range []ReasoningTier{ReasoningTierNone, ReasoningTierLow, ReasoningTierHigh} {
		req := b.BuildWithReasoningTier(hist, reg, "openrouter", cfg, Budget{}, tier)
		if req.Reasoning.Exclude == nil || *req.Reasoning.Exclude {
			t.Fatalf("tier %q with ShowReasoning: Exclude = %v, want false (stream the CoT)", tier, req.Reasoning.Exclude)
		}
	}
}

func TestAdaptiveReasoningPolicyBoundaries(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	reg := testRegistry()
	cfg := testConfig()
	cfg.AdaptiveReasoning = true

	t.Run("respects_configured_max_token_cap", func(t *testing.T) {
		t.Parallel()
		cfg := cfg
		cfg.MaxTokens = 1000
		req := b.BuildWithReasoningTier([]llm.Message{
			{Role: llm.RoleSystem, Content: "system prefix"},
			{Role: llm.RoleUser, Content: "scrivi uno script di scraping di la stampa"},
		}, reg, "openrouter", cfg, Budget{}, ReasoningTierHigh)
		if req.MaxTokens != 1000 {
			t.Fatalf("MaxTokens = %d, want configured cap 1000", req.MaxTokens)
		}
		if req.Reasoning.Effort != llm.ReasoningEffortHigh {
			t.Fatalf("Reasoning.Effort = %q, want high", req.Reasoning.Effort)
		}
	})

	t.Run("plain_build_leaves_request_unchanged", func(t *testing.T) {
		t.Parallel()
		hist := seedHistory()
		got := b.Build(hist, reg, "openrouter", cfg, Budget{})
		want := llm.Request{
			Model:       cfg.Model,
			Messages:    hist,
			Tools:       reg.RenderToolDefs(),
			Temperature: cfg.Temperature,
			MaxTokens:   cfg.MaxTokens,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("plain Build drifted request:\n got=%+v\nwant=%+v", got, want)
		}
	})

	t.Run("disabled_leaves_request_unchanged", func(t *testing.T) {
		t.Parallel()
		cfg := cfg
		cfg.AdaptiveReasoning = false
		hist := seedHistory()
		got := b.BuildWithReasoningTier(hist, reg, "openrouter", cfg, Budget{}, ReasoningTierHigh)
		want := b.Build(hist, reg, "openrouter", cfg, Budget{})
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("disabled adaptive reasoning drifted request:\n got=%+v\nwant=%+v", got, want)
		}
	})

	t.Run("non_openrouter_leaves_reasoning_empty", func(t *testing.T) {
		t.Parallel()
		req := b.BuildWithReasoningTier([]llm.Message{
			{Role: llm.RoleSystem, Content: "system prefix"},
			{Role: llm.RoleUser, Content: "scrivi uno script di scraping di la stampa"},
		}, reg, "anthropic", cfg, Budget{}, ReasoningTierHigh)
		if !req.Reasoning.Empty() {
			t.Fatalf("non-openrouter request carried reasoning: %+v", req.Reasoning)
		}
		if req.MaxTokens != cfg.MaxTokens {
			t.Fatalf("MaxTokens = %d, want unchanged %d", req.MaxTokens, cfg.MaxTokens)
		}
	})

	t.Run("local_openai_compat_endpoint_leaves_reasoning_empty", func(t *testing.T) {
		t.Parallel()
		cfg := cfg
		cfg.BaseURL = "http://127.0.0.1:8080/v1"
		req := b.BuildWithReasoningTier([]llm.Message{
			{Role: llm.RoleSystem, Content: "system prefix"},
			{Role: llm.RoleUser, Content: "scrivi uno script di scraping di la stampa"},
		}, reg, "openrouter", cfg, Budget{}, ReasoningTierHigh)
		if !req.Reasoning.Empty() {
			t.Fatalf("local endpoint request carried reasoning: %+v", req.Reasoning)
		}
		if req.MaxTokens != cfg.MaxTokens {
			t.Fatalf("MaxTokens = %d, want unchanged %d", req.MaxTokens, cfg.MaxTokens)
		}
	})

	t.Run("skips_synthetic_user_nudges", func(t *testing.T) {
		t.Parallel()
		hist := []llm.Message{
			{Role: llm.RoleSystem, Content: "system prefix"},
			{Role: llm.RoleUser, Content: "scrivi uno script di scraping di la stampa"},
			{Role: llm.RoleAssistant, Content: "working"},
			{Role: llm.RoleUser, Content: "Stop calling tools. Using only the tool results already gathered above, write the final answer to the user's original question now."},
		}
		if got := LastGenuineUserContent(hist); got != "scrivi uno script di scraping di la stampa" {
			t.Fatalf("LastGenuineUserContent = %q", got)
		}
	})

	t.Run("skips_empty_response_recovery_nudge", func(t *testing.T) {
		t.Parallel()
		hist := []llm.Message{
			{Role: llm.RoleSystem, Content: "system prefix"},
			{Role: llm.RoleUser, Content: "fai una ricerca e rispondi oggi"},
			{Role: llm.RoleAssistant, Content: ""},
			{Role: llm.RoleUser, Content: "Your last response was empty. Provide a concise final answer now."},
		}
		if got := LastGenuineUserContent(hist); got != "fai una ricerca e rispondi oggi" {
			t.Fatalf("LastGenuineUserContent = %q", got)
		}
	})

	t.Run("skips_synthetic_time_hint", func(t *testing.T) {
		t.Parallel()
		hist := []llm.Message{
			{Role: llm.RoleSystem, Content: "system prefix"},
			{Role: llm.RoleUser, Content: "crea un xlsx con la data di oggi"},
			{Role: llm.RoleUser, Content: "<current_time>2026-06-09T12:34:56+02:00</current_time>\n<today>2026-06-09</today>"},
		}
		if got := LastGenuineUserContent(hist); got != "crea un xlsx con la data di oggi" {
			t.Fatalf("LastGenuineUserContent = %q", got)
		}
	})
}
