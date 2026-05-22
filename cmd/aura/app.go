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

	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	swarmtools "github.com/aura/aura/internal/agent/tools/swarm"
	"github.com/aura/aura/internal/agentnote"
	"github.com/aura/aura/internal/api/auth"
	"github.com/aura/aura/internal/audio"
	"github.com/aura/aura/internal/budget"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/llm/pockettts"
	"github.com/aura/aura/internal/llm/whisper"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/storage/freshness"
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
	archiver         conversation.ClosingTurnAppender // nil until wireBot
	sched            *cron.Scheduler                  // nil until wireBot
	api              http.Handler                     // nil until wireBot
	freshnessStore   *freshness.Store                 // seeded by newApp
	mcpOverridePath  string
	mcpOverrideWatch bool
	mcpRuntimes      []*mcpServerRuntime
	mcpSupervisorRun bool
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
	a.startMCPOverrideWatcher()
	a.startMCPSupervisors()
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
	// 2. Cancel background goroutines (mcpwatch, opsfile-watcher, etc.).
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
	if shouldBootstrapPromptOverlayDefaults(cfg) {
		if err := conversation.EnsurePromptOverlayDefaults(cfg.PromptOverlayPath); err != nil {
			logger.Warn("failed to bootstrap prompt overlay defaults", "path", cfg.PromptOverlayPath, "error", err)
		}
	}

	// ---- LLM client ---------------------------------------------------------
	if cfg.LLMAPIKey != "" {
		deps.LLM = createLLMClient(cfg, logger)
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

	// ---- FTS5 page syncer (wiki write hook) ---------------------------------
	// Wire unconditionally: pool is always available, FTS5 mirror lives in the
	// same SQLite DB. This ensures the keyword-search channel stays in sync
	// even when Qdrant is disabled. ReconcileFTS5Mirror catches stale/empty
	// mirrors on restart (Phase WIKI-FIX-01).
	fts5Syncer := search.NewWikiFTS5Syncer(pool, logger)
	wikiStore.SetFTS5Syncer(fts5Syncer)
	wikiStore.ReconcileFTS5Mirror(context.Background())

	// ---- Search engine (embed + Qdrant vector search) -----------------------
	var embedFn search.EmbeddingFunction
	var batchEmbedFn search.BatchEmbeddingFunction
	compactVectorHealth := memoryindex.NewVectorHealthTracker(false, "")
	deps.CompactVectorHealth = compactVectorHealth

	if cfg.EmbeddingAPIKey != "" {
		embedFn = createEmbeddingFunc(cfg)
		if err := checkEmbedSidecarNCtx(context.Background(), cfg.EmbeddingBaseURL, 2048, logger); err != nil {
			return nil, fmt.Errorf("embed sidecar n_ctx smoke check: %w", err)
		}
		batchEmbedFn = search.NewOpenAICompatBatchEmbeddingFunction(
			cfg.EmbeddingBaseURL, cfg.EmbeddingAPIKey,
			cfg.EmbeddingModel, cfg.EmbeddingOutputDim, nil)
		cacheNamespace := search.EmbedCacheNamespace(cfg.EmbeddingBaseURL, cfg.EmbeddingModel)
		cache, err := search.NewEmbedCacheWithBatchWithDB(pool, cacheNamespace, cfg.EmbeddingOutputDim, embedFn, batchEmbedFn, logger)
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
			searchEngine, err := search.NewQdrantRepositoryWithDB(search.QdrantConfig{
				BaseURL:                cfg.QdrantURL,
				Collection:             cfg.QdrantCollection,
				APIKey:                 cfg.QdrantAPIKey,
				OutputDim:              cfg.EmbeddingOutputDim,
				SkipDimMismatchRebuild: cfg.NoRebuildOnDimMismatch,
			}, embedFn, cfg.WikiPath, pool, logger)
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

	// ---- Whisper sidecar (Phase-MM, US-MM-A02) ------------------------------
	if url := strings.TrimSpace(cfg.WhisperBaseURL); url != "" {
		timeout := time.Duration(cfg.WhisperTimeoutSec) * time.Second
		deps.Whisper = whisper.New(url, timeout, logger)
		logger.Info("whisper sidecar configured", "url", url, "timeout_sec", cfg.WhisperTimeoutSec)
	} else {
		logger.Info("whisper sidecar not configured (set WHISPER_BASE_URL to enable voice transcription)")
	}

	// ---- Pocket-TTS sidecar (Phase-MM Wave 3, US-MM-A05) --------------------
	if url := strings.TrimSpace(cfg.PocketttsBaseURL); url != "" {
		timeout := time.Duration(cfg.PocketttsTimeoutSec) * time.Second
		deps.PocketTTSClient = pockettts.New(url, timeout, logger)
		logger.Info("pocket-tts sidecar configured", "url", url, "timeout_sec", cfg.PocketttsTimeoutSec)
	} else {
		logger.Info("pocket-tts sidecar not configured (set POCKETTTS_BASE_URL to enable TTS)")
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
	sandboxMgr, sandboxHealth := setupSandboxRuntime(cfg, logger)
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
	skillRoots := skillSearchRoots(cfg)
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

	// ---- Agent note store ---------------------------------------------------
	deps.AgentNoteStore = agentnote.NewStore(schedStore.DB())

	// ---- Compact memory vector index ----------------------------------------
	if embedFn != nil && strings.TrimSpace(cfg.QdrantURL) != "" {
		collection := search.CompactMemoryQdrantCollection(cfg.QdrantCollection)
		compactVectorHealth.SetEnabled(true, collection)
		qindex, err := search.NewCompactMemoryQdrantIndexWithBatch(search.QdrantConfig{
			BaseURL:                cfg.QdrantURL,
			Collection:             collection,
			APIKey:                 cfg.QdrantAPIKey,
			OutputDim:              cfg.EmbeddingOutputDim,
			SkipDimMismatchRebuild: cfg.NoRebuildOnDimMismatch,
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

	// ---- Freshness projection registry seed ---------------------------------
	// Seeds 5 projection_state rows idempotently (skips rows that already exist).
	freshnessStore := freshness.NewStore(pool)
	embeddingModelID := cfg.EmbeddingModel
	embeddingDim := cfg.EmbeddingOutputDim
	if embeddingDim <= 0 {
		embeddingDim = 256
	}
	if err := seedProjectionState(context.Background(), freshnessStore, embeddingModelID, embeddingDim); err != nil {
		logger.Warn("freshness projection seed failed", "error", err)
	}
	if memoryStore != nil {
		memoryStore.SetFreshnessStore(freshnessStore)
		if t := int64(cfg.MemorySearchStaleThresholdSecs); t > 0 {
			memoryStore.SetStaleThresholdSecs(t)
		}
	}

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

	var swarmRunner swarm.AgentRunner
	if deps.LLM != nil {
		swarmRunner = &swarmRunnerAdapter{getDeps: newSwarmDepsGetter(cfg, &deps)}
	}

	if cfg.AuraBotEnabled {
		if deps.LLM == nil {
			logger.Warn("AuraBot swarm enabled but no LLM provider configured; swarm tools disabled")
		} else {
			swarmManager, err := swarm.NewManager(swarm.ManagerConfig{
				Runner:    swarmRunner,
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

	mcpOverridePath, mcpRuntimes := registerMCPServers(cfg, &deps, toolRegistry, logger)

	// ---- Auth store ---------------------------------------------------------
	authStore, err := auth.NewStoreWithDB(pool)
	if err != nil {
		return nil, fmt.Errorf("creating auth store: %w", err)
	}
	authStore.SetTokenTTL(time.Duration(cfg.DashboardTokenTTLHours) * time.Hour)
	deps.AuthDB = authStore

	runStore, err := runstore.NewStore(pool)
	if err != nil {
		return nil, fmt.Errorf("creating run store: %w", err)
	}
	deps.RunStore = runStore
	deps.AttemptsRepo = attempts.NewSQLiteRepo(pool)
	deps.VoicePolicy = audio.NewSQLiteStore(pool)

	// ---- Budget tracker -----------------------------------------------------
	deps.Budget = budget.NewTracker(budget.Config{
		SoftBudget:           cfg.SoftBudget,
		HardBudget:           cfg.HardBudget,
		InputCostPerMTokens:  cfg.CostInputPerMTokens,
		OutputCostPerMTokens: cfg.CostOutputPerMTokens,
	}, logger)

	// ---- Background context: owned by App (US-A13c) -------------------------
	bgCtx, bgCancel := context.WithCancel(context.Background())
	// telegram.New() and its per-Bot workers (docHandler OCR pipeline, future
	// long-lived consumers) derive their internal context from bgCtx so App
	// shutdown propagates without needing a separate Stop() round-trip.
	deps.ParentCtx = bgCtx

	return &App{
		deps:            deps,
		restart:         restart,
		bgCtx:           bgCtx,
		bgCancel:        bgCancel,
		freshnessStore:  freshnessStore,
		mcpOverridePath: mcpOverridePath,
		mcpRuntimes:     mcpRuntimes,
	}, nil
}

// import-cycle adapters live in cmd/aura/adapters.go to keep this file focused
// on App lifecycle + wiring. Add new bridges there.
