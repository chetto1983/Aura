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

	// This build runs with testRegistry() (tools present), so every tier now keeps
	// the full configured output budget (4096): the per-tier 512/2048 ceiling only
	// ever meant to bound a pure-chat visible answer, and starving a tool-capable turn
	// truncated tool-call arguments mid-JSON (the 203-turn disaster, 2026-06-14). The
	// tier distinction lives in Reasoning.Effort (asserted below), not max_tokens.
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

// TestAdaptiveReasoningToolTurnNotStarved is the regression guard for the 2026-06-14
// 203-turn truncation disaster: a turn that CAN emit tool calls must keep the full
// output budget even at the none/low reasoning tier. The tier ceiling bounds the
// VISIBLE answer, but a tool call's arguments (a fs_write file body or a shell_exec
// script) ARE the output and get cut mid-JSON at 512/2048 -> "unexpected end of JSON
// input" + a retry loop the dedup ring can't catch. Reasoning EFFORT still tiers.
func TestAdaptiveReasoningToolTurnNotStarved(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.AdaptiveReasoning = true
	want := configuredOrDefault(cfg.MaxTokens)
	for _, tier := range []ReasoningTier{ReasoningTierNone, ReasoningTierLow, ReasoningTierHigh} {
		req := &llm.Request{Tools: []llm.ToolDef{{}}, MaxTokens: cfg.MaxTokens}
		ApplyAdaptiveReasoning(req, cfg.Provider, cfg, tier)
		if req.MaxTokens != want {
			t.Fatalf("tier %q with tools: MaxTokens = %d, want full budget %d (not starved)", tier, req.MaxTokens, want)
		}
	}
}

// TestAdaptiveReasoningNoToolTurnKeepsTierCeiling proves the reasoning-tier output
// reduction still applies when a turn genuinely has NO tools to call — the ceiling
// only ever meant to keep a pure-chat visible answer short.
func TestAdaptiveReasoningNoToolTurnKeepsTierCeiling(t *testing.T) {
	t.Parallel()
	cfg := testConfig()
	cfg.AdaptiveReasoning = true
	cases := []struct {
		tier ReasoningTier
		want int
	}{
		{ReasoningTierNone, 512},
		{ReasoningTierLow, 2048},
	}
	for _, tc := range cases {
		req := &llm.Request{MaxTokens: cfg.MaxTokens} // no tools
		ApplyAdaptiveReasoning(req, cfg.Provider, cfg, tc.tier)
		if req.MaxTokens != tc.want {
			t.Fatalf("tier %q no tools: MaxTokens = %d, want %d", tc.tier, req.MaxTokens, tc.want)
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
