package telegram

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/agent/agentdef"
	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/agentnote"
	"github.com/aura/aura/internal/audio"
	"github.com/aura/aura/internal/budget"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/concurrency"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/llm/pockettts"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/storage/memoryindex"

	tele "gopkg.in/telebot.v4"
)

// DebugDocumentSend records metadata for documents delivered by the bot during
// a debug smoke. It never stores file bodies.
type DebugDocumentSend struct {
	Seq       uint64
	Filename  string
	Caption   string
	SizeBytes int
}

// debugDocState aggregates the debug-document tracking fields.
// Treated as a single "debugDocs* aggregate" field on Bot (US-A13c).
type debugDocState struct {
	mu   sync.Mutex
	docs []DebugDocumentSend
	seq  atomic.Uint64
}

// Bot wraps the telebot instance with allowlist access control and LLM integration.
//
// US-A13c: Bot struct reduced to Telegram-wrapper essentials + one bundled
// *botRuntime for non-Telegram operational deps. Goroutine lifecycle
// (bgCtx/bgCancel/bgWg) and resource cleanup (reindex, archiver Close,
// mcpClients) are owned by *App in cmd/aura/ — not by Bot.
type Bot struct {
	// ---- Telegram-wrapper essentials ----
	bot      *tele.Bot
	cfg      *config.Config
	loc      *time.Location
	logger   *slog.Logger
	gate     *concurrency.UserGate
	sessions *agent.SessionStore
	hub      *chat.Hub
	started  atomic.Bool
	docs     *docHandler
	voice    *voiceHandler
	dbg      debugDocState // debugDocs* aggregate

	// ---- Bundled non-Telegram operational deps ----
	// rt holds all non-Telegram deps (llm, wiki, tools, auth, etc.).
	// Goroutine lifecycle and cleanup resources live on *App.
	rt *botRuntime
}

// Username returns the bot's Telegram username.
func (b *Bot) Username() string {
	return b.bot.Me.Username
}

// CompactMemoryHealth returns the compact memory mirror's current health snapshot.
func (b *Bot) CompactMemoryHealth() memoryindex.VectorHealth {
	if b == nil || b.rt == nil || b.rt.compactMemoryHealth == nil {
		return memoryindex.VectorHealth{}
	}
	return b.rt.compactMemoryHealth.Snapshot()
}

func (b *Bot) sessionStore() *agent.SessionStore {
	if b == nil {
		return nil
	}
	if b.sessions == nil {
		b.sessions = agent.NewSessionStore()
	}
	return b.sessions
}

func (b *Bot) userGate() *concurrency.UserGate {
	if b == nil {
		return nil
	}
	return b.gate
}

// SetHub wires a chat.Hub into the bot after construction.
func (b *Bot) SetHub(hub *chat.Hub) {
	b.hub = hub
}

// AgentNoteStore returns the per-conversation note store (nil when not configured).
func (b *Bot) AgentNoteStore() *agentnote.Store {
	if b == nil || b.rt == nil {
		return nil
	}
	return b.rt.agentNoteStore
}

// VoicePolicy returns the per-chat TTS mode store (nil when not configured).
func (b *Bot) VoicePolicy() audio.Store {
	if b == nil || b.rt == nil {
		return nil
	}
	return b.rt.voicePolicy
}

// TTSClient returns the pocket-tts synthesis client (nil when POCKETTTS_BASE_URL unset).
func (b *Bot) TTSClient() *pockettts.Client {
	if b == nil || b.rt == nil {
		return nil
	}
	return b.rt.pocketttsClient
}

// Pool returns the shared SQLite pool (nil when not configured).
func (b *Bot) Pool() *sql.DB {
	if b == nil || b.rt == nil || b.rt.schedDB == nil {
		return nil
	}
	return b.rt.schedDB.DB()
}

// Config returns the bot's configuration.
func (b *Bot) Config() *config.Config { return b.cfg }

// Logger returns the bot's structured logger.
func (b *Bot) Logger() *slog.Logger { return b.logger }

// TimeLocation returns the display timezone (may be nil).
func (b *Bot) TimeLocation() *time.Location { return b.loc }

// SessionStore returns the per-user conversation session store.
func (b *Bot) SessionStore() *agent.SessionStore { return b.sessionStore() }

