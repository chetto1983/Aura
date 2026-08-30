package prompt

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestIsReasoningTarget covers the GENERALIZED fixed-path gate (D-08): true for
// OpenRouter, llama.cpp, or Ollama; false for anything else. It also re-asserts that the
// refactored IsOpenRouterReasoningTarget (now delegating to llm.ReasoningTarget)
// still returns the exact pre-refactor results — the regression guard the plan
// requires (existing callers unchanged).
func TestIsReasoningTarget(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		provider    string
		baseURL     string
		wantAny     bool // IsReasoningTarget (all supported reasoning backends)
		wantOpenRtr bool // IsOpenRouterReasoningTarget (regression: OpenRouter only)
	}{
		{"openrouter_default_url", "openrouter", "https://openrouter.ai/api/v1", true, true},
		{"openrouter_empty_url", "openrouter", "", true, true},
		{"openrouter_case_insensitive", "OpenRouter", "https://openrouter.ai/api/v1", true, true},
		{"llamacpp_fires_generalized_only", "llamacpp", "http://127.0.0.1:8080/v1", true, false},
		{"llamacpp_case_insensitive", "LlamaCpp", "", true, false},
		{"ollama_fires_generalized_only", "ollama", "http://127.0.0.1:11434/v1", true, false},
		{"vllm_local_is_none", "vllm", "http://127.0.0.1:8000/v1", false, false},
		{"openrouter_provider_foreign_url_is_none", "openrouter", "http://127.0.0.1:8080/v1", false, false},
		{"anthropic_is_none", "anthropic", "", false, false},
		{"empty_is_none", "", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsReasoningTarget(tc.provider, tc.baseURL); got != tc.wantAny {
				t.Fatalf("IsReasoningTarget(%q,%q) = %v, want %v", tc.provider, tc.baseURL, got, tc.wantAny)
			}
			if got := IsOpenRouterReasoningTarget(tc.provider, tc.baseURL); got != tc.wantOpenRtr {
				t.Fatalf("IsOpenRouterReasoningTarget(%q,%q) = %v, want %v (regression)", tc.provider, tc.baseURL, got, tc.wantOpenRtr)
			}
		})
	}
}

// TestApplyFixedReasoning covers the fixed per-turn override projection (D-04/D-08/
// D-10): a fixed effort forces req.Reasoning on ANY reasoning target (OpenRouter OR
// llama.cpp), no-ops off-target, derives exclude from cfg.ShowReasoning byte-identically
// to ReasoningTier.reasoning(), and is orthogonal to cfg.AdaptiveReasoning.
func TestApplyFixedReasoning(t *testing.T) {
	t.Parallel()

	t.Run("openrouter_high_sets_effort_and_exclude", func(t *testing.T) {
		t.Parallel()
		cfg := testConfig()
		cfg.ShowReasoning = false
		req := &llm.Request{}
		ApplyFixedReasoning(req, "openrouter", cfg, llm.ReasoningEffortHigh)
		if req.Reasoning.Effort != llm.ReasoningEffortHigh {
			t.Fatalf("Effort = %q, want high", req.Reasoning.Effort)
		}
		if req.Reasoning.Exclude == nil || !*req.Reasoning.Exclude {
			t.Fatalf("Exclude = %v, want true (ShowReasoning=false)", req.Reasoning.Exclude)
		}
	})

	t.Run("openrouter_max_serializes", func(t *testing.T) {
		t.Parallel()
		cfg := testConfig()
		req := &llm.Request{}
		ApplyFixedReasoning(req, "openrouter", cfg, llm.ReasoningEffortMax)
		if req.Reasoning.Effort != llm.ReasoningEffortMax {
			t.Fatalf("Effort = %q, want max", req.Reasoning.Effort)
		}
	})

	t.Run("llamacpp_medium_fires_generalized_path", func(t *testing.T) {
		t.Parallel()
		cfg := testConfig()
		cfg.Provider = "llamacpp"
		cfg.BaseURL = "http://127.0.0.1:8080/v1"
		req := &llm.Request{}
		ApplyFixedReasoning(req, "llamacpp", cfg, llm.ReasoningEffortMedium)
		if req.Reasoning.Effort != llm.ReasoningEffortMedium {
			t.Fatalf("llama.cpp Effort = %q, want medium (fixed path must fire for llama.cpp)", req.Reasoning.Effort)
		}
		if req.Reasoning.Exclude == nil {
			t.Fatal("llama.cpp Exclude must be set from ShowReasoning even though the wire ignores it (D-10 parity at the policy layer)")
		}
	})

	t.Run("ollama_high_fires_generalized_path", func(t *testing.T) {
		t.Parallel()
		cfg := testConfig()
		cfg.Provider = "ollama"
		cfg.BaseURL = "http://127.0.0.1:11434/v1"
		req := &llm.Request{}
		ApplyFixedReasoning(req, "ollama", cfg, llm.ReasoningEffortHigh)
		if req.Reasoning.Effort != llm.ReasoningEffortHigh {
			t.Fatalf("Ollama Effort = %q, want high", req.Reasoning.Effort)
		}
	})

	t.Run("non_reasoning_target_is_noop", func(t *testing.T) {
		t.Parallel()
		cfg := testConfig()
		cfg.Provider = "vllm"
		cfg.BaseURL = "http://127.0.0.1:8000/v1"
		req := &llm.Request{}
		ApplyFixedReasoning(req, "vllm", cfg, llm.ReasoningEffortHigh)
		if !req.Reasoning.Empty() {
			t.Fatalf("off-target request carried reasoning: %+v", req.Reasoning)
		}
	})

	t.Run("empty_effort_is_noop", func(t *testing.T) {
		t.Parallel()
		cfg := testConfig()
		req := &llm.Request{}
		ApplyFixedReasoning(req, "openrouter", cfg, "")
		if !req.Reasoning.Empty() {
			t.Fatalf("empty effort (auto) carried reasoning: %+v", req.Reasoning)
		}
	})

	// exclude is byte-identical to ReasoningTier.reasoning() — the D-10 parity gate.
	t.Run("exclude_parity_with_tier_reasoning", func(t *testing.T) {
		t.Parallel()
		for _, show := range []bool{true, false} {
			cfg := testConfig()
			cfg.ShowReasoning = show
			req := &llm.Request{}
			ApplyFixedReasoning(req, "openrouter", cfg, llm.ReasoningEffortHigh)
			wantExclude := ReasoningTierHigh.reasoning(show).Exclude
			if req.Reasoning.Exclude == nil || wantExclude == nil || *req.Reasoning.Exclude != *wantExclude {
				t.Fatalf("ShowReasoning=%v: Exclude = %v, want tier-parity %v", show, req.Reasoning.Exclude, wantExclude)
			}
		}
	})

	// The fixed override is orthogonal to the adaptive toggle: it fires even when
	// AdaptiveReasoning is OFF (the classifier being disabled must not disable the
	// explicit per-turn selector).
	t.Run("ignores_adaptive_reasoning_toggle", func(t *testing.T) {
		t.Parallel()
		cfg := testConfig()
		cfg.AdaptiveReasoning = false
		req := &llm.Request{}
		ApplyFixedReasoning(req, "openrouter", cfg, llm.ReasoningEffortLow)
		if req.Reasoning.Effort != llm.ReasoningEffortLow {
			t.Fatalf("fixed override must fire with AdaptiveReasoning=false, got %+v", req.Reasoning)
		}
	})
}

