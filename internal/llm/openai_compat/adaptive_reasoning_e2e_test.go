package openai_compat

import (
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

func TestAdaptiveReasoningTierWireE2E(t *testing.T) {
	builder := prompt.NewPromptBuilder()
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	cfg := llm.Config{
		Provider:          "openrouter",
		BaseURL:           "https://openrouter.ai/api/v1",
		Model:             "deepseek/deepseek-v4-flash:exacto",
		Temperature:       0.2,
		MaxTokens:         4096,
		AdaptiveReasoning: true,
	}

	cases := []struct {
		tier      prompt.ReasoningTier
		effort    string
		exclude   bool
		maxTokens int
	}{
		{prompt.ReasoningTierNone, "none", true, 512},
		{prompt.ReasoningTierLow, "low", false, 2048},
		{prompt.ReasoningTierHigh, "high", false, 4096},
	}

	for _, tc := range cases {
		t.Run(string(tc.tier), func(t *testing.T) {
			history := []llm.Message{
				{Role: llm.RoleSystem, Content: "STATIC-AURA-SYSTEM"},
				{Role: llm.RoleUser, Content: "che tempo fa domani a Caraglio?"},
			}
			req := builder.BuildWithReasoningTier(history, reg, cfg.Provider, cfg, prompt.Budget{}, tc.tier)
			body := decodeBody(t, captureBody(t, req))
			effort, exclude, ok := reasoningFields(body)
			gotMax, _ := body["max_tokens"].(float64)
			choice, _ := body["tool_choice"].(string)
			_, toolsPresent := body["tools"]
			if !ok || effort != tc.effort || exclude != tc.exclude || int(gotMax) != tc.maxTokens ||
				choice != "auto" || !toolsPresent {
				t.Fatalf("tier %q projected wrong: got effort=%q exclude=%v max_tokens=%d tool_choice=%q tools=%v, want effort=%q exclude=%v max_tokens=%d tool_choice=auto tools=true",
					tc.tier, effort, exclude, int(gotMax), choice, toolsPresent,
					tc.effort, tc.exclude, tc.maxTokens)
			}
		})
	}
}

func decodeBody(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(b, &body); err != nil {
		t.Fatalf("request body not JSON: %v", err)
	}
	return body
}

func reasoningFields(body map[string]any) (effort string, exclude bool, ok bool) {
	reasoning, _ := body["reasoning"].(map[string]any)
	if reasoning == nil {
		return "", false, false
	}
	effort, _ = reasoning["effort"].(string)
	exclude, _ = reasoning["exclude"].(bool)
	return effort, exclude, effort != ""
}
