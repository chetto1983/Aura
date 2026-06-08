package prompt

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestAdaptiveReasoningPolicy(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	reg := testRegistry()
	cfg := testConfig()
	cfg.AdaptiveReasoning = true

	cases := []struct {
		name       string
		query      string
		wantEffort llm.ReasoningEffort
		wantExcl   bool
		wantTokens int
	}{
		{
			name:       "greeting_no_reasoning",
			query:      "ciao",
			wantEffort: llm.ReasoningEffortNone,
			wantExcl:   true,
			wantTokens: 512,
		},
		{
			name:       "news_search_small_reasoning",
			query:      "cerca notizie di cuneo",
			wantEffort: llm.ReasoningEffortLow,
			wantExcl:   false,
			wantTokens: 2048,
		},
		{
			name:       "scraping_script_deep_reasoning",
			query:      "scrivi uno script di scraping di la stampa",
			wantEffort: llm.ReasoningEffortHigh,
			wantExcl:   false,
			wantTokens: 4096,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hist := []llm.Message{
				{Role: llm.RoleSystem, Content: "system prefix"},
				{Role: llm.RoleUser, Content: tc.query},
			}
			before, _ := json.Marshal(hist[0])

			req := b.Build(hist, reg, "openrouter", cfg, Budget{})

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
			if req.ToolChoice != "" {
				t.Fatalf("ToolChoice = %q, want unchanged empty default", req.ToolChoice)
			}
		})
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
		req := b.Build([]llm.Message{
			{Role: llm.RoleSystem, Content: "system prefix"},
			{Role: llm.RoleUser, Content: "scrivi uno script di scraping di la stampa"},
		}, reg, "openrouter", cfg, Budget{})
		if req.MaxTokens != 1000 {
			t.Fatalf("MaxTokens = %d, want configured cap 1000", req.MaxTokens)
		}
		if req.Reasoning.Effort != llm.ReasoningEffortHigh {
			t.Fatalf("Reasoning.Effort = %q, want high", req.Reasoning.Effort)
		}
	})

	t.Run("disabled_leaves_request_unchanged", func(t *testing.T) {
		t.Parallel()
		cfg := cfg
		cfg.AdaptiveReasoning = false
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
			t.Fatalf("disabled adaptive reasoning drifted request:\n got=%+v\nwant=%+v", got, want)
		}
	})

	t.Run("non_openrouter_leaves_reasoning_empty", func(t *testing.T) {
		t.Parallel()
		req := b.Build([]llm.Message{
			{Role: llm.RoleSystem, Content: "system prefix"},
			{Role: llm.RoleUser, Content: "scrivi uno script di scraping di la stampa"},
		}, reg, "anthropic", cfg, Budget{})
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
		req := b.Build([]llm.Message{
			{Role: llm.RoleSystem, Content: "system prefix"},
			{Role: llm.RoleUser, Content: "scrivi uno script di scraping di la stampa"},
		}, reg, "openrouter", cfg, Budget{})
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
		req := b.Build(hist, reg, "openrouter", cfg, Budget{})
		if req.Reasoning.Effort != llm.ReasoningEffortHigh {
			t.Fatalf("Reasoning.Effort = %q, want high from original user request", req.Reasoning.Effort)
		}
	})
}
