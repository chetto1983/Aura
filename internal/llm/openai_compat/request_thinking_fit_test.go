package openai_compat

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestFitThinkingBudget(t *testing.T) {
	cases := []struct {
		name                                            string
		maxTokens, budget, ceiling, wantMax, wantBudget int
	}{
		{"cap grows by the budget", 8092, 8192, 20000, 16284, 8192},
		{"ceiling binds, budget yields the answer floor", 8092, 16384, 9830, 9830, 8806},
		{"tiny cap switches thinking off", 500, 512, 20000, 1012, 0},
		{"ceiling below the cap never shrinks the cap", 8092, 512, 4000, 8092, 512},
		{"no cap: untouched", 0, 8192, 20000, 0, 8192},
		{"unlimited budget: untouched", 8092, -1, 20000, 8092, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMax, gotBudget := fitThinkingBudget(tc.maxTokens, tc.budget, tc.ceiling)
			if gotMax != tc.wantMax || gotBudget != tc.wantBudget {
				t.Fatalf("fit(%d,%d,%d) = (%d,%d), want (%d,%d)", tc.maxTokens, tc.budget, tc.ceiling, gotMax, gotBudget, tc.wantMax, tc.wantBudget)
			}
		})
	}
}

// The live 2026-08-30 shape: AURA_LLM_MAX_TOKENS=8092 under a "high" (8,192) budget on
// llama.cpp. The wire must carry max_tokens sized with the budget; OpenRouter and the
// unlimited budget stay byte-identical to before.
func TestSDKRequestThinkingBudgetFitsMaxTokens(t *testing.T) {
	local := llm.Config{Provider: "llamacpp", ContextWindow: 81920}

	high := captureSDKBody(t, local, llm.Request{
		Model: "m", MaxTokens: 8092, Reasoning: llm.ReasoningConfig{Effort: llm.ReasoningEffortHigh},
	})
	if high["max_tokens"] != float64(16284) || high["thinking_budget_tokens"] != float64(8192) {
		t.Fatalf("high: max_tokens=%v budget=%v, want 16284/8192", high["max_tokens"], high["thinking_budget_tokens"])
	}

	tiny := captureSDKBody(t, local, llm.Request{
		Model: "m", MaxTokens: 500, Reasoning: llm.ReasoningConfig{Effort: llm.ReasoningEffortLow},
	})
	kwargs, _ := tiny["chat_template_kwargs"].(map[string]any)
	if _, present := tiny["thinking_budget_tokens"]; present || kwargs["enable_thinking"] != false || tiny["max_tokens"] != float64(1012) {
		t.Fatalf("tiny cap: %#v", tiny)
	}

	unlimited := captureSDKBody(t, local, llm.Request{
		Model: "m", MaxTokens: 8092, Reasoning: llm.ReasoningConfig{Effort: llm.ReasoningEffortMax},
	})
	if unlimited["max_tokens"] != float64(8092) || unlimited["thinking_budget_tokens"] != float64(-1) {
		t.Fatalf("unlimited: %#v", unlimited)
	}

	cloud := captureSDKBody(t, llm.Config{Provider: "openrouter"}, llm.Request{
		Model: "m", MaxTokens: 8092, Reasoning: llm.ReasoningConfig{Effort: llm.ReasoningEffortHigh},
	})
	if cloud["max_tokens"] != float64(8092) {
		t.Fatalf("openrouter max_tokens = %v, want the operator cap untouched", cloud["max_tokens"])
	}
}
