package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/agent"
	toolindex "github.com/aura/aura/internal/agent/tools/index"
	swarmtools "github.com/aura/aura/internal/agent/tools/swarm"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/api/auth"
	"github.com/aura/aura/internal/budget"
	"github.com/aura/aura/internal/chat"
	cronadapter "github.com/aura/aura/internal/channels/cron"
	silentadapter "github.com/aura/aura/internal/channels/silent"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/mcp"
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
	"github.com/aura/aura/internal/telegram"
	"github.com/aura/aura/internal/wiki"
	"github.com/aura/aura/internal/workspace"
)

// App holds the composition root for Aura. It owns all goroutine lifecycle
// (bgCtx/bgCancel/bgWg), resource cleanup (archiver, reindex, mcpClients,
// sched), and the HTTP API handler. The Telegram Bot is a thin wrapper;
// App.Stop(bot) performs the authoritative shutdown sequence.
type App struct {
	deps    telegram.Deps
	restart func(context.Context) error

	// ---- Goroutine lifecycle (US-A13c) ----------------------------------------
	bgCtx    context.Context
	bgCancel context.CancelFunc
	bgWg     sync.WaitGroup

	// ---- Resources to clean up on Stop ----------------------------------------
	archiver   conversation.ClosingTurnAppender // nil until wireBot
	sched      *cron.Scheduler                  // nil until wireBot
	api        http.Handler                     // nil until wireBot
}

// startBg starts a background goroutine tracked by bgWg under bgCtx.
// The goroutine calls fn(bgCtx) and decrements bgWg when fn returns.
func (a *App) startBg(fn func(context.Context)) {
	a.bgWg.Add(1)
	go func() {
		defer a.bgWg.Done()
		fn(a.bgCtx)
	}()
}

// APIHandler returns the HTTP handler mounted at /api/ by main.go.
func (a *App) APIHandler() http.Handler {
	return a.api
}

// reindexHealth returns a func() reindex.Health for api.Deps.ReindexHealth.
func (a *App) reindexHealth() reindex.Health {
	if a.deps.ReindexWorker == nil {
		return reindex.Health{}
	}
	return a.deps.ReindexWorker.Health()
}

// CompactMemoryHealth satisfies the compactMemoryHealthReader interface used
// by main.go's compactMemoryHealthProvider.
func (a *App) CompactMemoryHealth() memoryindex.VectorHealth {
	if a == nil || a.deps.CompactVectorHealth == nil {
		return memoryindex.VectorHealth{}
	}
	return a.deps.CompactVectorHealth.Snapshot()
}

// Start starts the cron scheduler (non-blocking) and then starts Telegram
// polling (blocking). Must be called in a goroutine from main.
func (a *App) Start(bot *telegram.Bot) {
	if a.sched != nil {
		a.sched.Start(a.bgCtx)
	}
	bot.Start()
}

// Stop shuts down the Telegram bot, then cancels background goroutines and
// waits for them to finish, then closes all owned resources in reverse
// dependency order. Idempotent: safe to call more than once.
func (a *App) Stop(bot *telegram.Bot) {
	// 1. Telegram-specific shutdown (gate, polling, docs).
	if bot != nil {
		bot.Stop()
	}
	// 2. Cancel background goroutines (toolReconciler, mcpwatch, etc.).
	if a.bgCancel != nil {
		a.bgCancel()
	}
	a.bgWg.Wait()
	// 3. Stop reindex worker.
	if a.deps.ReindexWorker != nil {
		a.deps.ReindexWorker.Stop()
	}
	// 4. Flush + close conversation archiver.
	if a.archiver != nil {
		if err := a.archiver.Close(context.Background()); err != nil {
			a.deps.Logger.Warn("telegram shutdown: archiver close failed", "error", err)
		}
	}
	// 5. Close MCP client subprocesses.
	for _, cli := range a.deps.MCPClients {
		if err := cli.Close(); err != nil {
			a.deps.Logger.Warn("mcp client close failed", "error", err)
		}
	}
	// 6. Stop scheduler (drains in-flight dispatches).
	if a.sched != nil {
		a.sched.Stop()
	}
}

