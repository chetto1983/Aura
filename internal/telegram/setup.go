package telegram

import (
	"fmt"
	"strconv"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/concurrency"

	tele "gopkg.in/telebot.v4"
)

// New creates a Telegram Bot from pre-built Phase A deps (built by cmd/aura/newApp).
//
// Phase B: creates the tele.Bot and wires Telegram-specific fields.
// Phase C (Telegram-specific only): UserGate with Telegram-delivery callbacks,
// docs handler, registerHandlers, installBotCommands.
//
// Phase C composition wiring (api.Router, cron.Scheduler, conversation archive,
// memoryStore rebuild, toolReconciler + mcpwatch) is performed by
// cmd/aura/App.wireBot after New() returns.
//
// deps.SettingsStore may be nil (tests) — in that case the dashboard
// /settings endpoints respond 503.
func New(deps Deps) (*Bot, error) {
	if deps.Pool == nil {
		return nil, fmt.Errorf("telegram: db pool required")
	}

	cfg := deps.Cfg
	logger := deps.Logger

	// ---- Phase B: Telegram-specific setup -----------------------------------
	pref := tele.Settings{
		Token: cfg.TelegramToken,
	}
	tb, err := tele.NewBot(pref)
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}
	deps.Bot = tb

	b := NewBot(deps)

	// ---- Phase C (Telegram-specific): UserGate with Telegram callbacks ------
	//
	// OnEvict: persists conversation snapshot and clears session (D-10, T-01-22).
	// OnOverflow: sends Telegram notice to user in a separate goroutine (D-03, T-01-20, Pitfall 4).
	// OnQueueNotice: sends still-processing notice when entry waits > InboxQueueNoticeAfter.
	userGate := concurrency.New(concurrency.Config{
		InboxSize:         cfg.InboxSize,
		EvictionThreshold: cfg.InactivityThreshold,
		SweepInterval:     cfg.InactivitySweepInterval,
		QueueNoticeAfter:  cfg.InboxQueueNoticeAfter,
		OnEvict: func(userID string) {
			b.logger.Info("user evicted from gate; clearing session", "user_id", userID)
			b.sessionStore().Clear(userID)
		},
		OnOverflow: func(userID string) {
			go func() {
				chatID, err := strconv.ParseInt(userID, 10, 64)
				if err != nil {
					b.logger.Warn("overflow notice: invalid userID", "user_id", userID, "error", err)
					return
				}
				msg := "I'm still processing your previous message. Your new message was dropped. Please try again in a moment."
				if _, err := b.bot.Send(tele.ChatID(chatID), msg); err != nil {
					b.logger.Warn("overflow notice delivery failed", "user_id", userID, "error", err)
				}
			}()
		},
		OnQueueNotice: func(userID string) {
			// Pitfall 4: hand off to a separate goroutine -- the gate's timer
			// goroutine must never block on the Telegram API call (T-01-28).
			go func() {
				chatID, err := strconv.ParseInt(userID, 10, 64)
				if err != nil {
					b.logger.Warn("queue notice: invalid userID", "user_id", userID, "error", err)
					return
				}
				msg := "Still working on your previous message -- I'll get to this one shortly."
				if _, err := b.bot.Send(tele.ChatID(chatID), msg); err != nil {
					b.logger.Warn("queue notice delivery failed", "user_id", userID, "error", err)
				}
			}()
		},
	})
	// Wire gate and gate-aware session store into the bot (D-16).
	b.gate = userGate
	b.sessions = agent.NewSessionStore(userGate)

	// Wave 2.7b: wire the run_now action of the unified task tool now that
	// *Bot (which implements ScheduledTaskRunner) is available.
	if deps.TaskTool != nil {
		deps.TaskTool.SetRunner(b)
	}

	// Slice 6: doc handler (Telegram document upload → OCR/markitdown pipeline).
	b.docs = newDocHandler(docHandlerConfig{
		Bot:        tb,
		Sources:    deps.Sources,
		OCR:        deps.OCR,
		Markitdown: deps.Markitdown,
		MaxFileMB:  cfg.OCRMaxFileMB,
		Allowlist:  b.isAllowlisted,
		Logger:     logger,
		AfterOCR:   deps.Ingest.AfterOCR,
	})

	b.registerHandlers()
	b.installBotCommands()
	return b, nil
}
