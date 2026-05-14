package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent"
	swarmtools "github.com/aura/aura/internal/agent/tools/swarm"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/api/auth"
	"github.com/aura/aura/internal/budget"
	"github.com/aura/aura/internal/config"
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
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
)

// App holds the pre-built Phase A dependencies for Aura. Constructed by
// newApp() before telegram.New() so the Telegram wrapper package only
// handles Telegram-specific wiring (Phase B) and composition (Phase C).
type App struct {
	deps telegram.Deps
	// restart is stored for US-A13b.3 when api.Deps moves to App.
	restart func(context.Context) error
}

// newApp builds all Phase A (pure dependency construction) deps and returns
// an App whose .deps can be handed directly to telegram.New().
//
// US-A13b.2: moves ~400 LOC of pure construction out of telegram/setup.go.
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
	deps.CompactMemoryHealth = compactVectorHealth

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

	// ---- Background context (Bot lifecycle) ---------------------------------
	bgCtx, bgCancel := context.WithCancel(context.Background())
	deps.BgCtx = bgCtx
	deps.BgCancel = bgCancel

	return &App{deps: deps, restart: restart}, nil
}
