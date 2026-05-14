package telegram

import (
	"fmt"
	"strconv"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/concurrency"
	tools "github.com/aura/aura/internal/agent/tools/registry"

	tele "gopkg.in/telebot.v4"
)

// New creates a Telegram Bot from pre-built Phase A deps (built by cmd/aura/newApp).
//
// Phase B: creates the tele.Bot and wires Telegram-specific fields.
// Phase C (Telegram-specific only): UserGate with Telegram-delivery callbacks,
// sandbox tools, ToolSearch, bot-sender tools (request_dashboard_token, doc, wiki),
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

	// Sandbox tools (need b as DocumentSender / ScheduledTaskRunner).
	if tool := tools.NewExecuteCodeToolWithStoreAndRegistry(deps.SandboxMgr, b, deps.Sources, deps.Tools); tool != nil {
		deps.Tools.Register(tool)
	}
	if tool := tools.NewExecuteShellTool(deps.SandboxMgr); tool != nil {
		deps.Tools.Register(tool)
	}
	if tool := tools.NewDevToolTool(deps.ToolReg); tool != nil {
		deps.Tools.Register(tools.WithCategory(tool, tools.CategoryAutonomous))
	}

	// Deferred-tools rollout: tool_search is the always-on seed of the
	// per-turn agent pool. Registered last so it sees every other tool in
	// Registry.Search(). PrepareVectorReader is called later in wireBot.
	if tool := tools.NewToolSearchTool(deps.Tools); tool != nil {
		deps.Tools.Register(tool)
	}

	// Tools that need b as a sender/runner. Registered after b is
	// constructed and before the docs handler so they're available on first
	// message.
	if tokenTool := tools.NewRequestDashboardTokenTool(deps.AuthDB, b, b.isAllowlisted); tokenTool != nil {
		deps.Tools.Register(tokenTool)
	}
	if docTool := tools.NewDocTool(deps.Sources, b); docTool != nil {
		deps.Tools.Register(docTool)
	}
	if t := tools.NewWikiPageTool(deps.WikiStore, deps.ReindexWorker); t != nil {
		deps.Tools.Register(t)
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
