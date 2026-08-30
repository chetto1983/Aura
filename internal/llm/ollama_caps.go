package llm

import "context"

// ollamaReasoningCaps is the effort set supported by Ollama's OpenAI-compatible
// /v1/chat/completions contract. The endpoint publishes no per-model effort metadata.
type ollamaReasoningCaps struct{}

var _ ReasoningCapabilitySource = (*ollamaReasoningCaps)(nil)

func (*ollamaReasoningCaps) AllowedEfforts(context.Context) ([]ReasoningEffort, ReasoningEffort, bool) {
	return []ReasoningEffort{
		ReasoningEffortNone,
		ReasoningEffortLow,
		ReasoningEffortMedium,
		ReasoningEffortHigh,
	}, "", true
}
