package telegram

import (
	"context"
	"strings"

	"github.com/aura/aura/internal/agentruntime"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"

	tele "gopkg.in/telebot.v4"
)

func (b *Bot) finalizeTerminalToolWithNoToolLLM(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, placeholder *tele.Message, rawToolResult string, stats *turnStats) (string, bool) {
	stats.llmCalls++
	stats.loopSteps++
	finalized := agentruntime.FinalizeTerminalTool(ctx, agentruntime.TerminalFinalizationInput{
		Messages:      convCtx.Messages(),
		TerminalTool:  stats.terminalTool,
		RawToolResult: rawToolResult,
		Model:         b.cfg.LLMModel,
		Send: func(ctx context.Context, req llm.Request) (llm.Response, error) {
			return b.llm.Send(ctx, req)
		},
		RecordUsage: func(usage llm.TokenUsage) {
			if b.budget != nil {
				b.budget.RecordUsage(usage)
			}
		},
		EstimateCost: func(usage llm.TokenUsage) float64 {
			return estimateUsageCost(usage, b.cfg.CostInputPerMTokens, b.cfg.CostOutputPerMTokens)
		},
	})
	if finalized.Err != nil {
		b.logger.Warn("terminal tool finalization LLM failed", "user_id", userID, "terminal_tool", stats.terminalTool, "error", finalized.Err)
	}
	convCtx.TrackTokens(finalized.Usage)
	stats.tokensPrompt += finalized.TokensPrompt
	stats.tokensCompletion += finalized.TokensCompletion
	stats.tokensTotal += finalized.TokensTotal
	stats.costUSD += finalized.CostUSD
	response := strings.TrimSpace(finalized.Text)
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
