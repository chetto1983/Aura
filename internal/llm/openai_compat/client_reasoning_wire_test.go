package openai_compat

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestBuildWireRequestReasoningTarget is the DAEMON-FREE, coverage-load-bearing proof
// of the target-aware wire projection: the OpenRouter shape stays byte-unchanged
// (spike 096) while the net-new llama.cpp branch (spike 095) emits enable_thinking:false
// for OFF and the exact fixed thinking_budget_tokens per graduated level — never the
// OpenRouter reasoning object. A container/live-gated test would contribute ZERO gate
// coverage (CLAUDE.md), so this pure table test is what actually covers the branch.
func TestBuildWireRequestReasoningTarget(t *testing.T) {
	t.Run("openrouter unchanged", func(t *testing.T) {
		c := New(llm.Config{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1"})
		efforts := []llm.ReasoningEffort{
			llm.ReasoningEffortNone,
			llm.ReasoningEffortLow,
			llm.ReasoningEffortMedium,
			llm.ReasoningEffortHigh,
			llm.ReasoningEffortXHigh,
			llm.ReasoningEffortMax,
		}
		for _, eff := range efforts {
			t.Run(string(eff), func(t *testing.T) {
				wire := c.buildWireRequest(llm.Request{Reasoning: llm.ReasoningConfig{Effort: eff}})
				if wire.Reasoning == nil {
					t.Fatalf("Reasoning = nil, want the OpenRouter object for effort %q", eff)
				}
				if wire.Reasoning.Effort != string(eff) {
					t.Errorf("Reasoning.Effort = %q, want %q", wire.Reasoning.Effort, eff)
				}
				if wire.ChatTemplateKwargs != nil {
					t.Errorf("ChatTemplateKwargs = %v, want nil on the OpenRouter path", wire.ChatTemplateKwargs)
				}
				if wire.ThinkingBudgetTokens != nil {
					t.Errorf("ThinkingBudgetTokens = %d, want nil on the OpenRouter path", *wire.ThinkingBudgetTokens)
				}
			})
		}

		// Empty reasoning (auto) → no reasoning object at all — unchanged behavior.
		wire := c.buildWireRequest(llm.Request{})
		if wire.Reasoning != nil {
			t.Errorf("Reasoning = %+v, want nil for empty reasoning", wire.Reasoning)
		}
	})

	t.Run("llamacpp branch", func(t *testing.T) {
		c := New(llm.Config{Provider: "llamacpp", BaseURL: "http://localhost:8080/v1"})

		t.Run("off sets enable_thinking false", func(t *testing.T) {
			wire := c.buildWireRequest(llm.Request{Reasoning: llm.ReasoningConfig{Effort: llm.ReasoningEffortNone}})
			if wire.Reasoning != nil {
				t.Errorf("Reasoning = %+v, want nil on the llama.cpp branch", wire.Reasoning)
			}
			if wire.ThinkingBudgetTokens != nil {
				t.Errorf("ThinkingBudgetTokens = %d, want nil for OFF", *wire.ThinkingBudgetTokens)
			}
			v, ok := wire.ChatTemplateKwargs["enable_thinking"].(bool)
			if !ok || v {
				t.Errorf("ChatTemplateKwargs[enable_thinking] = %v (ok=%v), want false", wire.ChatTemplateKwargs["enable_thinking"], ok)
			}
		})

		budgets := []struct {
			effort llm.ReasoningEffort
			want   int
		}{
			{llm.ReasoningEffortLow, 512},
			{llm.ReasoningEffortMedium, 2048},
			{llm.ReasoningEffortHigh, 8192},
			{llm.ReasoningEffortXHigh, 16384},
			{llm.ReasoningEffortMax, -1},
		}
		for _, tc := range budgets {
			t.Run("budget "+string(tc.effort), func(t *testing.T) {
				wire := c.buildWireRequest(llm.Request{Reasoning: llm.ReasoningConfig{Effort: tc.effort}})
				if wire.Reasoning != nil {
					t.Errorf("Reasoning = %+v, want nil on the llama.cpp branch", wire.Reasoning)
				}
				if wire.ChatTemplateKwargs != nil {
					t.Errorf("ChatTemplateKwargs = %v, want nil for a graduated budget", wire.ChatTemplateKwargs)
				}
				if wire.ThinkingBudgetTokens == nil {
					t.Fatalf("ThinkingBudgetTokens = nil, want %d for effort %q", tc.want, tc.effort)
				}
				if *wire.ThinkingBudgetTokens != tc.want {
					t.Errorf("ThinkingBudgetTokens = %d, want %d for effort %q", *wire.ThinkingBudgetTokens, tc.want, tc.effort)
				}
			})
		}

		t.Run("auto emits no reasoning fields", func(t *testing.T) {
			wire := c.buildWireRequest(llm.Request{}) // empty reasoning
			if wire.Reasoning != nil || wire.ChatTemplateKwargs != nil || wire.ThinkingBudgetTokens != nil {
				t.Errorf("auto: reasoning=%+v kwargs=%v budget=%v, want all nil",
					wire.Reasoning, wire.ChatTemplateKwargs, wire.ThinkingBudgetTokens)
			}
		})
	})
}
