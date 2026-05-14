package agent

import (
	"context"

	"github.com/aura/aura/internal/llm"
)

// noStreamClient adapts llm.Client.Send to the ChatClient interface.
// Background agents (swarm workers, scheduler jobs, /api/chat pipe) use
// llm.Client.Send rather than Stream: there is no Telegram message to
// progressively edit. Streaming asymmetry is intentional — see the Runner
// doc comment in internal/agent/runner.go.
type noStreamClient struct {
	client          llm.Client
	model           string
	temperature     *float64
	reasoningEffort string
}

// NewNoStreamClient returns a ChatClient that delegates each Chat call to
// llm.Client.Send with the given per-run parameters.
func NewNoStreamClient(client llm.Client, model string, temperature *float64, reasoningEffort string) ChatClient {
	return &noStreamClient{
		client:          client,
		model:           model,
		temperature:     temperature,
		reasoningEffort: reasoningEffort,
	}
}

func (c *noStreamClient) Chat(ctx context.Context, messages []llm.Message, toolDefs []llm.ToolDefinition) (ChatResponse, error) {
	resp, err := c.client.Send(ctx, llm.Request{
		Messages:        messages,
		Model:           c.model,
		Temperature:     c.temperature,
		Tools:           toolDefs,
		ReasoningEffort: c.reasoningEffort,
	})
	return ChatResponse{Response: resp, Delivered: false}, err
}