// newApp builds all Phase A (pure dependency construction) deps and returns
// an App whose .deps can be handed directly to telegram.New().
//
// US-A13b.2: moves ~400 LOC of pure construction out of telegram/setup.go.
// US-A13c: bgCtx/bgCancel now live on App, not on Deps/Bot.
func newApp(
	cfg *config.Config,
	settingsStore config.Repository,
	pool *sql.DB,
	logger *slog.Logger,
	restart func(context.Context) error,
) (*App, error) {
	var deps telegram.Deps
	deps.Cfg = cfg
	deps.Logger = logger
	deps.Pool = pool
	deps.SettingsStore = settingsStore

	// ---- Timezone -----------------------------------------------------------
	loc, err := cfg.Location()
	if err != nil {
		return nil, fmt.Errorf("loading timezone %q: %w", cfg.Timezone, err)
	}
	deps.Loc = loc

	// ---- Prompt overlay bootstrap -------------------------------------------
	// Idempotent; only runs when the overlay path points at the standard
	// runtime workspace so operator-managed overlays are not overwritten.
	if telegram.ShouldBootstrapPromptOverlayDefaults(cfg) {
		if err := conversation.EnsurePromptOverlayDefaults(cfg.PromptOverlayPath); err != nil {
			logger.Warn("failed to bootstrap prompt overlay defaults", "path", cfg.PromptOverlayPath, "error", err)
		}
	}

	// ---- LLM client ---------------------------------------------------------
	if cfg.LLMAPIKey != "" {
		deps.LLM = telegram.CreateLLMClient(cfg, logger)
	} else {
		logger.Warn("no LLM provider configured, bot will echo messages without LLM")
	}

	// ---- Qdrant client ------------------------------------------------------
	if strings.TrimSpace(cfg.QdrantURL) != "" {
		qcli, err := qdrant.NewClient(qdrant.Config{
			BaseURL: cfg.QdrantURL,
			APIKey:  cfg.QdrantAPIKey,
		})
		if err != nil {
			logger.Warn("failed to create qdrant client; qdrant-dependent features disabled", "error", err)
		} else {
			healthCtx, healthCancel := context.WithTimeout(context.Background(), 120*time.Second)
			waitErr := qdrant.WaitForReady(healthCtx, qcli, 120*time.Second)
			healthCancel()
			if waitErr != nil {
				return nil, fmt.Errorf("qdrant health gate failed: %w", waitErr)
			}
			logger.Info("qdrant health gate passed", "url", cfg.QdrantURL)
			deps.QdrantClient = qcli
		}
	}

	// ---- Wiki store ---------------------------------------------------------
	wikiStore, err := wiki.NewStore(cfg.WikiPath, logger)
	if err != nil {
		return nil, fmt.Errorf("creating wiki store: %w", err)
	}
	if migrated, err := wikiStore.MigrateYAMLToMD(context.Background()); err != nil {
		logger.Warn("wiki migration failed", "error", err)
	} else if migrated > 0 {
		logger.Info("wiki migration completed", "pages_migrated", migrated)
	}
	wikiStore.RebuildGraph(context.Background())
	deps.WikiStore = wikiStore
	deps.Wiki = wikiStore

	// ---- Search engine (embed + Qdrant vector search) -----------------------
	var embedFn search.EmbeddingFunction
	var batchEmbedFn search.BatchEmbeddingFunction
	compactVectorHealth := memoryindex.NewVectorHealthTracker(false, "")
	deps.CompactVectorHealth = compactVectorHealth

	if cfg.EmbeddingAPIKey != "" {
		embedFn = telegram.CreateEmbeddingFunc(cfg)
		batchEmbedFn = search.NewOpenAICompatBatchEmbeddingFunction(
			cfg.EmbeddingBaseURL, cfg.EmbeddingAPIKey,
			cfg.EmbeddingModel, cfg.EmbeddingOutputDim, nil)
		cacheNamespace := search.EmbedCacheNamespace(cfg.EmbeddingBaseURL, cfg.EmbeddingModel)
		cache, err := search.NewEmbedCacheWithBatchWithDB(pool, cacheNamespace, embedFn, batchEmbedFn, logger)
		if err != nil {
			logger.Warn("embed cache unavailable, falling back to uncached embedding", "error", err)
		} else {
			embedFn = cache.EmbedFunc()
			batchEmbedFn = cache.BatchEmbed
			deps.EmbedCache = cache
		}
		if strings.TrimSpace(cfg.QdrantURL) == "" {
			logger.Warn("QDRANT_URL is required for wiki vector search; search disabled")
		} else {
			searchEngine, err := search.NewQdrantRepository(search.QdrantConfig{
				BaseURL:    cfg.QdrantURL,
				Collection: cfg.QdrantCollection,
				APIKey:     cfg.QdrantAPIKey,
			}, embedFn, cfg.WikiPath, logger)
			if err != nil {
				logger.Warn("failed to create qdrant search backend, search disabled", "error", err)
			} else if err := searchEngine.IndexWikiPages(context.Background()); err != nil {
				logger.Warn("failed to index wiki pages in qdrant on startup", "error", err)
			} else {
				logger.Info("qdrant search backend enabled", "url", cfg.QdrantURL, "collection", cfg.QdrantCollection)
				deps.SearchRepo = searchEngine
				deps.Search = searchEngine
			}
		}
	}

	// ---- Reindex worker -----------------------------------------------------
	if deps.SearchRepo != nil {
		rw := reindex.NewWorker(deps.SearchRepo, reindex.DefaultConfig())
		deps.ReindexWorker = rw
		// Wire the reindex submitter on the concrete store type.
		wikiStore.SetReindexSubmitter(rw)
	}

	// ---- Source store -------------------------------------------------------
	sourceStore, err := source.NewStore(cfg.WikiPath, logger)
	if err != nil {
		return nil, fmt.Errorf("creating source store: %w", err)
	}
	deps.Sources = sourceStore

	// ---- OCR client ---------------------------------------------------------
	if cfg.MistralAPIKey != "" {
		deps.OCR = ocr.New(ocr.Config{
			APIKey:        cfg.MistralAPIKey,
			BaseURL:       cfg.MistralOCRBaseURL,
			Model:         cfg.MistralOCRModel,
			TableFormat:   cfg.MistralOCRTableFormat,
			ExtractHeader: cfg.MistralOCRExtractHeader,
			ExtractFooter: cfg.MistralOCRExtractFooter,
		})
	} else {
		logger.Info("OCR disabled (set MISTRAL_API_KEY to enable)")
	}

	// ---- Markitdown sidecar -------------------------------------------------
	if url := strings.TrimSpace(cfg.MarkitdownURL); url != "" {
		timeout := time.Duration(cfg.MarkitdownTimeoutSec) * time.Second
		deps.Markitdown = markitdown.New(markitdown.Config{BaseURL: url, Timeout: timeout})
		logger.Info("markitdown sidecar configured", "url", url, "timeout_sec", cfg.MarkitdownTimeoutSec)
	} else {
		logger.Warn("markitdown sidecar not configured (set MARKITDOWN_URL); non-PDF uploads will fail")
	}

	// ---- Ingest pipeline ----------------------------------------------------
	var ingestExtractor ingest.Extractor
	if deps.LLM != nil {
		ingestExtractor = ingest.NewLLMExtractor(deps.LLM, cfg.LLMModel)
	}
	ingestPipeline, err := ingest.New(ingest.Config{
		Sources:   sourceStore,
		Wiki:      wikiStore,
		Search:    deps.SearchRepo,
		Extractor: ingestExtractor,
		Logger:    logger,
	})
	if err != nil {
		return nil, fmt.Errorf("creating ingest pipeline: %w", err)
	}
	deps.Ingest = ingestPipeline

	// ---- Sandbox ------------------------------------------------------------
	sandboxMgr, sandboxHealth := telegram.SetupSandboxRuntime(cfg, logger)
	deps.SandboxMgr = sandboxMgr
	deps.SandboxHealth = sandboxHealth

	// ---- Tool registry ------------------------------------------------------
	toolReg, err := tools.NewToolRegistry(wikiStore)
	if err != nil {
		logger.Warn("tool registry unavailable", "error", err)
	}
	deps.ToolReg = toolReg

	toolRegistry := tools.NewRegistry(logger)
	deps.Tools = toolRegistry

	if config.NormalizeWorkspaceTools(cfg.WorkspaceTools) == "enabled" {
		workspaceRoot, err := workspace.New(cfg.WorkspaceRoot)
		if err != nil {
			logger.Warn("workspace file tools unavailable", "root", cfg.WorkspaceRoot, "error", err)
		} else {
			if fileTool := tools.NewFileTool(workspaceRoot); fileTool != nil {
				toolRegistry.Register(tools.WithCategory(fileTool, tools.CategoryAutonomous))
			}
			logger.Info("workspace file tools enabled", "root", workspaceRoot.Path())
		}
	} else {
		logger.Info("workspace file tools disabled (set AURA_WORKSPACE_TOOLS=enabled to enable)")
	}

	// ---- Skills loader ------------------------------------------------------
	skillRoots := telegram.SkillSearchRoots(cfg)
	skillLoader := auraskills.NewLoader(skillRoots[0], skillRoots[1:]...)
	deps.Skills = skillLoader
	deps.SkillsCatalog = auraskills.NewCatalogClient(cfg.SkillsCatalogURL)

	// ---- Web search tool ----------------------------------------------------
	switch strings.ToLower(strings.TrimSpace(cfg.WebSearchProvider)) {
	case "searxng":
		toolRegistry.Register(tools.WithCategory(tools.NewWebTool(cfg.SearXNGBaseURL), tools.CategoryAutonomous))
	case "", "disabled":
		// Explicitly disabled.
	default:
		logger.Warn("unknown WEB_SEARCH_PROVIDER; web tools disabled", "provider", cfg.WebSearchProvider)
	}

	// ---- Scheduler store ----------------------------------------------------
	schedStore, err := cron.NewStoreWithDB(pool)
	if err != nil {
		return nil, fmt.Errorf("creating scheduler store: %w", err)
	}
	deps.SchedDB = schedStore

	// ---- Task tool (runner wired after Bot construction in setup.go) --------
	taskTool := tools.NewTaskTool(schedStore, nil, loc)
	if taskTool != nil {
		toolRegistry.Register(taskTool)
	}
	deps.TaskTool = taskTool

	// ---- Summaries store ----------------------------------------------------
	deps.SummariesStore = summarizer.NewSummariesStore(schedStore.DB())

	// ---- Compact memory vector index ----------------------------------------
	if embedFn != nil && strings.TrimSpace(cfg.QdrantURL) != "" {
		collection := search.CompactMemoryQdrantCollection(cfg.QdrantCollection)
		compactVectorHealth.SetEnabled(true, collection)
		qindex, err := search.NewCompactMemoryQdrantIndexWithBatch(search.QdrantConfig{
			BaseURL:    cfg.QdrantURL,
			Collection: collection,
			APIKey:     cfg.QdrantAPIKey,
		}, embedFn, batchEmbedFn, logger)
		if err != nil {
			logger.Warn("compact qdrant memory mirror unavailable; using SQLite compact memory only", "error", err)
		} else {
			deps.CompactVector = qindex
			logger.Info("compact qdrant memory mirror enabled", "url", cfg.QdrantURL, "collection", collection)
		}
	}

	// ---- Memory store -------------------------------------------------------
	memoryStore, err := memoryindex.NewStoreWithVector(schedStore.DB(), deps.CompactVector, logger)
	if err != nil {
		logger.Warn("compact memory index unavailable", "error", err)
	}
	deps.MemoryStore = memoryStore

	// ---- Source tool (needs memoryStore as delete purger) -------------------
	if sourceTool := tools.NewSourceTool(sourceStore, sourceStore, sourceStore, sourceStore, deps.OCR, ingestPipeline, memoryStore); sourceTool != nil {
		toolRegistry.Register(tools.WithCategory(sourceTool, tools.CategoryAutonomous))
	}

	// ---- Swarm store + manager + tools --------------------------------------
	swarmStore, err := swarm.NewStoreWithDB(pool)
	if err != nil {
		return nil, fmt.Errorf("creating swarm store: %w", err)
	}
	deps.SwarmStore = swarmStore

	timeoutSec := cfg.AuraBotTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = config.DefaultAuraBotTimeoutSec
	}
	maxIterations := cfg.AuraBotMaxIterations
	if maxIterations <= 0 {
		maxIterations = 5
	}

	if deps.LLM != nil {
		auraRunner, err := agent.NewRunner(agent.Config{
			LLM:             deps.LLM,
			Tools:           toolRegistry,
			Model:           cfg.LLMModel,
			MaxIterations:   maxIterations,
			Timeout:         time.Duration(timeoutSec) * time.Second,
			ToolTimeout:     time.Duration(timeoutSec) * time.Second,
			ReasoningEffort: cfg.ReasoningEffort,
			Logger:          logger,
			PhantomToolGuard: &agent.PhantomToolGuard{
				ToolNamesFn: toolRegistry.Names,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("creating aurabot runner: %w", err)
		}
		deps.AgentRunner = auraRunner
	}

	if cfg.AuraBotEnabled {
		if deps.LLM == nil {
			logger.Warn("AuraBot swarm enabled but no LLM provider configured; swarm tools disabled")
		} else {
			swarmManager, err := swarm.NewManager(swarm.ManagerConfig{
				Runner:    deps.AgentRunner,
				Store:     swarmStore,
				MaxActive: cfg.AuraBotMaxActive,
				MaxDepth:  cfg.AuraBotMaxDepth,
				Logger:    logger,
			})
			if err != nil {
				return nil, fmt.Errorf("creating swarm manager: %w", err)
			}
			deps.SwarmMgr = swarmManager
			if tool := swarmtools.NewSpawnAuraBotTool(swarmManager); tool != nil {
				toolRegistry.Register(tool)
			}
			if tool := swarmtools.NewRunAuraBotSwarmTool(swarmManager); tool != nil {
				toolRegistry.Register(tool)
			}
			if tool := swarmtools.NewListSwarmTasksTool(swarmStore); tool != nil {
				toolRegistry.Register(tool)
			}
			if tool := swarmtools.NewReadSwarmResultTool(swarmStore); tool != nil {
				toolRegistry.Register(tool)
			}
			logger.Info("AuraBot swarm enabled",
				"max_active", cfg.AuraBotMaxActive,
				"max_depth", cfg.AuraBotMaxDepth,
				"timeout_sec", timeoutSec)
		}
	} else {
		logger.Info("AuraBot swarm disabled (set AURABOT_ENABLED=true to enable)")
	}

	// ---- MCP servers --------------------------------------------------------
	mcpServers, mcpErr := mcp.LoadServers(cfg.MCPServersPath)
	if mcpErr != nil {
		logger.Warn("MCP config load failed; continuing without MCP", "error", mcpErr, "path", cfg.MCPServersPath)
	}
	mcpClients := make([]mcp.ConnectedClient, 0, len(mcpServers))
	for name, srv := range mcpServers {
		srv.Env = mcp.NormalizeRuntimeEnv(name, srv.Env)
		var cli *mcp.Client
		var err error
		switch {
		case srv.Command != "":
			cli, err = mcp.NewStdioClient(name, srv.Command, srv.Args, srv.Env)
		case srv.URL != "":
			cli, err = mcp.NewHTTPClient(name, srv.URL, srv.Headers)
		}
		if err != nil {
			logger.Warn("MCP server unavailable", "server", name, "error", err)
			continue
		}
		mcpClients = append(mcpClients, cli)
		registeredTools := 0
		for _, t := range cli.Tools() {
			if !mcp.ToolEnabledForAura(name, srv.Env, t.Name) {
				continue
			}
			tool := tools.NewMCPTool(cli, name, t)
			if mcp.ToolAutonomousForAura(name, t.Name) {
				toolRegistry.Register(tools.WithCategory(tool, tools.CategoryAutonomous))
			} else {
				toolRegistry.Register(tool)
			}
			registeredTools++
		}
		logger.Info("MCP server registered", "server", name, "tools", len(cli.Tools()), "aura_tools", registeredTools)
	}
	deps.MCPClients = mcpClients

	// ---- Auth store ---------------------------------------------------------
	authStore, err := auth.NewStoreWithDB(pool)
	if err != nil {
		return nil, fmt.Errorf("creating auth store: %w", err)
	}
	authStore.SetTokenTTL(time.Duration(cfg.DashboardTokenTTLHours) * time.Hour)
	deps.AuthDB = authStore

	// ---- Budget tracker -----------------------------------------------------
	deps.Budget = budget.NewTracker(budget.Config{
		SoftBudget:           cfg.SoftBudget,
		HardBudget:           cfg.HardBudget,
		InputCostPerMTokens:  cfg.CostInputPerMTokens,
		OutputCostPerMTokens: cfg.CostOutputPerMTokens,
	}, logger)

	// ---- Background context: owned by App (US-A13c) -------------------------
	bgCtx, bgCancel := context.WithCancel(context.Background())

	return &App{deps: deps, restart: restart, bgCtx: bgCtx, bgCancel: bgCancel}, nil
}

// wireBot performs Phase C composition wiring after telegram.New() constructs
// the Bot (Phase B). It builds api.Router, cron.Scheduler, conversation archive,
// memoryStore rebuild, toolReconciler + mcpwatch goroutines, and wires them onto
// b via exported setter methods.
//
// Goroutine lifecycle is owned by App (a.startBg / a.bgCtx).
// Resource cleanup (archiver.Close, reindex.Stop, mcp.Close, sched.Stop) is
// performed by App.Stop, NOT by bot.Stop.
//
// Must be called from cmd/aura before App.Start(bot) so all infrastructure is
// ready before the first Telegram message arrives.
func (a *App) wireBot(b *telegram.Bot) error {
	cfg := a.deps.Cfg
	logger := a.deps.Logger
	loc := a.deps.Loc

	// ---- Tool vector index: reconciler + mcpwatch goroutines ----------------
	toolReaderConfig := tools.ToolVectorConfig{
		Backend:      cfg.ToolSearchBackend,
		TopK:         cfg.ToolSearchTopK,
		QdrantURL:    cfg.QdrantURL,
		QdrantAPIKey: cfg.QdrantAPIKey,
		Collection:   tools.ToolSearchCollection,
		EmbedBaseURL: cfg.EmbeddingBaseURL,
		EmbedAPIKey:  cfg.EmbeddingAPIKey,
		EmbedModel:   cfg.EmbeddingModel,
	}
	var reconciler *toolindex.Reconciler
	if a.deps.QdrantClient != nil && a.deps.EmbedCache != nil && cfg.ToolSearchBackend != "fts" {
		r, err := toolindex.New(toolindex.Config{
			Provider:    a.deps.Tools,
			Qdrant:      a.deps.QdrantClient,
			Embedder:    a.deps.EmbedCache,
			State:       toolindex.NewSQLiteStateStore(a.deps.Pool),
			Collection:  tools.ToolSearchCollection,
			VectorDim:   tools.ToolVectorDim(cfg.EmbeddingOutputDim),
			EmbedModel:  search.EmbedCacheNamespace(cfg.EmbeddingBaseURL, cfg.EmbeddingModel),
			EmbedTextFn: tools.SearchableEmbeddingTextForLLMDef,
			PointIDFn:   tools.ToolQdrantPointID,
			Logger:      logger.With("component", "toolindex"),
		})
		if err != nil {
			logger.Warn("tool index reconciler unavailable", "error", err)
		} else {
			rep := r.Reconcile(context.Background(), toolindex.ReasonBoot)
			logger.Info("tool index reconciled at boot",
				"upserted", len(rep.Upserted), "deleted", len(rep.Deleted),
				"unchanged", rep.Unchanged, "errors", len(rep.Errors),
				"elapsed_ms", rep.ElapsedMs)
			for i, e := range rep.Errors {
				logger.Warn("tool index reconcile error", "idx", i, "err", e)
			}
			reconciler = r
			// toolReconciler.Run goroutine lifecycle owned by App (US-A13c).
			a.startBg(r.Run)

			// fsnotify on mcp.json — debounced notification when the operator edits the file.
			// mcpwatch goroutine lifecycle owned by App (US-A13c).
			if cfg.MCPServersPath != "" {
				watcher, werr := mcp.New(mcp.Config{
					Path:     cfg.MCPServersPath,
					Callback: func() { r.Notify(toolindex.ReasonMCPConfig) },
					Logger:   logger.With("component", "mcpwatch"),
				})
				if werr != nil {
					logger.Warn("mcp.json watcher unavailable", "error", werr)
				} else {
					a.startBg(func(ctx context.Context) {
						if rerr := watcher.Run(ctx); rerr != nil {
							logger.Warn("mcpwatch exited with error", "error", rerr)
						}
					})
				}
			}
		}
	}
	// Wire the ToolVectorIndex on the registry for the search path.
	a.deps.Tools.PrepareVectorReader(toolReaderConfig)

	// ---- Conversation archive ------------------------------------------------
	var archiveDB conversation.ArchiveRepository
	if cfg.ConvArchiveEnabled {
		archiveStore, err := conversation.NewArchiveStore(a.deps.SchedDB.DB())
		if err != nil {
			logger.Warn("conversation archive unavailable", "error", err)
		} else {
			if a.deps.MemoryStore != nil {
				archiveDB = memoryindex.NewIndexingArchiveRepository(archiveStore, a.deps.MemoryStore)
			} else {
				archiveDB = archiveStore
			}
			b.SetArchiveDB(archiveDB)
			archiver := conversation.NewBufferedAppender(archiveDB, 100)
			// App owns the archiver Close lifecycle (US-A13c).
			a.archiver = archiver
			b.SetArchiver(archiver)
		}
	}

	// ---- Compact memory index rebuild ----------------------------------------
	if a.deps.MemoryStore != nil {
		rebuildCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		report, err := memoryindex.Rebuild(rebuildCtx, a.deps.MemoryStore, memoryindex.RebuildInput{
			Sources:    a.deps.Sources,
			Archive:    archiveDB,
			Proposals:  a.deps.SummariesStore,
			SkipVector: a.deps.CompactVector != nil,
		})
		cancel()
		if err != nil {
			logger.Warn("compact memory index rebuild failed", "error", err)
		} else {
			logger.Info("compact memory index rebuilt",
				"sources", report.SourcesIndexed, "archive", report.ArchiveIndexed,
				"proposals", report.ProposalsIndexed,
				"vector_collection", report.Vector.Collection, "vector_docs", report.Vector.DocsIndexed)
		}
		if a.deps.CompactVector != nil {
			// Compact memory vector mirror sync goroutine owned by App (US-A13c).
			go func() {
				vectorCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				logger.Info("compact memory vector mirror sync started")
				a.deps.CompactVectorHealth.Started()
				syncReport, err := a.deps.MemoryStore.SyncVector(vectorCtx)
				if err != nil {
					a.deps.CompactVectorHealth.Failed(err)
					logger.Warn("compact memory vector mirror sync failed", "error", err)
					return
				}
				a.deps.CompactVectorHealth.Succeeded(syncReport)
				logger.Info("compact memory vector mirror synced",
					"vector_collection", syncReport.Collection, "vector_docs", syncReport.DocsIndexed,
					"vector_size", syncReport.VectorSize)
			}()
		}
	}

	// SearchMemoryTool — registered after memory rebuild so the index is populated.
	if tool := tools.NewSearchMemoryToolConfigured(a.deps.SearchRepo, a.deps.MemoryStore,
		time.Duration(cfg.MemorySearchTimeoutMS)*time.Millisecond,
		cfg.RecencyHalfLifeWikiDays, cfg.RecencyHalfLifeArchiveDays); tool != nil {
		a.deps.Tools.Register(tool)
	}

	// ---- Wiki issues store + daily briefing tool ----------------------------
	issues := cron.NewIssuesStore(a.deps.SchedDB.DB())
	b.SetIssues(issues)
	if tool := tools.NewDailyBriefingTool(a.deps.SchedDB, a.deps.Sources, a.deps.SummariesStore, issues, archiveDB, loc); tool != nil {
		a.deps.Tools.Register(tool)
	}

	// ---- Bot-dependent tool registrations (need *Bot as sender/runner) -----
	// Sandbox tools require b as DocumentSender; ToolSearch, token, doc, wiki_page
	// need b constructed and available. Registered before scheduler so they're
	// live when the first Telegram message arrives.
	if tool := tools.NewExecuteCodeToolWithStoreAndRegistry(a.deps.SandboxMgr, b, a.deps.Sources, a.deps.Tools); tool != nil {
		a.deps.Tools.Register(tool)
	}
	if tool := tools.NewExecuteShellTool(a.deps.SandboxMgr); tool != nil {
		a.deps.Tools.Register(tool)
	}
	if tool := tools.NewDevToolTool(a.deps.ToolReg); tool != nil {
		a.deps.Tools.Register(tools.WithCategory(tool, tools.CategoryAutonomous))
	}
	if tool := tools.NewToolSearchTool(a.deps.Tools); tool != nil {
		a.deps.Tools.Register(tool)
	}
	if tokenTool := tools.NewRequestDashboardTokenTool(a.deps.AuthDB, b, b.IsAllowlisted); tokenTool != nil {
		a.deps.Tools.Register(tokenTool)
	}
	if docTool := tools.NewDocTool(a.deps.Sources, b); docTool != nil {
		a.deps.Tools.Register(docTool)
	}
	if t := tools.NewWikiPageTool(a.deps.WikiStore, a.deps.ReindexWorker); t != nil {
		a.deps.Tools.Register(t)
	}

	// ---- Cron scheduler -----------------------------------------------------
	// Build a Handler from injected deps so cron no longer depends on *Bot directly.
	cronHandler := cron.NewHandler(cron.HandlerConfig{
		Notifier: b,
		Wiki:     a.deps.Wiki,
		Issues:   issues,
		Sources:  a.deps.Sources,
		SchedDB:  a.deps.SchedDB,
		Logger:   logger,
		Location: loc,
	})
	// Wire run_now action now that the handler (not *Bot) implements RunNow.
	if a.deps.TaskTool != nil {
		a.deps.TaskTool.SetRunner(&scheduledTaskRunnerAdapter{h: cronHandler})
	}
	// Route KindAgentJob through the cron Hub (InboundAdapter → CronAgentLoop →
	// silent Outbound); all other kinds fall back to cronHandler.Dispatch.
	cronDispatcher := cron.Dispatcher(cronHandler.Dispatch)
	if a.deps.AgentRunner != nil {
		cronLoop := cronadapter.NewCronAgentLoop(&agentJobRunnerAdapter{r: a.deps.AgentRunner}, loc)
		cronHub, hubErr := chat.New(chat.Config{Loop: cronLoop, Logger: logger})
		if hubErr != nil {
			logger.Warn("cron hub unavailable; falling back to legacy agent dispatch", "error", hubErr)
		} else {
			cronHub.RegisterInbound(cronadapter.New())
			cronHub.RegisterOutbound(silentadapter.NewCron(silentadapter.Config{Logger: logger}))
			cronDispatcher = cronadapter.NewHubDispatcher(cronHub, cronHandler.Dispatch)
		}
	}
	sched, err := cron.New(cron.Config{
		Store:      a.deps.SchedDB,
		Dispatcher: cronDispatcher,
		Logger:     logger,
		Location:   loc,
	})
	if err != nil {
		return fmt.Errorf("creating scheduler: %w", err)
	}
	// App owns the scheduler lifecycle (US-A13c).
	a.sched = sched

	// Bootstrap the autonomous nightly wiki-maintenance task (idempotent upsert).
	nightlyAt, err := cron.NextDailyRun("03:00", loc, time.Now())
	if err != nil {
		return fmt.Errorf("computing nightly run: %w", err)
	}
	if _, err := a.deps.SchedDB.Upsert(context.Background(), &cron.Task{
		Name:          "nightly-wiki-maintenance",
		Kind:          cron.KindWikiMaintenance,
		ScheduleKind:  cron.ScheduleDaily,
		ScheduleDaily: "03:00",
		NextRunAt:     nightlyAt,
		Status:        cron.StatusActive,
	}); err != nil {
		logger.Warn("failed to bootstrap nightly maintenance task", "err", err)
	}

	// ---- Skill adapters (bridge auraskills → api interfaces) ----------------
	skillRoots := telegram.SkillSearchRoots(cfg)
	skillsInstaller, err := auraskills.NewNPXInstaller(cfg.SkillsPath, cfg.SkillsInstallProjectDir)
	if err != nil {
		logger.Warn("skills installer unavailable", "error", err)
	}
	skillsDeleter, err := auraskills.NewFSDeleter(skillRoots[0], skillRoots[1:]...)
	if err != nil {
		logger.Warn("skills deleter unavailable", "error", err)
	}
	skillProposalApplier, err := auraskills.NewFSProposalApplier(skillRoots[0])
	if err != nil {
		logger.Warn("skill proposal applier unavailable", "error", err)
	}

	// ---- HTTP API router (dashboard + /api endpoints) -----------------------
	// App owns the api handler; App.APIHandler() exposes it to main.go (US-A13c).
	a.api = api.NewRouter(api.Deps{
		Wiki:        a.deps.WikiStore,
		Sources:     a.deps.Sources,
		Scheduler:   a.deps.SchedDB,
		OCR:         a.deps.OCR,
		Ingest:      a.deps.Ingest,
		Markitdown:  a.deps.Markitdown,
		Auth:        a.deps.AuthDB,
		Allowlist:   b.IsAllowlisted,
		MaxUploadMB: cfg.OCRMaxFileMB,
		Location:    loc,
		// Surface reindex worker health via /api/health (US-A13c: moved from bot).
		ReindexHealth: a.reindexHealth,
		// Keep in sync with cmd/aura/main.go's auraVersion.
		Version:   "3.0",
		StartedAt: time.Now().UTC(),
		Logger:    logger,
		// Skills + MCP dashboard panels.
		Skills: a.deps.Skills,
		MCP:    a.deps.MCPClients,
		// skills.sh catalog + admin-gated install/delete.
		SkillsCatalog:   a.deps.SkillsCatalog,
		SkillsInstaller: skillsInstaller,
		SkillsDeleter:   telegram.NewSkillsDeleterAdapter(skillsDeleter),
		SkillProposals:  telegram.NewSkillProposalApplierAdapter(skillProposalApplier),
		SkillsAdmin:     cfg.SkillsAdmin,
		// Embed cache hit/miss counters in /api/health.
		EmbedCache:    a.deps.EmbedCache,
		CompactMemory: a.deps.CompactVectorHealth,
		Sandbox:       a.deps.SandboxHealth,
		// ToolReconciler backs POST /api/tools/reindex.
		ToolReconciler: reconciler,
		// SourcePurger for DELETE /sources/{id} compact memoryindex cleanup.
		SourcePurger: a.deps.MemoryStore,
		// Multi-root file manager.
		WikiDir:      cfg.WikiPath,
		WorkspaceDir: cfg.WorkspaceRoot,
		SkillsDir:    cfg.SkillsPath,
		// WikiSearch reindexes after dashboard wiki writes/renames/deletes.
		WikiSearch: a.deps.SearchRepo,
		// Pending-approval pipeline.
		PendingApprover: b,
		// Conversation archive read API.
		Archive: archiveDB,
		// Summaries review queue.
		Summaries:     a.deps.SummariesStore,
		SummariesWiki: a.deps.WikiStore,
		// Wiki maintenance issue queue.
		Issues: issues,
		// Runtime settings page.
		Settings:             a.deps.SettingsStore,
		RuntimeConfig:        cfg,
		ApplyRuntimeSettings: b.RuntimeSettingsApplier(a.deps),
		Restart:              a.restart,
		// AuraBot swarm observability.
		Swarm: a.deps.SwarmStore,
		Chat:  api.NewWebChatService(a.deps.AgentRunner, a.deps.Tools),
	})

	// Wave 2.10.b — late notify. Fire one final Notify so the debounced reconcile
	// picks up every tool that landed in the registry during setup.
	if reconciler != nil {
		reconciler.Notify(toolindex.ReasonBoot)
	}
	return nil
}

// ---- import-cycle adapters --------------------------------------------------

// agentJobRunnerAdapter bridges *agent.Runner to cron.JobRunner.
type agentJobRunnerAdapter struct {
	r *agent.Runner
}

func (a *agentJobRunnerAdapter) RunJob(ctx context.Context, req cron.JobRequest) (cron.JobResult, error) {
	if a.r == nil {
		return cron.JobResult{}, fmt.Errorf("agent runner unavailable")
	}
	result, err := a.r.Run(ctx, agent.Task{
		SystemPrompt:  req.SystemPrompt,
		Prompt:        req.Prompt,
		ToolAllowlist: req.ToolAllowlist,
		UserID:        req.UserID,
		Temperature:   req.Temperature,
	})
	if err != nil {
		return cron.JobResult{}, err
	}
	return cron.JobResult{
		Content:          result.Content,
		LLMCalls:         result.LLMCalls,
		ToolCalls:        result.ToolCalls,
		TokensPrompt:     result.Tokens.PromptTokens,
		TokensCompletion: result.Tokens.CompletionTokens,
		TokensTotal:      result.Tokens.TotalTokens,
		Elapsed:          result.Elapsed,
	}, nil
}

// scheduledTaskRunnerAdapter bridges cron.Handler.RunNow to tools.ScheduledTaskRunner.
type scheduledTaskRunnerAdapter struct {
	h *cron.Handler
}

func (a *scheduledTaskRunnerAdapter) RunTaskNow(ctx context.Context, name string) (tools.RunTaskNowResult, error) {
	res, err := a.h.RunNow(ctx, name)
	if err != nil {
		return tools.RunTaskNowResult{}, err
	}
	return tools.RunTaskNowResult{
		OK:               res.OK,
		Name:             res.Name,
		Kind:             res.Kind,
		Status:           res.Status,
		Summary:          res.Summary,
		LastError:        res.LastError,
		LLMCalls:         res.LLMCalls,
		ToolCalls:        res.ToolCalls,
		TokensPrompt:     res.TokensPrompt,
		TokensCompletion: res.TokensCompletion,
		TokensTotal:      res.TokensTotal,
		ElapsedMS:        res.ElapsedMS,
		Notified:         res.Notified,
		Skipped:          res.Skipped,
		WakeSignature:    res.WakeSignature,
		ToolAllowlist:    res.ToolAllowlist,
	}, nil
}
