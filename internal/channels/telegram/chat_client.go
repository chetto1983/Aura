package telegramadapter

import (
	"context"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"

	tele "gopkg.in/telebot.v4"
)

// streamingChatClient implements agent.ChatClient by routing every LLM call
// through Outbound.ConsumeStream so the canonical channels/telegram path
// owns progressive Telegram edits.  It replaces tgtelegram.NewHubChatClient
// (legacy, lives in internal/telegram/entity_messages.go); the legacy version
// stays in place until Phase 3 US-E02 deletes it.
type streamingChatClient struct {
	llmc            llm.Client
	model           string
	reasoningEffort string
	outbound        *Outbound
	teleCtx         tele.Context
	userID          string
	placeholder     *tele.Message
}

func newStreamingChatClient(
	llmc llm.Client,
	model string,
	reasoningEffort string,
	outbound *Outbound,
	teleCtx tele.Context,
	userID string,
	placeholder *tele.Message,
) agent.ChatClient {
	return &streamingChatClient{
		llmc:            llmc,
		model:           model,
		reasoningEffort: reasoningEffort,
		outbound:        outbound,
		teleCtx:         teleCtx,
		userID:          userID,
		placeholder:     placeholder,
	}
}

// Chat builds a streaming llm.Request, hands the token channel to
// Outbound.ConsumeStream and returns the captured agent.ChatResponse.
func (c *streamingChatClient) Chat(ctx context.Context, messages []llm.Message, toolDefs []llm.ToolDefinition) (agent.ChatResponse, error) {
	req := llm.Request{
		Messages:        messages,
		Model:           c.model,
		Tools:           toolDefs,
		ReasoningEffort: c.reasoningEffort,
	}
	ch, err := c.llmc.Stream(ctx, req)
	if err != nil {
		return agent.ChatResponse{Response: llm.Response{Content: "Sorry, I couldn't process your message. Please try again."}}, err
	}
	resp, delivered, err := c.outbound.ConsumeStream(c.teleCtx, ch, c.userID, c.placeholder)
	if err != nil {
		return agent.ChatResponse{Response: llm.Response{Content: "Sorry, I couldn't process your message. Please try again."}}, err
	}
	return agent.ChatResponse{Response: resp, Delivered: delivered}, nil
}