// LLMClient returns the LLM client (nil when not configured).
func (b *Bot) LLMClient() llm.Client {
	if b.rt == nil {
		return nil
	}
	return b.rt.llm
}

// BudgetRuntime returns the budget runtime (nil when not configured).
func (b *Bot) BudgetRuntime() budget.Runtime {
	if b.rt == nil {
		return nil
	}
	return b.rt.budget
}

// SkillsLoader returns the skills loader (nil when not configured).
func (b *Bot) SkillsLoader() *auraskills.Loader {
	if b.rt == nil {
		return nil
	}
	return b.rt.skills
}

// ToolRegistry returns the tool registry (nil when not configured).
func (b *Bot) ToolRegistry() *tools.Registry {
	if b.rt == nil {
		return nil
	}
	return b.rt.tools
}

// AgentDefinitions returns the immutable AGENTDEF registry loaded at boot.
func (b *Bot) AgentDefinitions() *agentdef.Registry {
	if b == nil || b.rt == nil {
		return nil
	}
	return b.rt.agentDefs
}

// WikiTOC returns the cached wiki TOC string for injection into the system prompt.
// Returns "" when the wiki store is unavailable or the cache has not yet been built.
func (b *Bot) WikiTOC() string {
	if b.rt == nil || b.rt.wikiTOCFn == nil {
		return ""
	}
	return b.rt.wikiTOCFn()
}

// ToolAttemptsRepo returns the tool observation repository used by chat turns.
func (b *Bot) ToolAttemptsRepo() attempts.Repo {
	if b == nil || b.rt == nil {
		return nil
	}
	return b.rt.attemptsRepo
}

// ArchiveRepository returns the conversation archive repository (nil when not configured).
func (b *Bot) ArchiveRepository() conversation.ArchiveRepository {
	if b.rt == nil {
		return nil
	}
	return b.rt.archiveDB
}

// SendToUser delivers a Telegram message to userID's direct chat.
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
// Telegram chat. Satisfies tools.DocumentSender.
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
		Seq:       b.dbg.seq.Add(1),
		Filename:  filename,
		Caption:   caption,
		SizeBytes: len(body),
	}
	b.dbg.mu.Lock()
	defer b.dbg.mu.Unlock()
	b.dbg.docs = append(b.dbg.docs, send)
	if len(b.dbg.docs) > 100 {
		b.dbg.docs = append([]DebugDocumentSend(nil), b.dbg.docs[len(b.dbg.docs)-100:]...)
	}
}

func (b *Bot) debugDocumentSendsAfter(seq uint64) []DebugDocumentSend {
	if b == nil {
		return nil
	}
	b.dbg.mu.Lock()
	defer b.dbg.mu.Unlock()
	out := make([]DebugDocumentSend, 0)
	for _, send := range b.dbg.docs {
		if send.Seq > seq {
			out = append(out, send)
		}
	}
	return out
}

// Start begins polling for Telegram messages.
// The cron scheduler is started by App.Start() before calling this.
func (b *Bot) Start() {
	b.logger.Info("telegram bot started")
	b.started.Store(true)
	defer b.started.Store(false)
	b.bot.Start()
}

// Stop gracefully shuts down the Telegram bot and UserGate.
//
// US-A13c: Bot.Stop owns ONLY Telegram-specific shutdown:
//   - UserGate.Close (drains in-flight actor goroutines)
//   - tele.Bot.Stop (stops Telegram polling)
//   - docHandler.Stop
//
// Background goroutine lifecycle (bgCancel/bgWg.Wait), resource cleanup
// (archiver.Close, reindex.Stop, mcpClients.Close), and scheduler shutdown
// are owned by App.Stop in cmd/aura/.
func (b *Bot) Stop() {
	if gate := b.userGate(); gate != nil {
		gate.Close()
	}
	if b.bot != nil && b.started.Load() {
		b.bot.Stop()
	}
	if b.docs != nil {
		b.docs.Stop()
	}
	if b.voice != nil {
		b.voice.Stop()
	}
}

// IsAllowlisted reports whether userID is permitted to use the bot.
func (b *Bot) IsAllowlisted(userID string) bool { return b.isAllowlisted(userID) }

