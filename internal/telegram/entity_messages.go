package telegram

import (
	"log/slog"

	tele "gopkg.in/telebot.v4"
)

// sendAssistant delivers LLM-generated text to the user using Telegram entities.
func (b *Bot) sendAssistant(c tele.Context, text string) {
	if err := sendTelegramEntityMessages(c.Bot(), c.Recipient(), text); err != nil {
		b.logger.Warn("entity send failed, falling back to plain text", "error", err)
		if err := c.Send(text); err != nil {
			b.logger.Warn("plain-text send failed", "error", err)
		}
	}
}

func (b *Bot) sendAssistantToRecipient(bot tele.API, recipient tele.Recipient, text string) error {
	if err := sendTelegramEntityMessages(bot, recipient, text); err != nil {
		b.logger.Warn("entity send failed, falling back to plain text", "error", err)
		_, plainErr := bot.Send(recipient, text)
		return plainErr
	}
	return nil
}

func (b *Bot) editAssistantMessage(bot tele.API, msg tele.Editable, part renderedTelegramMessage, rawText string) (*tele.Message, error) {
	edited, err := editTelegramEntityMessage(bot, msg, part)
	if err == nil {
		return edited, nil
	}
	b.logger.Warn("entity edit failed, falling back to plain text", "error", err)
	edited, err = bot.Edit(msg, rawText)
	if err != nil {
		return nil, err
	}
	return edited, nil
}

func (b *Bot) sendAssistantRemainder(bot tele.API, recipient tele.Recipient, parts []renderedTelegramMessage, start int) {
	for _, part := range parts[start:] {
		if _, err := bot.Send(recipient, part.Text, part.Entities); err != nil {
			b.logger.Warn("streaming remainder send failed", "error", err)
			return
		}
	}
}

// LogPlaceholderDeleteFailure is exported for channels/telegram.InvocationBuilder.
func LogPlaceholderDeleteFailure(logger *slog.Logger, userID string, placeholder *tele.Message, err error) {
	if err == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	args := []any{"user_id", userID, "error", err}
	if placeholder != nil {
		args = append(args, "message_id", placeholder.ID)
	}
	logger.Debug("telegram cleanup: placeholder delete failed", args...)
}

// SendAssistantText is the exported surface of sendAssistant.
func (b *Bot) SendAssistantText(c tele.Context, text string) { b.sendAssistant(c, text) }

// EditAssistantMsg is the exported surface of editAssistantMessage.
func (b *Bot) EditAssistantMsg(bot tele.API, msg tele.Editable, part RenderedMessage, rawText string) (*tele.Message, error) {
	return b.editAssistantMessage(bot, msg, part, rawText)
}

// SendAssistantMsgRemainder is the exported surface of sendAssistantRemainder.
func (b *Bot) SendAssistantMsgRemainder(bot tele.API, recipient tele.Recipient, parts []RenderedMessage, start int) {
	b.sendAssistantRemainder(bot, recipient, parts, start)
}

// toolActivityMessage returns the generic activity placeholder shown while a
// tool turn is in flight. The tool name is intentionally not included in the
// output (privacy: argument values must never appear in user-visible strings).
func toolActivityMessage(_ string) string {
	return "Sto lavorando alla richiesta..."
}
