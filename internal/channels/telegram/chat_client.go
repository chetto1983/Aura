package telegramadapter

import (
	"context"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"

	tele "gopkg.in/telebot.v4"
)

// streamingChatClient implements agent.ChatClient by routing every LLM call
// through Outbound.ConsumeStream so the canonical channels/telegram path owns
// progressive Telegram edits.
type streamingChatClient struct {
	llmc            llm.Client
	model           string
	reasoningEffort string
	threadID        string // forwarded as prompt_cache_key for provider-side KV-cache reuse
	outbound        *Outbound
	teleCtx         tele.Context
	userID          string
	placeholder     *tele.Message
	pane            streamStatus // optional: when set, Outbound calls EnterContentMode at first content flush
}

func newStreamingChatClient(
	llmc llm.Client,
	model string,
	reasoningEffort string,
	threadID string,
	outbound *Outbound,
	teleCtx tele.Context,
	userID string,
	placeholder *tele.Message,
	pane streamStatus,
) agent.ChatClient {
	return &streamingChatClient{
		llmc:            llmc,
		model:           model,
		reasoningEffort: reasoningEffort,
		threadID:        threadID,
		outbound:        outbound,
		teleCtx:         teleCtx,
		userID:          userID,
		placeholder:     placeholder,
		pane:            pane,
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
		PromptCacheKey:  c.threadID,
	}
	ch, err := c.llmc.Stream(ctx, req)
	if err != nil {
		return agent.ChatResponse{Response: llm.Response{Content: llm.UserMessageFor(err)}}, err
	}
	resp, delivered, err := c.outbound.ConsumeStream(c.teleCtx, ch, c.userID, c.placeholder, c.pane)
	if err != nil {
		return agent.ChatResponse{Response: llm.Response{Content: llm.UserMessageFor(err)}}, err
	}
	return agent.ChatResponse{Response: resp, Delivered: delivered}, nil
}
