package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"

	"github.com/aura/aura/internal/auth"
	"github.com/aura/aura/internal/budget"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/mcp"
	"github.com/aura/aura/internal/ocr"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/search"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/tools"
	"github.com/aura/aura/internal/wiki"

	tele "gopkg.in/telebot.v4"
)

// Bot wraps the telebot instance with allowlist access control and LLM integration.
type Bot struct {
	bot        *tele.Bot
	cfg        *config.Config
	logger     *slog.Logger
	llm        llm.Client
	wiki       *wiki.Store
	search     *search.Engine
	tools      *tools.Registry
	budget     *budget.Tracker
	sources    *source.Store
	ocr        *ocr.Client
	skills     *auraskills.Loader
	docs       *docHandler
	sched      *scheduler.Scheduler
	schedDB    *scheduler.Store
	authDB     *auth.Store                    // dashboard bearer-token store (slice 10d)
	mcpClients []*mcp.Client                  // active MCP server connections (slice 11a)
	archiveDB  *conversation.ArchiveStore     // nil when CONV_ARCHIVE_ENABLED=false
	archiver   *conversation.BufferedAppender // nil when CONV_ARCHIVE_ENABLED=false
	summRunner *summarizer.Runner             // nil when SUMMARIZER_ENABLED=false
	issues     *scheduler.IssuesStore         // wiki_issues queue, shared by API + maintenance
	api        http.Handler                   // read-only JSON API for the dashboard, mounted on the health server
	active     sync.Map                       // maps userID string -> bool (active conversation tracking)
	ctxMap     sync.Map                       // maps userID string -> *conversation.Context
}

// Username returns the bot's Telegram username.
func (b *Bot) Username() string {
	return b.bot.Me.Username
}

// APIHandler returns the read-only JSON dashboard API. Caller is expected
// to wrap with http.StripPrefix and mount under /api/ on the health server.
func (b *Bot) APIHandler() http.Handler { return b.api }

// SendToUser delivers a Telegram message to userID's direct chat. Used
// by the request_dashboard_token tool to ship the bearer token out of
// band so it never lands in LLM conversation history. Satisfies
// tools.TokenSender.
func (b *Bot) SendToUser(userID, message string) error {
	chatID, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return fmt.Errorf("send to user %q: %w", userID, err)
	}
	if _, err := b.bot.Send(tele.ChatID(chatID), message); err != nil {
		return fmt.Errorf("send to user %s: %w", userID, err)
	}
	return nil
}

// Start begins polling for Telegram messages and the scheduler tick loop.
func (b *Bot) Start() {
	b.logger.Info("telegram bot started")
	if b.sched != nil {
		b.sched.Start(context.Background())
	}
	b.bot.Start()
}

// Stop gracefully stops the bot and the scheduler.
func (b *Bot) Stop() {
	if b.archiver != nil {
		_ = b.archiver.Close(context.Background())
	}
	if b.sched != nil {
		b.sched.Stop()
	}
	if b.schedDB != nil {
		_ = b.schedDB.Close()
	}
	if b.authDB != nil {
		_ = b.authDB.Close()
	}
	for _, c := range b.mcpClients {
		_ = c.Close()
	}
	b.bot.Stop()
}
