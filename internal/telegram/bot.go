package telegram

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/agentruntime"
	"github.com/aura/aura/internal/api/auth"
	"github.com/aura/aura/internal/budget"
	"github.com/aura/aura/internal/concurrency"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/mcp"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/sources/ocr"
	"github.com/aura/aura/internal/storage/reindex"
	"github.com/aura/aura/internal/sandbox"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/storage/search"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/storage/sources/store"
	"github.com/aura/aura/internal/swarm"
	"github.com/aura/aura/internal/toolindex"
	"github.com/aura/aura/internal/tools"
	"github.com/aura/aura/internal/wiki"

	tele "gopkg.in/telebot.v4"
)

// Bot wraps the telebot instance with allowlist access control and LLM integration.
type Bot struct {
	bot                 *tele.Bot
	cfg                 *config.Config
	loc                 *time.Location
	logger              *slog.Logger
	llm                 llm.Client
	wiki                wiki.Repository
	search              search.Searcher
	tools               *tools.Registry
	budget              budget.Runtime
	sources             source.Repository
	ocr                 *ocr.Client
	skills              *auraskills.Loader
	docs                *docHandler
	sched               *scheduler.Scheduler
	schedDB             scheduler.AgentJobRepository
	agentRunner         *agent.Runner
	swarmStore          swarm.Reader
	swarmMgr            swarm.RunRunner
	authDB              auth.Repository                  // dashboard bearer-token and allowlist repository
	mcpClients          []mcp.ConnectedClient            // active MCP server connections (slice 11a)
	archiveDB           conversation.ArchiveRepository   // nil when CONV_ARCHIVE_ENABLED=false
	archiver            conversation.ClosingTurnAppender // nil when CONV_ARCHIVE_ENABLED=false
	issues              scheduler.IssueRepository        // wiki_issues queue, shared by API + maintenance
	api                 http.Handler                     // read-only JSON API for the dashboard, mounted on the health server
	sandboxMgr          sandbox.ExecutionRuntime         // nil when SANDBOX_ENABLED=false or runtime unavailable
	toolReg             tools.ToolStore                  // persistent LLM-written Python tools
	reindex             *reindex.Worker                  // optional; nil when search engine is unavailable (Phase 2 INDEX-01)
	toolReconciler      *toolindex.Reconciler            // Wave 2.10.b — keeps tool embedding index in sync with the registry
	bgCtx               context.Context                  // long-lived ctx for toolReconciler.Run + mcpwatch
	bgCancel            context.CancelFunc               // cancels bgCtx during Stop()
	bgWg                sync.WaitGroup                   // wait for background goroutines on Stop()
	compactMemoryHealth interface {
		Snapshot() memoryindex.VectorHealth
	}
	debugDocsMu sync.Mutex
	debugDocs   []DebugDocumentSend
	debugDocSeq atomic.Uint64
	sessions    *agentruntime.SessionStore
	started     atomic.Bool
	gate        *concurrency.UserGate
}

// Username returns the bot's Telegram username.
func (b *Bot) Username() string {
	return b.bot.Me.Username
}

// APIHandler returns the read-only JSON dashboard API. Caller is expected
// to wrap with http.StripPrefix and mount under /api/ on the health server.
func (b *Bot) APIHandler() http.Handler { return b.api }

func (b *Bot) CompactMemoryHealth() memoryindex.VectorHealth {
	if b == nil || b.compactMemoryHealth == nil {
		return memoryindex.VectorHealth{}
	}
	return b.compactMemoryHealth.Snapshot()
}

// ReindexHealth returns the current reindex worker's operational snapshot,
// or the zero value when the worker is not constructed (e.g. in tests).
// Surfaced via /api/health (Phase 2 D-16). WARNING 12 of 2026-05-10 plan
// revision wires this into the dashboard health JSON.
func (b *Bot) ReindexHealth() reindex.Health {
	if b == nil || b.reindex == nil {
		return reindex.Health{}
	}
	return b.reindex.Health()
}

func (b *Bot) sessionStore() *agentruntime.SessionStore {
	if b == nil {
		return nil
	}
	if b.sessions == nil {
		b.sessions = agentruntime.NewSessionStore()
	}
	return b.sessions
}

func (b *Bot) userGate() *concurrency.UserGate {
	if b == nil {
		return nil
	}
	return b.gate
}

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

