package conversations

import (
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/llm"
)

// finalRequestEnvelope is the model-visible portion of the OpenAI-compatible
// request. Counting the serialized envelope includes roles, tool-call framing,
// tool names/descriptions/schemas, and JSON punctuation in addition to message
// content, exactly as it is on the provider wire.
type finalRequestEnvelope struct {
	Messages []llm.Message `json:"messages"`
	Tools    []llm.ToolDef `json:"tools,omitempty"`
}

// ValidateFinalRequestBudget estimates the complete, already-assembled prompt
// immediately before transport and rejects it when it cannot fit beside the
// configured output and provider-tokenizer reserves.
//
// The history ladder deliberately excludes the system prompt and tool manifest,
// reserving llm.PromptHeadroom(ContextWindow) for them. This final guard counts those bytes for
// real and compares the complete envelope against:
//
//	ContextWindow - max(MaxOutputTokens, 20000) - provider error reserve
//
// A zero ContextWindow keeps hand-built legacy test clients compatible. Boot
// validation rejects zero in production.
func ValidateFinalRequestBudget(req llm.Request, cfg llm.Config) (int, error) {
	if cfg.ContextWindow <= 0 {
		return 0, nil
	}

	tools := req.Tools
	if req.ToolChoice == "none" {
		tools = nil
	}
	wire, err := json.Marshal(finalRequestEnvelope{
		Messages: req.Messages,
		Tools:    tools,
	})
	if err != nil {
		return 0, fmt.Errorf("marshal final model request for token preflight: %w", err)
	}
	enc, err := encoder()
	if err != nil {
		return 0, fmt.Errorf("final model request token encoder: %w", err)
	}
	estimate := countTokens(enc, string(wire))
	cap := cfg.ContextWindow -
		max(cfg.MaxOutputTokens, l2MinOutputReservation) -
		llm.ProviderErrorReserveTokens(cfg)
	if cap <= 0 || estimate > cap {
		return estimate, fmt.Errorf(
			"%w (final request %d tokens, cap %d; output reserve %d, provider reserve %d)",
			ErrContextWindowExceeded,
			estimate,
			cap,
			max(cfg.MaxOutputTokens, l2MinOutputReservation),
			llm.ProviderErrorReserveTokens(cfg),
		)
	}
	return estimate, nil
}
