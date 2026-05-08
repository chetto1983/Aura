package telegram

import (
	"context"
	"strings"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"

	tele "gopkg.in/telebot.v4"
)

func (b *Bot) finalizeTerminalToolWithNoToolLLM(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, placeholder *tele.Message, rawToolResult string, stats *turnStats) (string, bool) {
	req := llm.Request{
		Messages: terminalToolFinalizationMessages(convCtx.Messages(), stats.terminalTool),
		Model:    b.cfg.LLMModel,
		Tools:    nil,
	}
	stats.llmCalls++
	stats.loopSteps++
	resp, err := b.llm.Send(ctx, req)
	if err != nil {
		b.logger.Warn("terminal tool finalization LLM failed", "user_id", userID, "terminal_tool", stats.terminalTool, "error", err)
		response := terminalToolFallbackResponse(stats.terminalTool, rawToolResult)
		convCtx.AddAssistantMessage(response)
		return response, false
	}
	convCtx.TrackTokens(resp.Usage)
	stats.tokensPrompt += resp.Usage.PromptTokens
	stats.tokensCompletion += resp.Usage.CompletionTokens
	stats.tokensTotal += resp.Usage.TotalTokens
	stats.costUSD += estimateUsageCost(resp.Usage, b.cfg.CostInputPerMTokens, b.cfg.CostOutputPerMTokens)
	if b.budget != nil {
		b.budget.RecordUsage(resp.Usage)
	}
	response := strings.TrimSpace(resp.Content)
	if response == "" || resp.HasToolCalls || looksLikeToolCallMarkup(response) {
		response = terminalToolFallbackResponse(stats.terminalTool, rawToolResult)
	}
	convCtx.AddAssistantMessage(response)
	if c != nil {
		parts := renderForTelegramEntities(response)
		if len(parts) > 0 {
			if placeholder != nil {
				if _, err := b.editAssistantMessage(c.Bot(), placeholder, parts[0], response); err == nil {
					b.sendAssistantRemainder(c.Bot(), c.Recipient(), parts, 1)
					return response, true
				}
			}
			if sent, err := c.Bot().Send(c.Recipient(), parts[0].Text, parts[0].Entities); err == nil {
				b.sendAssistantRemainder(c.Bot(), c.Recipient(), parts, 1)
				_ = sent
				return response, true
			}
		}
	}
	return response, false
}
