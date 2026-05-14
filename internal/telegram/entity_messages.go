package telegram

import (
	"log/slog"
	"strings"
	"time"

	tele "gopkg.in/telebot.v4"
)

// Streaming constants mirror channels/telegram/outbound.go so both paths
// produce identical flush decisions.
const (
	streamingMinThreshold          = 30
	streamingReasoningMinThreshold = 8
	streamingEditThrottle          = 600 * time.Millisecond
)

// composeStreamingMessage builds the live-edited Telegram message body.
func composeStreamingMessage(cot, content string) string {
	cot = strings.TrimSpace(cot)
	content = strings.TrimSpace(content)
	switch {
	case cot != "" && content != "":
		return "🧠 _" + cot + "_\n\n" + content
	case cot != "":
		return "🧠 _" + cot + "_"
	default:
		return content
	}
}

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

func logPlaceholderDeleteFailure(logger *slog.Logger, userID string, placeholder *tele.Message, err error) {
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
