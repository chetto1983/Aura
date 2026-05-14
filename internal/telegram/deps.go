package telegram

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/aura/aura/internal/agent"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/api/auth"
	"github.com/aura/aura/internal/budget"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/mcp"
	"github.com/aura/aura/internal/sandbox"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/qdrant"
	"github.com/aura/aura/internal/storage/reindex"
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
// surface. Used to populate Bot.compactMemoryHealth via Deps without
// coupling cmd/aura directly to memoryindex internals.
type CompactMemoryHealthProvider interface {
	Snapshot() memoryindex.VectorHealth
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

	// ---- LLM / embedding ----------------------------------------------------
	LLM        llm.Client
	EmbedCache *search.EmbedCache // nil when EMBEDDING_API_KEY unset

	// ---- Wiki / search ------------------------------------------------------
	Wiki          wiki.Repository    // narrow interface stored on b.wiki
	WikiStore     *wiki.Store        // concrete type for SetReindexSubmitter + NewWikiPageTool
	Search        search.Searcher    // stored on b.search
	SearchRepo    search.Repository  // full repo (WikiSearch, ingest.Config.Search, reindex)
	ReindexWorker *reindex.Worker    // nil when search unavailable
	QdrantClient  qdrant.Client      // nil when QDRANT_URL unset

	// ---- Sources / OCR / ingest ---------------------------------------------
	Sources    source.Repository
	OCR        *ocr.Client
	Markitdown markitdown.Converter
	Ingest     *ingest.Pipeline

	// ---- Cron / scheduler ---------------------------------------------------
	SchedDB        *cron.Store               // full concrete store (satisfies AgentJobRepository + Repository)
	SummariesStore *summarizer.SummariesStore // proposals review queue
	TaskTool       *tools.TaskTool            // needs SetRunner(b) after Bot construction

	// ---- Memory index -------------------------------------------------------
	MemoryStore        *memoryindex.Store          // nil when compact memory unavailable
	CompactVector      memoryindex.VectorIndex     // nil when Qdrant + embed unavailable
	CompactVectorHealth *memoryindex.VectorHealthTracker // for Started/Failed/Succeeded in Phase C

	// ---- Agent / swarm ------------------------------------------------------
	AgentRunner *agent.Runner
	SwarmStore  *swarm.Store   // full concrete store (satisfies swarm.Reader + swarm.Repository)
	SwarmMgr    *swarm.Manager // nil when AURABOT_ENABLED=false

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

	// ---- Budget / background lifecycle --------------------------------------
	Budget   budget.Runtime
	BgCtx    context.Context
	BgCancel context.CancelFunc

	// ---- Compact memory health (interface) ----------------------------------
	// CompactVectorHealth (above) holds the concrete *VectorHealthTracker.
	// CompactMemoryHealth is the interface stored on b.compactMemoryHealth.
	CompactMemoryHealth CompactMemoryHealthProvider
}

// NewBot creates a Bot from the given Deps. Mirrors the field assignment
// that used to happen at the end of telegram.New(). Post-construction
// wiring (gate, scheduler, archive, api router, register handlers) is NOT
// performed here — callers handle it separately or use telegram.New().
func NewBot(deps Deps) *Bot {
	return &Bot{
		bot:                 deps.Bot,
		cfg:                 deps.Cfg,
		loc:                 deps.Loc,
		logger:              deps.Logger,
		llm:                 deps.LLM,
		wiki:                deps.Wiki,
		search:              deps.Search,
		tools:               deps.Tools,
		sources:             deps.Sources,
		ocr:                 deps.OCR,
		skills:              deps.Skills,
		bgCtx:               deps.BgCtx,
		bgCancel:            deps.BgCancel,
		schedDB:             deps.SchedDB,
		agentRunner:         deps.AgentRunner,
		swarmStore:          deps.SwarmStore,
		swarmMgr:            deps.SwarmMgr,
		authDB:              deps.AuthDB,
		mcpClients:          deps.MCPClients,
		sandboxMgr:          deps.SandboxMgr,
		toolReg:             deps.ToolReg,
		compactMemoryHealth: deps.CompactMemoryHealth,
		budget:              deps.Budget,
		reindex:             deps.ReindexWorker,
	}
}
