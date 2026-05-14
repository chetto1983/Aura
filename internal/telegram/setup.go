package telegram

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/api/auth"
	"github.com/aura/aura/internal/budget"
	"github.com/aura/aura/internal/concurrency"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/mcp"
	"github.com/aura/aura/internal/storage/sources/ingest"
	"github.com/aura/aura/internal/storage/sources/markitdown"

	"github.com/aura/aura/internal/agent/tools/index"
	"github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/agent/tools/swarm"
	"github.com/aura/aura/internal/cron"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/qdrant"
	"github.com/aura/aura/internal/storage/reindex"
	"github.com/aura/aura/internal/storage/search"
	"github.com/aura/aura/internal/storage/sources/ocr"
	"github.com/aura/aura/internal/storage/sources/store"
	"github.com/aura/aura/internal/swarm"
	"github.com/aura/aura/internal/wiki"
	"github.com/aura/aura/internal/workspace"

	tele "gopkg.in/telebot.v4"
)

type Option func(*options)

type options struct {
	Restart func(context.Context) error
}

func WithRestart(fn func(context.Context) error) Option {
	return func(o *options) {
		o.Restart = fn
	}
}

// New creates a new Telegram bot with allowlist enforcement and LLM integration.
//
// settingsStore is the runtime configuration repository opened by main.go on
// cfg.DBPath. It's threaded through so the dashboard's /settings page can
// persist edits on the shared SQLite pool. May be nil (tests) — in that case
// the dashboard /settings endpoints respond 503.
func New(cfg *config.Config, settingsStore config.Repository, pool *sql.DB, logger *slog.Logger, opts ...Option) (*Bot, error) {
	if pool == nil {
		return nil, fmt.Errorf("telegram: db pool required")
	}
	var opt options
	for _, apply := range opts {
		if apply != nil {
			apply(&opt)
		}
	}
	loc, err := cfg.Location()
	if err != nil {
		return nil, fmt.Errorf("loading timezone %q: %w", cfg.Timezone, err)
	}
	if shouldBootstrapPromptOverlayDefaults(cfg) {
		if err := conversation.EnsurePromptOverlayDefaults(cfg.PromptOverlayPath); err != nil {
			logger.Warn("failed to bootstrap prompt overlay defaults", "path", cfg.PromptOverlayPath, "error", err)
		}
	}

	pref := tele.Settings{
		Token: cfg.TelegramToken,
	}

	tb, err := tele.NewBot(pref)
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}

	// Set up the configured chat client with retry.
	var client llm.Client
	if cfg.LLMAPIKey != "" {
		client = createLLMClient(cfg, logger)
	} else {
		logger.Warn("no LLM provider configured, bot will echo messages without LLM")
	}

	// Shared Qdrant client (D-17). Created once early so WaitForReady, search,
	// compact memory, and tool vector all use the same HTTP client pool.
	// When QDRANT_URL is empty, qdrantCli remains nil and all consumers skip Qdrant.
	var qdrantCli qdrant.Client
	if strings.TrimSpace(cfg.QdrantURL) != "" {
		qcli, err := qdrant.NewClient(qdrant.Config{
			BaseURL: cfg.QdrantURL,
			APIKey:  cfg.QdrantAPIKey,
		})
		if err != nil {
			logger.Warn("failed to create qdrant client; qdrant-dependent features disabled", "error", err)
		} else {
			// Qdrant startup health gate (D-20, QDRANT-01).
			// Block until Qdrant /readyz responds 2xx or 120s timeout expires.
			// Returns a clear diagnostic on failure (endpoint + elapsed time).
			healthCtx, healthCancel := context.WithTimeout(context.Background(), 120*time.Second)
			waitErr := qdrant.WaitForReady(healthCtx, qcli, 120*time.Second)
			healthCancel()
			if waitErr != nil {
				return nil, fmt.Errorf("qdrant health gate failed: %w", waitErr)
			}
			logger.Info("qdrant health gate passed", "url", cfg.QdrantURL)
			qdrantCli = qcli
		}
	}

	// Set up wiki store and writer
	wikiStore, err := wiki.NewStore(cfg.WikiPath, logger)
	if err != nil {
		return nil, fmt.Errorf("creating wiki store: %w", err)
	}

	// Migrate legacy .yaml pages to .md format
	if migrated, err := wikiStore.MigrateYAMLToMD(context.Background()); err != nil {
		logger.Warn("wiki migration failed", "error", err)
	} else if migrated > 0 {
		logger.Info("wiki migration completed", "pages_migrated", migrated)
	}
	wikiStore.RebuildGraph(context.Background())

	// Set up search engine
	var searchEngine search.Repository
	var embedFn search.EmbeddingFunction
	var batchEmbedFn search.BatchEmbeddingFunction
	var embedCache *search.EmbedCache
	if cfg.EmbeddingAPIKey != "" {
		embedFn = createEmbeddingFunc(cfg)
		batchEmbedFn = search.NewOpenAICompatBatchEmbeddingFunction(cfg.EmbeddingBaseURL, cfg.EmbeddingAPIKey, cfg.EmbeddingModel, cfg.EmbeddingOutputDim, nil)
		// Slice 11h: wrap the upstream embedding fn with a SHA-keyed
		// SQLite cache so unchanged wiki pages don't re-embed on every
		// restart. Same cache also serves query embeddings, so repeat
		// questions skip the Mistral round trip too.
		cacheNamespace := search.EmbedCacheNamespace(cfg.EmbeddingBaseURL, cfg.EmbeddingModel)
		cache, err := search.NewEmbedCacheWithBatchWithDB(pool, cacheNamespace, embedFn, batchEmbedFn, logger)
		if err != nil {
			logger.Warn("embed cache unavailable, falling back to uncached embedding", "error", err)
		} else {
			embedFn = cache.EmbedFunc()
			batchEmbedFn = cache.BatchEmbed
			embedCache = cache
		}
		if strings.TrimSpace(cfg.QdrantURL) == "" {
			logger.Warn("QDRANT_URL is required for wiki vector search; search disabled")
		} else {
			searchEngine, err = search.NewQdrantRepository(search.QdrantConfig{
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
			}
		}
	}

	// PHASE 2 INDEX-01: Construct reindex.Worker by capturing the searchEngine
	// LOCAL variable (search.Repository — which includes ReindexWikiPage via
	// WikiPageReindexer). BLOCKER 6 Option B of 2026-05-10 plan revision:
	// passing searchEngine directly avoids widening Bot.search from
	// search.Searcher to search.Repository. Worker is nil when searchEngine is
	// nil (no Qdrant configured), and nil is handled gracefully by
	// wikiStore.SetReindexSubmitter and tools.NewWikiPageTool.
	var reindexWorker *reindex.Worker
	if searchEngine != nil {
		reindexWorker = reindex.NewWorker(searchEngine, reindex.DefaultConfig())
	}

	// BLOCKER 3 of 2026-05-11 plan revision 2: SetReindexSubmitter lives only
	// on the concrete *wiki.Store, NOT on the wiki.Repository interface.
	// Use the local wikiStore variable — NEVER b.wiki for this setter call.
	if reindexWorker != nil && wikiStore != nil {
		wikiStore.SetReindexSubmitter(reindexWorker)
	}

	// Source store backs PDF uploads and OCR artifacts. Always create it —
	// even when OCR is off, the bot still stores raw PDFs as immutable
	// sources so a later /reocr can run.
	sourceStore, err := source.NewStore(cfg.WikiPath, logger)
	if err != nil {
		return nil, fmt.Errorf("creating source store: %w", err)
	}

	// OCR client is optional and auto-enables when MISTRAL_API_KEY is present.
	var ocrClient *ocr.Client
	if cfg.MistralAPIKey != "" {
		ocrClient = ocr.New(ocr.Config{
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

	// Markitdown sidecar client. Lazy-connects on first use, so an unreachable
	// sidecar does not block bot startup — uploads simply fail with a clear
	// error message until docker compose brings it back. Wave 2.9 replaces
	// the previous ExtractGo / Python sandbox extractor split with this one
	// converter for everything non-PDF.
	var markitdownClient markitdown.Converter
	if url := strings.TrimSpace(cfg.MarkitdownURL); url != "" {
		timeout := time.Duration(cfg.MarkitdownTimeoutSec) * time.Second
		markitdownClient = markitdown.New(markitdown.Config{BaseURL: url, Timeout: timeout})
		logger.Info("markitdown sidecar configured", "url", url, "timeout_sec", cfg.MarkitdownTimeoutSec)
	} else {
		logger.Warn("markitdown sidecar not configured (set MARKITDOWN_URL); non-PDF uploads will fail")
	}

	// Ingest pipeline (slice 6) compiles ocr_complete sources into wiki
	// summary pages. Wave 2.4 — when an LLM client is configured, the
	// pipeline ALSO runs entity/concept extraction (one structured-output
	// call per ingest) so a single source produces 1+N pages with
	// bidirectional [[wiki-links]] and `^[provenance]` markers. Without
	// an LLM the pipeline degrades to the legacy single-page-summary
	// behavior — no abort, no half-state.
	var ingestExtractor ingest.Extractor
	if client != nil {
		ingestExtractor = ingest.NewLLMExtractor(client, cfg.LLMModel)
	}
	ingestPipeline, err := ingest.New(ingest.Config{
		Sources:   sourceStore,
		Wiki:      wikiStore,
		Search:    searchEngine,
		Extractor: ingestExtractor,
		Logger:    logger,
	})
	if err != nil {
		return nil, fmt.Errorf("creating ingest pipeline: %w", err)
	}

	// Sandbox code execution. The tool is registered only after the configured
	// runtime passes availability checks.
	sandboxMgr, sandboxHealth := setupSandboxRuntime(cfg, logger)

	// Tool registry (persistent LLM-written Python tools)
	toolReg, err := tools.NewToolRegistry(wikiStore)
	if err != nil {
		logger.Warn("tool registry unavailable", "error", err)
	}

	toolRegistry := tools.NewRegistry(logger)
	if config.NormalizeWorkspaceTools(cfg.WorkspaceTools) == "enabled" {
		workspaceRoot, err := workspace.New(cfg.WorkspaceRoot)
		if err != nil {
			logger.Warn("workspace file tools unavailable", "root", cfg.WorkspaceRoot, "error", err)
		} else {
			// Wave 2.7e: unified file tool replaces list_files+read_file+
			// search_files+write_file+apply_patch.
			if fileTool := tools.NewFileTool(workspaceRoot); fileTool != nil {
				toolRegistry.Register(tools.WithCategory(fileTool, tools.CategoryAutonomous))
			}
			logger.Info("workspace file tools enabled", "root", workspaceRoot.Path())
		}
	} else {
		logger.Info("workspace file tools disabled (set AURA_WORKSPACE_TOOLS=enabled to enable)")
	}
	// Loader scans both the operator-curated SKILLS_PATH and the skills.sh
	// CLI install roots. The catalog CLI has used both `.claude/skills` and
	// `.agents/skills` layouts across versions/agents, so keep both visible.
	// In containers the install cwd is /workspace/skills, so catalog installs
	// persist under the mounted narrow runtime workspace.
	skillRoots := skillSearchRoots(cfg)
	skillLoader := auraskills.NewLoader(skillRoots[0], skillRoots[1:]...)
	skillsCatalog := auraskills.NewCatalogClient(cfg.SkillsCatalogURL)
	switch strings.ToLower(strings.TrimSpace(cfg.WebSearchProvider)) {
	case "searxng":
		// Wave 2.7c: unified web tool replaces web_search + web_fetch.
		toolRegistry.Register(tools.WithCategory(tools.NewWebTool(cfg.SearXNGBaseURL), tools.CategoryAutonomous))
	case "", "disabled":
		// Explicitly disabled.
	default:
		logger.Warn("unknown WEB_SEARCH_PROVIDER; web tools disabled", "provider", cfg.WebSearchProvider)
	}
	// Wave 2.7f: unified source tool replaces store_source / read_source /
	// list_sources / lint_sources / ocr_source / ingest_source / delete_source.
	// Registration is deferred to after memoryStore is built (line ~340)
	// because the unified tool's delete action needs the memoryindex purger.
	// Scheduler (slice 8). Persistent SQLite-backed task queue with one
	// goroutine ticking every DefaultTickInterval. Two task kinds ship:
	// reminder (delivered to the LLM-call's user via Telegram) and
	// wiki_maintenance (autonomous nightly pass). Three LLM tools wrap
	// the queue: schedule_task / list_tasks / cancel_task.
	schedStore, err := cron.NewStoreWithDB(pool)
	if err != nil {
		return nil, fmt.Errorf("creating scheduler store: %w", err)
	}
	// Wave 2.7b: unified task tool replaces schedule_task/list_tasks/cancel_task/run_task_now.
	// runner stays nil here; SetRunner wires *Bot below once it's constructed.
	taskTool := tools.NewTaskTool(schedStore, nil, loc)
	if taskTool != nil {
		toolRegistry.Register(taskTool)
	}
	summariesStore := summarizer.NewSummariesStore(schedStore.DB())
	var compactVector memoryindex.VectorIndex
	compactVectorHealth := memoryindex.NewVectorHealthTracker(false, "")
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
			compactVector = qindex
			logger.Info("compact qdrant memory mirror enabled", "url", cfg.QdrantURL, "collection", collection)
		}
	}
	memoryStore, err := memoryindex.NewStoreWithVector(schedStore.DB(), compactVector, logger)
	if err != nil {
		logger.Warn("compact memory index unavailable", "error", err)
	}
	// Wave 2.7f: register the unified source tool now that every dependency
	// is wired (sourceStore, ocrClient, ingestPipeline, memoryStore as the
	// delete-side purger). One action-enum tool replaces the previous seven.
	if sourceTool := tools.NewSourceTool(sourceStore, sourceStore, sourceStore, sourceStore, ocrClient, ingestPipeline, memoryStore); sourceTool != nil {
		toolRegistry.Register(tools.WithCategory(sourceTool, tools.CategoryAutonomous))
	}

	swarmStore, err := swarm.NewStoreWithDB(pool)
	if err != nil {
		return nil, fmt.Errorf("creating swarm store: %w", err)
	}
	timeoutSec := cfg.AuraBotTimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = config.DefaultAuraBotTimeoutSec
	}
	maxIterations := cfg.AuraBotMaxIterations
	if maxIterations <= 0 {
		maxIterations = 5
	}
	var auraRunner *agent.Runner
	if client != nil {
		auraRunner, err = agent.NewRunner(agent.Config{
			LLM:             client,
			Tools:           toolRegistry,
			Model:           cfg.LLMModel,
			MaxIterations:   maxIterations,
			Timeout:         time.Duration(timeoutSec) * time.Second,
			ToolTimeout:     time.Duration(timeoutSec) * time.Second,
			ReasoningEffort: cfg.ReasoningEffort,
			Logger:          logger,
			// Wave 2.8b: phantom-tool guard. Same wiring as the Telegram
			// bot loop (conversation.go). ToolNamesFn reads the live
			// registry so MCP additions are picked up without restart.
			PhantomToolGuard: &agent.PhantomToolGuard{
				ToolNamesFn: toolRegistry.Names,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("creating aurabot runner: %w", err)
		}
	}
	var swarmManager *swarm.Manager
	if cfg.AuraBotEnabled {
		if client == nil {
			logger.Warn("AuraBot swarm enabled but no LLM provider configured; swarm tools disabled")
		} else {
			swarmManager, err = swarm.NewManager(swarm.ManagerConfig{
				Runner:    auraRunner,
				Store:     swarmStore,
				MaxActive: cfg.AuraBotMaxActive,
				MaxDepth:  cfg.AuraBotMaxDepth,
				Logger:    logger,
			})
			if err != nil {
				return nil, fmt.Errorf("creating swarm manager: %w", err)
			}
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
			logger.Info("AuraBot swarm enabled", "max_active", cfg.AuraBotMaxActive, "max_depth", cfg.AuraBotMaxDepth, "timeout_sec", timeoutSec)
		}
	} else {
		logger.Info("AuraBot swarm disabled (set AURABOT_ENABLED=true to enable)")
	}

	// Slice 11a: MCP servers. Each configured server is contacted on
	// startup, its tools are discovered via tools/list and registered as
	// `mcp_<server>_<tool>` so the LLM can call them like native tools.
	// Connection failures are warned but never fatal — a flaky third-party
	// MCP server should not stop the bot.
	mcpServers, mcpErr := mcp.LoadServers(cfg.MCPServersPath)
	if mcpErr != nil {
		logger.Warn("MCP config load failed; continuing without MCP", "error", mcpErr, "path", cfg.MCPServersPath)
	}
	mcpClients := make([]mcp.ConnectedClient, 0, len(mcpServers))
	for name, srv := range mcpServers {
		srv.Env = mcp.NormalizeRuntimeEnv(name, srv.Env)
		var client *mcp.Client
		var err error
		switch {
		case srv.Command != "":
			client, err = mcp.NewStdioClient(name, srv.Command, srv.Args, srv.Env)
		case srv.URL != "":
			client, err = mcp.NewHTTPClient(name, srv.URL, srv.Headers)
		}
		if err != nil {
			logger.Warn("MCP server unavailable", "server", name, "error", err)
			continue
		}
		mcpClients = append(mcpClients, client)
		registeredTools := 0
		for _, t := range client.Tools() {
			if !mcp.ToolEnabledForAura(name, srv.Env, t.Name) {
				continue
			}
			tool := tools.NewMCPTool(client, name, t)
			if mcp.ToolAutonomousForAura(name, t.Name) {
				toolRegistry.Register(tools.WithCategory(tool, tools.CategoryAutonomous))
			} else {
				toolRegistry.Register(tool)
			}
			registeredTools++
		}
		logger.Info("MCP server registered", "server", name, "tools", len(client.Tools()), "aura_tools", registeredTools)
	}

	// Slice 10d: dashboard auth. Open the api_tokens table on the same
	// SQLite file the scheduler uses — saves a second file path config
	// and keeps everything backup-able as a single artifact.
	authStore, err := auth.NewStoreWithDB(pool)
	if err != nil {
		return nil, fmt.Errorf("creating auth store: %w", err)
	}
	authStore.SetTokenTTL(time.Duration(cfg.DashboardTokenTTLHours) * time.Hour)

	bgCtx, bgCancel := context.WithCancel(context.Background())
	b := &Bot{
		bot:                 tb,
		cfg:                 cfg,
		loc:                 loc,
		logger:              logger,
		llm:                 client,
		wiki:                wikiStore,
		search:              searchEngine,
		tools:               toolRegistry,
		sources:             sourceStore,
		ocr:                 ocrClient,
		skills:              skillLoader,
		bgCtx:               bgCtx,
		bgCancel:            bgCancel,
		schedDB:             schedStore,
		agentRunner:         auraRunner,
		swarmStore:          swarmStore,
		swarmMgr:            swarmManager,
		authDB:              authStore,
		mcpClients:          mcpClients,
		sandboxMgr:          sandboxMgr,
		toolReg:             toolReg,
		compactMemoryHealth: compactVectorHealth,
		budget: budget.NewTracker(budget.Config{
			SoftBudget:           cfg.SoftBudget,
			HardBudget:           cfg.HardBudget,
			InputCostPerMTokens:  cfg.CostInputPerMTokens,
			OutputCostPerMTokens: cfg.CostOutputPerMTokens,
		}, logger),
	}
	// Wire the reindex worker into the Bot struct after construction.
	// wikiStore.SetReindexSubmitter was already called above (before Bot was
	// built) so the same worker drives both the wiki store submitter and the
	// Bot.ReindexHealth accessor.
	b.reindex = reindexWorker

	// Create UserGate with real callbacks (D-16).
	// OnEvict: persists conversation snapshot and clears session (D-10, T-01-22).
	// OnOverflow: sends Telegram notice to user in a separate goroutine (D-03, T-01-20, Pitfall 4).
	// OnQueueNotice: sends still-processing notice when entry waits > InboxQueueNoticeAfter (CONC-01 gap).
	// The gate is created after b so callbacks can close over b safely.
	userGate := concurrency.New(concurrency.Config{
		InboxSize:         cfg.InboxSize,
		EvictionThreshold: cfg.InactivityThreshold,
		SweepInterval:     cfg.InactivitySweepInterval,
		QueueNoticeAfter:  cfg.InboxQueueNoticeAfter,
		OnEvict: func(userID string) {
			// Conversation snapshot is maintained by the actor goroutine as it runs;
			// eviction signals cleanup. Log only userID (numeric Telegram ID), not content (T-01-22).
			b.logger.Info("user evicted from gate; clearing session", "user_id", userID)
			// Clear the session so memory is released and IsActive returns false (T-01-23).
			b.sessionStore().Clear(userID)
		},
		OnOverflow: func(userID string) {
			// Telegram notice in a separate goroutine per Pitfall 4 (T-01-20).
			// The gate goroutine must never block on external I/O.
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

	_ = qdrantCli // shared client used by search/compact consumers above; no direct Bot field needed

	// Wave 2.7b: wire the run_now action of the unified task tool now that
	// *Bot (which implements ScheduledTaskRunner) is available.
	if taskTool != nil {
		taskTool.SetRunner(b)
	}

	// Sandbox tools
	if tool := tools.NewExecuteCodeToolWithStoreAndRegistry(sandboxMgr, b, sourceStore, toolRegistry); tool != nil {
		toolRegistry.Register(tool)
	}
	if tool := tools.NewExecuteShellTool(sandboxMgr); tool != nil {
		toolRegistry.Register(tool)
	}
	if tool := tools.NewDevToolTool(toolReg); tool != nil {
		toolRegistry.Register(tools.WithCategory(tool, tools.CategoryAutonomous))
	}

	// Deferred-tools rollout: tool_search is the always-on seed of the
	// per-turn agent pool. The model uses it to fetch input schemas for
	// tools advertised in the system-prompt manifest. Registered last so
	// it sees every other tool in Registry.Search().
	if tool := tools.NewToolSearchTool(toolRegistry); tool != nil {
		toolRegistry.Register(tool)
	}

	// Tool vector index backs the hybrid backend of Registry.Search, which
	// the LLM-callable tool_search tool delegates to. toolindex.Reconciler
	// is the source of truth for the embedding index (Wave 2.10.b+); this
	// path only wires the search-side reader.
	//
	// 2026-05-12 benchmark: embed cosine top-5 hits 90% Hit@5 on 64 labeled
	// queries vs 50% for BM25 alone — vector backend pulls real weight even
	// though mini-LLM rerankers proved unusable on CPU (p95 ≥ 1500ms).
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
	if qdrantCli != nil && embedCache != nil && cfg.ToolSearchBackend != "fts" {
		reconciler, err := toolindex.New(toolindex.Config{
			Provider:    toolRegistry,
			Qdrant:      qdrantCli,
			Embedder:    embedCache,
			State:       toolindex.NewSQLiteStateStore(pool),
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
			rep := reconciler.Reconcile(context.Background(), toolindex.ReasonBoot)
			logger.Info("tool index reconciled at boot",
				"upserted", len(rep.Upserted), "deleted", len(rep.Deleted),
				"unchanged", rep.Unchanged, "errors", len(rep.Errors),
				"elapsed_ms", rep.ElapsedMs)
			for i, e := range rep.Errors {
				logger.Warn("tool index reconcile error", "idx", i, "err", e)
			}
			b.toolReconciler = reconciler
			b.bgWg.Add(1)
			go func() {
				defer b.bgWg.Done()
				reconciler.Run(b.bgCtx)
			}()

			// fsnotify on mcp.json — debounced notification when the
			// operator edits the file. Wave 2.10.c will pair this with
			// MCP server reload; today the watcher just triggers a
			// reconcile pass so the operator sees confirmation in logs.
			if cfg.MCPServersPath != "" {
				watcher, werr := mcp.New(mcp.Config{
					Path:     cfg.MCPServersPath,
					Callback: func() { reconciler.Notify(toolindex.ReasonMCPConfig) },
					Logger:   logger.With("component", "mcpwatch"),
				})
				if werr != nil {
					logger.Warn("mcp.json watcher unavailable", "error", werr)
				} else {
					b.bgWg.Add(1)
					go func() {
						defer b.bgWg.Done()
						if rerr := watcher.Run(b.bgCtx); rerr != nil {
							logger.Warn("mcpwatch exited with error", "error", rerr)
						}
					}()
				}
			}
		}
	}

	// Wire the *ToolVectorIndex on the registry for the search path.
	// Writes are owned by toolindex.Reconciler; this just hooks up the
	// query-side client. If the Reconciler is not wired (qdrant or embed
	// cache unavailable) the reader degrades to fts via Ready() check.
	toolRegistry.PrepareVectorReader(toolReaderConfig)

	// Slice 12b/12c: conversation archive. Open the ArchiveStore on the same
	// SQLite file as the scheduler (migration is idempotent). Store the
	// archive repository for API reads, and wrap it with a BufferedAppender
	// for non-blocking writes from the hot conversation path.
	if cfg.ConvArchiveEnabled {
		archiveStore, err := conversation.NewArchiveStore(schedStore.DB())
		if err != nil {
			logger.Warn("conversation archive unavailable", "error", err)
		} else {
			if memoryStore != nil {
				b.archiveDB = memoryindex.NewIndexingArchiveRepository(archiveStore, memoryStore)
			} else {
				b.archiveDB = archiveStore
			}
			b.archiver = conversation.NewBufferedAppender(b.archiveDB, 100)
		}
	}
	if memoryStore != nil {
		rebuildCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		report, err := memoryindex.Rebuild(rebuildCtx, memoryStore, memoryindex.RebuildInput{
			Sources:    sourceStore,
			Archive:    b.archiveDB,
			Proposals:  summariesStore,
			SkipVector: compactVector != nil,
		})
		cancel()
		if err != nil {
			logger.Warn("compact memory index rebuild failed", "error", err)
		} else {
			logger.Info("compact memory index rebuilt", "sources", report.SourcesIndexed, "archive", report.ArchiveIndexed, "proposals", report.ProposalsIndexed, "vector_collection", report.Vector.Collection, "vector_docs", report.Vector.DocsIndexed)
		}
		if compactVector != nil {
			go func() {
				vectorCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
				defer cancel()
				logger.Info("compact memory vector mirror sync started")
				compactVectorHealth.Started()
				report, err := memoryStore.SyncVector(vectorCtx)
				if err != nil {
					compactVectorHealth.Failed(err)
					logger.Warn("compact memory vector mirror sync failed", "error", err)
					return
				}
				compactVectorHealth.Succeeded(report)
				logger.Info("compact memory vector mirror synced", "vector_collection", report.Collection, "vector_docs", report.DocsIndexed, "vector_size", report.VectorSize)
			}()
		}
	}
	if tool := tools.NewSearchMemoryToolConfigured(searchEngine, memoryStore,
		time.Duration(cfg.MemorySearchTimeoutMS)*time.Millisecond,
		cfg.RecencyHalfLifeWikiDays, cfg.RecencyHalfLifeArchiveDays); tool != nil {
		toolRegistry.Register(tool)
	}

	// Slice 12h/12l.1: shared wiki_issues store. Both the API maintenance
	// handlers and the nightly maintenance job read/write the same queue.
	b.issues = cron.NewIssuesStore(schedStore.DB())
	if tool := tools.NewDailyBriefingTool(schedStore, sourceStore, summariesStore, b.issues, b.archiveDB, loc); tool != nil {
		toolRegistry.Register(tool)
	}

	// Scheduler dispatcher closes over b so reminder/wiki_maintenance
	// tasks can invoke the bot's send + the wiki store. Built after b
	// is initialized.
	sched, err := cron.New(cron.Config{
		Store:      schedStore,
		Dispatcher: b.dispatchTask,
		Logger:     logger,
		Location:   loc,
	})
	if err != nil {
		return nil, fmt.Errorf("creating scheduler: %w", err)
	}
	b.sched = sched

	// Bootstrap the autonomous nightly wiki-maintenance task. Idempotent
	// upsert keyed by name so restarting the bot won't duplicate it. The
	// LLM can override the schedule with schedule_task using the same
	// name, or cancel it with cancel_task.
	nightlyAt, err := cron.NextDailyRun("03:00", loc, time.Now())
	if err != nil {
		return nil, fmt.Errorf("computing nightly run: %w", err)
	}
	if _, err := schedStore.Upsert(context.Background(), &cron.Task{
		Name:          "nightly-wiki-maintenance",
		Kind:          cron.KindWikiMaintenance,
		ScheduleKind:  cron.ScheduleDaily,
		ScheduleDaily: "03:00",
		NextRunAt:     nightlyAt,
		Status:        cron.StatusActive,
	}); err != nil {
		logger.Warn("failed to bootstrap nightly maintenance task", "err", err)
	}

	b.docs = newDocHandler(docHandlerConfig{
		Bot:        tb,
		Sources:    sourceStore,
		OCR:        ocrClient,
		Markitdown: markitdownClient,
		MaxFileMB:  cfg.OCRMaxFileMB,
		Allowlist:  b.isAllowlisted,
		Logger:     logger,
		// Slice 6: auto-ingest hook. Compile every freshly-OCR'd source
		// into a wiki summary page so the user sees a [[source-src-...]]
		// link in the final progress message instead of "ready for ingest".
		AfterOCR: ingestPipeline.AfterOCR,
	})

	// Slice 10a: build the read-only HTTP API. Mounted by main.go onto the
	// health server at /api/. The router is mount-agnostic — its routes are
	// /health, /wiki/..., /sources/..., /tasks/... — so callers wrap with
	// http.StripPrefix.
	// Slice 11c: skill install/delete adapters. Constructed unconditionally
	// — the SkillsAdmin gate is the actual write guard inside the api
	// package, so even when the gate is off we still wire the deps so
	// flipping SKILLS_ADMIN=true requires only a restart, not a rebuild.
	// Empty projectDir → installer falls back to os.Getwd() (the bot's
	// cwd at startup). Containers set
	// SKILLS_INSTALL_PROJECT_DIR=/workspace/skills so installs persist under
	// the mounted narrow runtime workspace skill trees.
	skillsInstaller, err := auraskills.NewNPXInstaller(cfg.SkillsPath, cfg.SkillsInstallProjectDir)
	if err != nil {
		logger.Warn("skills installer unavailable", "error", err)
	}
	// Deleter mirrors the loader's roots so catalog-installed skills are
	// deletable too.
	skillsDeleter, err := auraskills.NewFSDeleter(skillRoots[0], skillRoots[1:]...)
	if err != nil {
		logger.Warn("skills deleter unavailable", "error", err)
	}
	skillProposalApplier, err := auraskills.NewFSProposalApplier(skillRoots[0])
	if err != nil {
		logger.Warn("skill proposal applier unavailable", "error", err)
	}

	b.api = api.NewRouter(api.Deps{
		Wiki:        wikiStore,
		Sources:     sourceStore,
		Scheduler:   schedStore,
		OCR:         ocrClient,
		Ingest:      ingestPipeline,
		Markitdown:  markitdownClient,
		Auth:        authStore,
		Allowlist:   b.isAllowlisted,
		MaxUploadMB: cfg.OCRMaxFileMB,
		Location:    loc,
		// PHASE 2 WARNING 12: surface reindex worker health via /api/health.
		ReindexHealth: b.ReindexHealth,
		// Keep in sync with cmd/aura/main.go's auraVersion. Hardcoded
		// here because cmd/aura is not importable.
		Version:   "3.0",
		StartedAt: time.Now().UTC(),
		Logger:    logger,
		// Slice 11b: skills + MCP dashboard panels read off these.
		Skills: skillLoader,
		MCP:    mcpClients,
		// Slice 11c: skills.sh catalog + admin-gated install/delete.
		SkillsCatalog:   skillsCatalog,
		SkillsInstaller: skillsInstaller,
		SkillsDeleter:   skillsDeleterAdapter{inner: skillsDeleter},
		SkillProposals:  skillProposalApplierAdapter{inner: skillProposalApplier},
		SkillsAdmin:     cfg.SkillsAdmin,
		// Slice 11j: surface cache hit/miss counters in /api/health.
		EmbedCache:    embedCache,
		CompactMemory: compactVectorHealth,
		Sandbox:       sandboxHealth,
		// ToolReconciler backs POST /api/tools/reindex and is notified
		// on skill install/delete success (Wave 2.10.b). Nil when
		// QDRANT_URL or EMBEDDING_API_KEY is unset; the reindex
		// endpoint returns 503 in that case.
		ToolReconciler: b.toolReconciler,
		// SourcePurger lets DELETE /sources/{id} clean up the compact
		// memoryindex (and its Qdrant mirror) after removing the raw
		// files. Same store as the delete_source LLM tool.
		SourcePurger: memoryStore,
		// Multi-root file manager (dashboard).
		WikiDir:      cfg.WikiPath,
		WorkspaceDir: cfg.WorkspaceRoot,
		SkillsDir:    cfg.SkillsPath,
		// Reindex hook: wiki writes/renames/deletes trigger Qdrant
		// reindex so the LLM never reads stale embeddings.
		WikiSearch: searchEngine,
		// Pending-approval pipeline. Bot owns the side-effects (DB
		// transition + Telegram delivery), so the api package just sees
		// the interface.
		PendingApprover: b,
		// Slice 12c: conversation archive read API.
		Archive: b.archiveDB,
		// Slice 12k.1: summaries review queue.
		Summaries:     summariesStore,
		SummariesWiki: wikiStore,
		// Slice 12l.1: wiki maintenance issue queue (shared with the
		// nightly maintenance dispatch).
		Issues: b.issues,
		// Slice 14d: runtime settings page surface.
		Settings:      settingsStore,
		RuntimeConfig: cfg,
		ApplyRuntimeSettings: func(ctx context.Context) error {
			return applyRuntimeSettings(ctx, settingsStore, cfg, auraRunner, swarmManager, b.budget, logger)
		},
		Restart: opt.Restart,
		// Slice 17d: AuraBot swarm observability.
		Swarm: swarmStore,
		Chat:  NewWebChatService(auraRunner, toolRegistry),
	})

	// Slice 10d: request_dashboard_token tool. Registered after b is
	// constructed so the bot can satisfy tools.TokenSender via its own
	// SendToUser method. The tool delivers the freshly-minted token over
	// Telegram (not through the LLM result) so the plaintext stays out
	// of conversation history.
	if tokenTool := tools.NewRequestDashboardTokenTool(authStore, b, b.isAllowlisted); tokenTool != nil {
		toolRegistry.Register(tokenTool)
	}

	// Wave 2.7d: unified doc tool replaces create_xlsx + create_docx + create_pdf.
	// Same post-construction registration pattern as request_dashboard_token —
	// bot satisfies tools.DocumentSender via its own SendDocumentToUser method,
	// so we wait until b exists.
	if docTool := tools.NewDocTool(sourceStore, b); docTool != nil {
		toolRegistry.Register(docTool)
	}
	// Wave 2.6 — register the unified wiki_page tool. Replaces the legacy
	// write_wiki_page with a four-action verb family (create/replace/edit/append).
	// Uses the local wikiStore (*wiki.Store) — not b.wiki (wiki.Repository
	// interface) — consistent with BLOCKER 3 of 2026-05-11 plan revision 2.
	// reindexWorker is the same worker wired into wikiStore.SetReindexSubmitter
	// above; nil is safe.
	if t := tools.NewWikiPageTool(wikiStore, reindexWorker); t != nil {
		toolRegistry.Register(t)
	}

	b.registerHandlers()
	b.installBotCommands()

	// Wave 2.10.b — late notify. The early boot Reconcile ran before
	// some tools were registered (search_memory, doc, daily_briefing,
	// wiki_page, request_dashboard_token, and more wire after b.api +
	// after the dependency-graph resolution finishes). Fire one final
	// Notify here so the debounced reconcile picks up every tool that
	// landed in the registry during setup. No-op when the Reconciler
	// isn't wired (QDRANT_URL / EMBEDDING_API_KEY missing).
	if b.toolReconciler != nil {
		b.toolReconciler.Notify(toolindex.ReasonBoot)
	}
	return b, nil
}
