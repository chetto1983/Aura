package webadapter

import (
	"context"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"
)

type streamingChatClient struct {
	llmc            llm.Client
	model           string
	reasoningEffort string
	threadID        string
	router          *StreamRouter
}

// NewStreamingChatClient adapts llm.Client.Stream to the agent.ChatClient
// contract while writing live token deltas to the active web StreamRouter.
func NewStreamingChatClient(
	llmc llm.Client,
	model string,
	reasoningEffort string,
	threadID string,
	router *StreamRouter,
) agent.ChatClient {
	return &streamingChatClient{
		llmc:            llmc,
		model:           model,
		reasoningEffort: reasoningEffort,
		threadID:        threadID,
		router:          router,
	}
}

func (c *streamingChatClient) Chat(ctx context.Context, messages []llm.Message, toolDefs []llm.ToolDefinition) (agent.ChatResponse, error) {
	req := llm.Request{
		Messages:        messages,
		Model:           c.model,
		Tools:           toolDefs,
		ReasoningEffort: c.reasoningEffort,
		PromptCacheKey:  c.threadID,
	}
	ch, err := c.llmc.Stream(ctx, req)
	if err != nil {
		return agent.ChatResponse{Response: llm.Response{Content: llm.UserMessageFor(err)}}, err
	}
	if c.router == nil {
		resp, _, err := drainTokenStream(ctx, ch)
		if err != nil {
			return agent.ChatResponse{Response: llm.Response{Content: llm.UserMessageFor(err)}}, err
		}
		return agent.ChatResponse{Response: resp}, nil
	}
	resp, delivered, err := c.router.ConsumeTokens(ctx, c.threadID, ch)
	if err != nil {
		return agent.ChatResponse{Response: llm.Response{Content: llm.UserMessageFor(err)}}, err
	}
	return agent.ChatResponse{Response: resp, Delivered: delivered}, nil
}