// SendReminder implements cron.Notifier: delivers a plain-text reminder via Telegram.
func (b *Bot) SendReminder(userID, body string) error {
	return b.SendToUser(userID, body)
}

// SendCompletion implements cron.Notifier: delivers a Markdown agent-job completion report.
func (b *Bot) SendCompletion(userID, body string) error {
	return b.sendGeneratedToUser(userID, body)
}

// NotifyOwners implements cron.Notifier: broadcasts a maintenance message to all owners.
func (b *Bot) NotifyOwners(_ context.Context, msg string) {
	for _, ownerID := range b.collectOwnerIDs() {
		if err := b.SendToUser(ownerID, msg); err != nil {
			b.logger.Warn("owner notification failed", "owner", ownerID, "error", err)
		}
	}
}

// RuntimeSettingsApplier returns a closure for api.Deps.ApplyRuntimeSettings.
func (b *Bot) RuntimeSettingsApplier(deps Deps) func(context.Context) error {
	return func(ctx context.Context) error {
		var bgd budget.Configurator
		if b.rt != nil {
			bgd = b.rt.budget
		}
		return config.ApplyRuntimeSettings(ctx, deps.SettingsStore, deps.Cfg, deps.SwarmMgr, bgd, deps.Logger)
	}
}

// SetArchiveDB assigns the conversation archive repository (wired by wireBot).
// Stored in botRuntime so buildTelegramInvocation can look up MaxTurnIndex.
func (b *Bot) SetArchiveDB(a conversation.ArchiveRepository) {
	if b.rt != nil {
		b.rt.archiveDB = a
	}
}

// SetArchiver assigns the buffered conversation turn appender (wired by wireBot).
// Stored in botRuntime so buildTelegramInvocation can call Append during turns.
// App.Stop() also holds a reference to call Close on shutdown.
func (b *Bot) SetArchiver(a conversation.ClosingTurnAppender) {
	if b.rt != nil {
		b.rt.archiver = a
	}
}

// SetIssues assigns the wiki issues store (wired by wireBot).
func (b *Bot) SetIssues(is cron.IssueRepository) {
	if b.rt != nil {
		b.rt.issues = is
	}
}

// SetMemoryStore wires the compact memory store (wired by wireBot).
// Used by the Telegram InvocationBuilder to inject top-N operational lessons
// into the system prompt overlay at conversation start (US-OP03).
func (b *Bot) SetMemoryStore(s *memoryindex.Store) {
	if b.rt != nil {
		b.rt.memoryStore = s
	}
}

// MemoryStore returns the compact memory store (nil when not configured).
func (b *Bot) MemoryStore() *memoryindex.Store {
	if b == nil || b.rt == nil {
		return nil
	}
	return b.rt.memoryStore
}

// ArchiveAppender returns the turn appender for use by channels/telegram.InvocationBuilder.
func (b *Bot) ArchiveAppender() conversation.TurnAppender {
	return b.archiveAppenderForTurn()
}

func (b *Bot) archiveAppenderForTurn() conversation.TurnAppender {
	if b == nil || b.rt == nil {
		return nil
	}
	return b.rt.archiver
}

// FinalizeModel returns the configured LLM model name (implements agent.TerminalFinalizer).
func (b *Bot) FinalizeModel() string { return b.cfg.LLMModel }

// FinalizeChat sends a no-tool LLM request, applying the configured reasoning effort
// (implements agent.TerminalFinalizer).
func (b *Bot) FinalizeChat(ctx context.Context, req llm.Request) (llm.Response, error) {
	req.ReasoningEffort = b.cfg.ReasoningEffort
	return b.rt.llm.Send(ctx, req)
}

// FinalizeRecordUsage records token usage against the per-user budget
// (implements agent.TerminalFinalizer).
func (b *Bot) FinalizeRecordUsage(usage llm.TokenUsage) {
	if b.rt != nil && b.rt.budget != nil {
		b.rt.budget.RecordUsage(usage)
	}
}

// FinalizeEstimateCost returns the estimated cost in USD for the given token usage
// (implements agent.TerminalFinalizer).
func (b *Bot) FinalizeEstimateCost(usage llm.TokenUsage) float64 {
	return agent.EstimateUsageCost(usage, b.cfg.CostInputPerMTokens, b.cfg.CostOutputPerMTokens)
}
