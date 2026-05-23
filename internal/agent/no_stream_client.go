package agent

import (
	"context"

	"github.com/aura/aura/internal/llm"
)

// noStreamClient adapts llm.Client.Send to the ChatClient interface.
// Background agents (swarm workers, scheduler jobs, /api/chat pipe) use
// llm.Client.Send rather than Stream: there is no Telegram message to
// progressively edit. Streaming asymmetry is intentional.
type noStreamClient struct {
	client          llm.Client
	model           string
	temperature     *float64
	reasoningEffort string
	promptCacheKey  string
}

// NewNoStreamClient returns a ChatClient that delegates each Chat call to
// llm.Client.Send with the given per-run parameters.
// promptCacheKey, when non-empty, is forwarded as prompt_cache_key on every
// LLM request so providers can reuse the KV-cached static prefix across turns.
func NewNoStreamClient(client llm.Client, model string, temperature *float64, reasoningEffort string, promptCacheKey string) ChatClient {
	return &noStreamClient{
		client:          client,
		model:           model,
		temperature:     temperature,
		reasoningEffort: reasoningEffort,
		promptCacheKey:  promptCacheKey,
	}
}

func (c *noStreamClient) Chat(ctx context.Context, messages []llm.Message, toolDefs []llm.ToolDefinition) (ChatResponse, error) {
	resp, err := c.client.Send(ctx, llm.Request{
		Messages:        messages,
		Model:           c.model,
		Temperature:     c.temperature,
		Tools:           toolDefs,
		ReasoningEffort: c.reasoningEffort,
		PromptCacheKey:  c.promptCacheKey,
	})
	return ChatResponse{Response: resp, Delivered: false}, err
}
