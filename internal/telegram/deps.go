package telegram

import (
	"context"
	"log/slog"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/api/auth"
	"github.com/aura/aura/internal/budget"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/mcp"
	"github.com/aura/aura/internal/sandbox"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/search"
	"github.com/aura/aura/internal/storage/sources/ocr"
	"github.com/aura/aura/internal/storage/sources/store"
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
// Mirrors the fields set in the b := &Bot{...} literal that used to live
// inside setup.go::New(). Used by NewBot to assign field values without
// exporting Bot's unexported fields.
//
// Stage 1 of US-A13b (composition-root extraction): purely additive
// scaffolding. The existing telegram.New() still owns wiring; this struct
// + constructor let cmd/aura/app.go populate Bot in later slices.
type Deps struct {
	Bot                 *tele.Bot
	Cfg                 *config.Config
	Loc                 *time.Location
	Logger              *slog.Logger
	LLM                 llm.Client
	Wiki                wiki.Repository
	Search              search.Searcher
	Tools               *tools.Registry
	Sources             source.Repository
	OCR                 *ocr.Client
	Skills              *auraskills.Loader
	BgCtx               context.Context
	BgCancel            context.CancelFunc
	SchedDB             cron.AgentJobRepository
	AgentRunner         *agent.Runner
	SwarmStore          swarm.Reader
	SwarmMgr            swarm.RunRunner
	AuthDB              auth.Repository
	MCPClients          []mcp.ConnectedClient
	SandboxMgr          sandbox.ExecutionRuntime
	ToolReg             tools.ToolStore
	CompactMemoryHealth CompactMemoryHealthProvider
	Budget              budget.Runtime
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
	}
}