// sendGeneratedToUser delivers assistant-generated text with the same
// Markdown-to-Telegram-HTML rendering used by interactive chat replies.
// Keep SendToUser raw for tokens and operator strings where escaping could
// alter the payload.
func (b *Bot) sendGeneratedToUser(userID, message string) error {
	chatID, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return fmt.Errorf("send generated to user %q: %w", userID, err)
	}
	if err := b.sendAssistantToRecipient(b.bot, tele.ChatID(chatID), message); err != nil {
		return fmt.Errorf("send generated to user %s: %w", userID, err)
	}
	return nil
}

// SendDocumentToUser delivers a generated file to userID's direct
// Telegram chat. Used by the create_xlsx tool (slice 15a) to ship
// LLM-authored spreadsheets back to the user. Satisfies
// tools.DocumentSender.
//
// Telegram caps non-premium bot documents at 50 MB; the generator
// already enforces a tighter cap, but we surface the upstream error
// when present so the user knows why delivery failed.
func (b *Bot) SendDocumentToUser(userID, filename string, body []byte, caption string) error {
	chatID, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return fmt.Errorf("send document to user %q: %w", userID, err)
	}
	doc := &tele.Document{
		File:     tele.FromReader(bytes.NewReader(body)),
		FileName: filename,
		Caption:  caption,
	}
	if _, err := b.bot.Send(tele.ChatID(chatID), doc); err != nil {
		return fmt.Errorf("send document to user %s: %w", userID, err)
	}
	b.recordDebugDocumentSend(filename, body, caption)
	return nil
}

func (b *Bot) recordDebugDocumentSend(filename string, body []byte, caption string) {
	if b == nil {
		return
	}
	send := DebugDocumentSend{
		Seq:       b.debugDocSeq.Add(1),
		Filename:  filename,
		Caption:   caption,
		SizeBytes: len(body),
	}
	b.debugDocsMu.Lock()
	defer b.debugDocsMu.Unlock()
	b.debugDocs = append(b.debugDocs, send)
	if len(b.debugDocs) > 100 {
		b.debugDocs = append([]DebugDocumentSend(nil), b.debugDocs[len(b.debugDocs)-100:]...)
	}
}

func (b *Bot) debugDocumentSendsAfter(seq uint64) []DebugDocumentSend {
	if b == nil {
		return nil
	}
	b.debugDocsMu.Lock()
	defer b.debugDocsMu.Unlock()
	out := make([]DebugDocumentSend, 0)
	for _, send := range b.debugDocs {
		if send.Seq > seq {
			out = append(out, send)
		}
	}
	return out
}

// Start begins polling for Telegram messages and the scheduler tick loop.
func (b *Bot) Start() {
	b.logger.Info("telegram bot started")
	if b.sched != nil {
		b.sched.Start(context.Background())
	}
	b.started.Store(true)
	defer b.started.Store(false)
	b.bot.Start()
}

// Stop gracefully stops update intake and waits for background workers.
func (b *Bot) Stop() {
	// Close the UserGate first (T-01-21): stops InactivityTracker and cancels all
	// actor goroutines before the Telegram bot and scheduler shut down.
	if gate := b.userGate(); gate != nil {
		gate.Close()
	}
	if b.bot != nil && b.started.Load() {
		b.bot.Stop()
	}
	if b.sched != nil {
		b.sched.Stop()
	}
	if b.docs != nil {
		b.docs.Stop()
	}
	logger := b.logger
	if logger == nil {
		logger = slog.Default()
	}
	if b.archiver != nil {
		if err := b.archiver.Close(context.Background()); err != nil {
			logger.Error("telegram shutdown: archiver close failed", "error", err)
		}
	}
	// PHASE 2 ADD: stop reindex worker after archiver so final enqueues drain first.
	if b.reindex != nil {
		b.reindex.Stop() // cancels in-flight Reindex; waits for drain goroutine exit
	}
	// Wave 2.10.b — cancel background goroutines (toolReconciler + mcpwatch)
	// and wait for them to exit before returning. Safe even when bgCancel is
	// nil (e.g. constructed-but-never-started in tests).
	if b.bgCancel != nil {
		b.bgCancel()
	}
	b.bgWg.Wait()
	for i, c := range b.mcpClients {
		if c == nil {
			continue
		}
		if err := c.Close(); err != nil {
			logger.Error("telegram shutdown: mcp client close failed", "index", i, "error", err)
		}
	}
}