// TestBuildWithReasoningOverride proves the builder sibling threads the fixed effort
// through buildBase → ApplyFixedReasoning without disturbing messages[0] (the cache
// invariant) or max_tokens.
func TestBuildWithReasoningOverride(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	reg := testRegistry()
	cfg := testConfig()

	hist := []llm.Message{
		{Role: llm.RoleSystem, Content: "system prefix"},
		{Role: llm.RoleUser, Content: "ciao"},
	}
	before, _ := json.Marshal(hist[0])
	req := b.BuildWithReasoningOverride(hist, reg, "openrouter", cfg, Budget{}, llm.ReasoningEffortHigh, nil)
	after, _ := json.Marshal(req.Messages[0])
	if string(before) != string(after) {
		t.Fatalf("messages[0] changed: before=%s after=%s", before, after)
	}
	if req.Reasoning.Effort != llm.ReasoningEffortHigh {
		t.Fatalf("Reasoning.Effort = %q, want high", req.Reasoning.Effort)
	}
	if req.MaxTokens != cfg.MaxTokens {
		t.Fatalf("MaxTokens = %d, want unchanged %d", req.MaxTokens, cfg.MaxTokens)
	}
	// auto (empty effort) must leave the request byte-identical to a plain Build.
	got := b.BuildWithReasoningOverride(hist, reg, "openrouter", cfg, Budget{}, "", nil)
	want := b.Build(hist, reg, "openrouter", cfg, Budget{}, nil)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("empty-effort override drifted from plain Build:\n got=%+v\nwant=%+v", got, want)
	}
}

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
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hist := []llm.Message{
				{Role: llm.RoleSystem, Content: "system prefix"},
				{Role: llm.RoleUser, Content: "che tempo fa domani a Caraglio?"},
			}
			before, _ := json.Marshal(hist[0])

			req := b.BuildWithReasoningTier(hist, reg, "openrouter", cfg, Budget{}, tc.tier, nil)

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
		req := b.BuildWithReasoningTier(hist, reg, "openrouter", cfg, Budget{}, tier, nil)
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
		}, reg, "openrouter", cfg, Budget{}, ReasoningTierHigh, nil)
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
		got := b.Build(hist, reg, "openrouter", cfg, Budget{}, nil)
		want := llm.Request{
			Model:       cfg.Model,
			Messages:    hist,
			Tools:       reg.RenderToolDefs(nil),
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
		got := b.BuildWithReasoningTier(hist, reg, "openrouter", cfg, Budget{}, ReasoningTierHigh, nil)
		want := b.Build(hist, reg, "openrouter", cfg, Budget{}, nil)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("disabled adaptive reasoning drifted request:\n got=%+v\nwant=%+v", got, want)
		}
	})

	t.Run("non_openrouter_leaves_reasoning_empty", func(t *testing.T) {
		t.Parallel()
		req := b.BuildWithReasoningTier([]llm.Message{
			{Role: llm.RoleSystem, Content: "system prefix"},
			{Role: llm.RoleUser, Content: "scrivi uno script di scraping di la stampa"},
		}, reg, "anthropic", cfg, Budget{}, ReasoningTierHigh, nil)
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
		}, reg, "openrouter", cfg, Budget{}, ReasoningTierHigh, nil)
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
