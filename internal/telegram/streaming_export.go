package telegram

import (
	"io"
	"log/slog"

	"github.com/aura/aura/internal/llm"
	tele "gopkg.in/telebot.v4"
)

// NewTestBot returns a minimal *Bot for use in fixture and integration tests.
// Only the logger is initialised; all production fields remain nil.
// consumeStream only reads b.logger, so a Bot constructed here is sufficient
// to drive the streaming path without a real Telegram connection or database.
func NewTestBot(logger *slog.Logger) *Bot {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Bot{logger: logger}
}

// ConsumeStream is an exported bridge to the unexported consumeStream method.
// It exists solely so that internal/channels/telegram/fixture can drive the
// streaming path from a separate package. Do not call it from production code.
func (b *Bot) ConsumeStream(c tele.Context, ch <-chan llm.Token, userID string, placeholder *tele.Message) (llm.Response, bool, error) {
	return b.consumeStream(c, ch, userID, placeholder)
}
