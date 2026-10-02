package prompt

import (
	"github.com/chetto1983/aura/internal/llm"
	"testing"
)

func TestChatGPTFixedEffortReachesResponsesRequest(t *testing.T) {
	cfg := llm.Config{Provider: llm.ChatGPTProvider, BaseURL: llm.ChatGPTBaseURL, ShowReasoning: true,
		SupportedReasoningEfforts: []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortHigh}, ReasoningMandatory: true}
	for _, tc := range []struct{ selected, want llm.ReasoningEffort }{
		{llm.ReasoningEffortHigh, llm.ReasoningEffortHigh},
		{llm.ReasoningEffortNone, llm.ReasoningEffortLow},
		{llm.ReasoningEffortMax, llm.ReasoningEffortHigh},
	} {
		req := llm.Request{}
		ApplyFixedReasoning(&req, cfg.Provider, cfg, tc.selected)
		if req.Reasoning.Effort != tc.want || req.Reasoning.Exclude == nil || *req.Reasoning.Exclude {
			t.Fatalf("selected %q produced %#v", tc.selected, req.Reasoning)
		}
	}
}

func TestChatGPTAdaptiveEffortHonorsMandatoryModel(t *testing.T) {
	cfg := llm.Config{Provider: llm.ChatGPTProvider, BaseURL: llm.ChatGPTBaseURL, AdaptiveReasoning: true, ShowReasoning: true,
		SupportedReasoningEfforts: []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortHigh}, ReasoningMandatory: true}
	for _, tc := range []struct {
		tier ReasoningTier
		want llm.ReasoningEffort
	}{
		{ReasoningTierNone, llm.ReasoningEffortLow}, {ReasoningTierLow, llm.ReasoningEffortLow}, {ReasoningTierHigh, llm.ReasoningEffortHigh},
	} {
		req := llm.Request{}
		ApplyAdaptiveReasoning(&req, cfg.Provider, cfg, tc.tier)
		if req.Reasoning.Effort != tc.want || req.Reasoning.Exclude == nil || *req.Reasoning.Exclude {
			t.Fatalf("tier %q produced %#v", tc.tier, req.Reasoning)
		}
	}
}
