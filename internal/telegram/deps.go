package telegram

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/agentnote"
	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/api/auth"
	"github.com/aura/aura/internal/budget"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/mcp"
	"github.com/aura/aura/internal/sandbox"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/qdrant"
	"github.com/aura/aura/internal/storage/reindex"
	runstore "github.com/aura/aura/internal/storage/runs"
	"github.com/aura/aura/internal/storage/search"
	"github.com/aura/aura/internal/storage/sources/ingest"
	"github.com/aura/aura/internal/storage/sources/markitdown"
	"github.com/aura/aura/internal/storage/sources/ocr"
	source "github.com/aura/aura/internal/storage/sources/store"
	"github.com/aura/aura/internal/swarm"
	"github.com/aura/aura/internal/wiki"

	tele "gopkg.in/telebot.v4"
)

// CompactMemoryHealthProvider exposes the compact-memory mirror snapshot
// surface. Used to populate botRuntime.compactMemoryHealth via Deps without
// coupling cmd/aura directly to memoryindex internals.
type CompactMemoryHealthProvider interface {
	Snapshot() memoryindex.VectorHealth
}

// botRuntime bundles all non-Telegram operational dependencies onto Bot as a
// single field (Bot.rt). Keeping it separate from the Bot struct reduces Bot
// to ~11 Telegram-wrapper essentials and makes goroutine lifecycle ownership
// (bgCtx/bgCancel/bgWg, reindex, archiver close) clearly App's responsibility.
//
// Fields here are wired by cmd/aura/App.wireBot after telegram.New() returns.
// Only fields actually read by Bot methods live here; deps consumed exclusively
// by App (reindex, mcpClients, swarmStore, etc.) stay on App/Deps.
type botRuntime struct {
	llm                 llm.Client
	wiki                wiki.Repository
	tools               *tools.Registry
	sources             source.Repository
	skills              *auraskills.Loader
	schedDB             *cron.Store
	authDB              auth.Repository
	compactMemoryHealth CompactMemoryHealthProvider
	budget              budget.Runtime
	archiver            conversation.ClosingTurnAppender
	archiveDB           conversation.ArchiveRepository
	issues              cron.IssueRepository
	agentNoteStore      *agentnote.Store
	attemptsRepo        attempts.Repo
	memoryStore         *memoryindex.Store
}

// Deps holds the pre-built dependencies a Bot needs at construction time.
// cmd/aura/app.go (composition root) builds Phase A deps and populates this
// struct; telegram.New() receives it and performs only Telegram-specific
// Phase B/C wiring.
//
// US-A13b.2: expanded from scaffold (US-A13b.1) to carry every Phase A dep
// so telegram/setup.go can shed ~400 LOC of pure construction logic.
type Deps struct {
	// ---- Telegram bot -------------------------------------------------------
	// Bot is nil from App; telegram.New() sets it via tele.NewBot before
	// calling NewBot(deps).
	Bot    *tele.Bot
	Cfg    *config.Config
	Loc    *time.Location
	Logger *slog.Logger

	// ---- Core infrastructure ------------------------------------------------
	Pool          *sql.DB           // shared SQLite pool (toolindex state, archive, issues)
	SettingsStore config.Repository // runtime settings overlay
	RunStore      *runstore.Store   // canonical run/event/audit store

	// ---- LLM / embedding ----------------------------------------------------
	LLM        llm.Client
	EmbedCache *search.EmbedCache // nil when EMBEDDING_API_KEY unset

	// ---- Wiki / search ------------------------------------------------------
	Wiki          wiki.Repository   // narrow interface stored on rt.wiki
	WikiStore     *wiki.Store       // concrete type for SetReindexSubmitter + NewWikiPageTool
	Search        search.Searcher   // stored on rt (unused by Bot methods; kept for App wiring)
	SearchRepo    search.Repository // full repo (WikiSearch, ingest.Config.Search, reindex)
	ReindexWorker *reindex.Worker   // nil when search unavailable; lifecycle owned by App
	QdrantClient  qdrant.Client     // nil when QDRANT_URL unset

	// ---- Sources / OCR / ingest ---------------------------------------------
	Sources    source.Repository
	OCR        *ocr.Client
	Markitdown markitdown.Converter
	Ingest     *ingest.Pipeline

	// ---- Cron / scheduler ---------------------------------------------------
	SchedDB        *cron.Store                // full concrete store (satisfies AgentJobRepository + Repository)
	SummariesStore *summarizer.SummariesStore // proposals review queue
	TaskTool       *tools.TaskTool            // needs SetRunner(b) after Bot construction

	// ---- Memory index -------------------------------------------------------
	MemoryStore         *memoryindex.Store               // nil when compact memory unavailable
	CompactVector       memoryindex.VectorIndex          // nil when Qdrant + embed unavailable
	CompactVectorHealth *memoryindex.VectorHealthTracker // for Started/Failed/Succeeded in Phase C

	// ---- Agent / swarm ------------------------------------------------------
	SwarmStore *swarm.Store   // full concrete store (satisfies swarm.Reader + swarm.Repository)
	SwarmMgr   *swarm.Manager // nil when AURABOT_ENABLED=false

	// ---- Auth / MCP / sandbox -----------------------------------------------
	AuthDB        auth.Repository
	MCPClients    []mcp.ConnectedClient
	SandboxMgr    *sandbox.Manager
	SandboxHealth api.SandboxHealth

	// ---- Skills -------------------------------------------------------------
	Skills        *auraskills.Loader
	SkillsCatalog *auraskills.CatalogClient

	// ---- Tool registry ------------------------------------------------------
	Tools   *tools.Registry
	ToolReg tools.ToolStore

	// ---- Budget -------------------------------------------------------------
	Budget budget.Runtime

	// ---- Agent note ---------------------------------------------------------
	AgentNoteStore *agentnote.Store

	// ---- Tool experience loop ----------------------------------------------
	AttemptsRepo attempts.Repo

	// ---- Lifecycle ----------------------------------------------------------
	// ParentCtx is the App's background context. Long-lived per-Bot workers
	// (e.g. docHandler OCR pipeline) derive their internal context from it so
	// App shutdown propagates without a separate Stop() round-trip. Nil falls
	// back to context.Background() and the worker's own Stop() is the only
	// cancel path.
	ParentCtx context.Context
}

// NewBot creates a Bot from the given Deps. All non-Telegram operational deps
// are bundled into a *botRuntime (Bot.rt). Goroutine lifecycle (bgCtx/bgCancel/bgWg)
// and resource cleanup (reindex, archiver, mcpClients) are owned by *App — not by Bot.
func NewBot(deps Deps) *Bot {
	rt := &botRuntime{
		llm:                 deps.LLM,
		wiki:                deps.Wiki,
		tools:               deps.Tools,
		sources:             deps.Sources,
		skills:              deps.Skills,
		schedDB:             deps.SchedDB,
		authDB:              deps.AuthDB,
		compactMemoryHealth: deps.CompactVectorHealth,
		budget:              deps.Budget,
		agentNoteStore:      deps.AgentNoteStore,
		attemptsRepo:        deps.AttemptsRepo,
	}
	return &Bot{
		bot:    deps.Bot,
		cfg:    deps.Cfg,
		loc:    deps.Loc,
		logger: deps.Logger,
		rt:     rt,
	}
}
