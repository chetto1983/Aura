package telegram

import (
	"context"
	"strings"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"

	tele "gopkg.in/telebot.v4"
)

// FinalizeTerminalTool is the exported surface of finalizeTerminalToolWithNoToolLLM.
// Used by internal/channels/telegram.InvocationBuilder.
func (b *Bot) FinalizeTerminalTool(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, placeholder *tele.Message, rawToolResult string, stats *agent.TurnStats) (string, bool) {
	return b.finalizeTerminalToolWithNoToolLLM(ctx, c, convCtx, userID, placeholder, rawToolResult, stats)
}

func (b *Bot) finalizeTerminalToolWithNoToolLLM(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, placeholder *tele.Message, rawToolResult string, stats *agent.TurnStats) (string, bool) {
	stats.LLMCalls++
	stats.LoopSteps++
	finalized := agent.FinalizeTerminalTool(ctx, agent.TerminalFinalizationInput{
		Messages:      convCtx.Messages(),
		TerminalTool:  stats.TerminalTool,
		RawToolResult: rawToolResult,
		Model:         b.cfg.LLMModel,
		Send: func(ctx context.Context, req llm.Request) (llm.Response, error) {
			req.ReasoningEffort = b.cfg.ReasoningEffort
			return b.rt.llm.Send(ctx, req)
		},
		RecordUsage: func(usage llm.TokenUsage) {
			if b.rt != nil && b.rt.budget != nil {
				b.rt.budget.RecordUsage(usage)
			}
		},
		EstimateCost: func(usage llm.TokenUsage) float64 {
			return agent.EstimateUsageCost(usage, b.cfg.CostInputPerMTokens, b.cfg.CostOutputPerMTokens)
		},
	})
	if finalized.Err != nil {
		b.logger.Warn("terminal tool finalization LLM failed", "user_id", userID, "terminal_tool", stats.TerminalTool, "error", finalized.Err)
	}
	convCtx.TrackTokens(finalized.Usage)
	stats.TokensPrompt += finalized.TokensPrompt
	stats.TokensCompletion += finalized.TokensCompletion
	stats.TokensTotal += finalized.TokensTotal
	stats.CostUSD += finalized.CostUSD
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
