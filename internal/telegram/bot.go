package telegram

import (
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"github.com/aura/aura/internal/config"

	tele "gopkg.in/telebot.v4"
)

// Bot wraps the telebot instance with allowlist access control.
type Bot struct {
	bot      *tele.Bot
	cfg      *config.Config
	logger   *slog.Logger
	active   sync.Map // maps userID string -> bool (active conversation tracking)
}

// New creates a new Telegram bot with allowlist enforcement.
func New(cfg *config.Config, logger *slog.Logger) (*Bot, error) {
	pref := tele.Settings{
		Token: cfg.TelegramToken,
	}

	tb, err := tele.NewBot(pref)
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}

	b := &Bot{
		bot:    tb,
		cfg:    cfg,
		logger: logger,
	}

	b.registerHandlers()
	return b, nil
}

// Start begins polling for Telegram messages.
func (b *Bot) Start() {
	b.logger.Info("telegram bot started")
	b.bot.Start()
}

// Stop gracefully stops the bot.
func (b *Bot) Stop() {
	b.bot.Stop()
}

func (b *Bot) registerHandlers() {
	b.bot.Handle(tele.OnText, b.onMessage)
}

func (b *Bot) onMessage(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)

	if !b.cfg.IsAllowlisted(userID) {
		b.logger.Warn("message from non-allowlisted user",
			"user_id", userID,
			"username", c.Sender().Username,
		)
		return nil
	}

	// Launch conversation in its own goroutine
	go b.handleConversation(c)

	return nil
}

func (b *Bot) handleConversation(c tele.Context) {
	userID := strconv.FormatInt(c.Sender().ID, 10)

	// Track active conversation
	b.active.Store(userID, true)
	defer b.active.Delete(userID)

	b.logger.Info("conversation started",
		"user_id", userID,
		"username", c.Sender().Username,
		"message", c.Text(),
	)

	// Send confirmation that conversation has started
	if err := c.Send("Aura here. Conversation started."); err != nil {
		b.logger.Error("failed to send confirmation",
			"user_id", userID,
			"error", err,
		)
	}
}