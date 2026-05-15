package telegram

import (
	"context"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/conversation"

	tele "gopkg.in/telebot.v4"
)

// FinalizeTerminalTool finalizes a terminal tool turn: calls the LLM for a prose
// summary then edits the Telegram placeholder with the result.
// Used by internal/channels/telegram.InvocationBuilder.
func (b *Bot) FinalizeTerminalTool(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, placeholder *tele.Message, rawToolResult string, stats *agent.TurnStats) (string, bool) {
	response, ok := agent.FinalizeAfterTerminalTool(ctx, b, convCtx, rawToolResult, stats)
	if !ok {
		b.logger.Warn("terminal tool finalization failed", "user_id", userID, "terminal_tool", stats.TerminalTool)
	}
	if c != nil {
		if parts := renderForTelegramEntities(response); len(parts) > 0 {
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
