package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/auth"
	"github.com/aura/aura/internal/budget"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/ingest"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/mcp"
	"github.com/aura/aura/internal/ocr"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/search"
	"github.com/aura/aura/internal/settings"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/swarm"
	"github.com/aura/aura/internal/swarmtools"
	"github.com/aura/aura/internal/tools"
	"github.com/aura/aura/internal/wiki"

	"github.com/philippgille/chromem-go"
	tele "gopkg.in/telebot.v4"
)

// New creates a new Telegram bot with allowlist enforcement and LLM integration.
//
// settingsStore is the runtime configuration store opened by main.go on
// cfg.DBPath. It's threaded through so the dashboard's /settings page
// can persist edits without re-opening the SQLite file. May be nil
// (tests) — in that case the dashboard /settings endpoints respond 503.
func New(cfg *config.Config, settingsStore *settings.Store, logger *slog.Logger) (*Bot, error) {
	pref := tele.Settings{
		Token: cfg.TelegramToken,
	}

	tb, err := tele.NewBot(pref)
	if err != nil {
		return nil, fmt.Errorf("creating telegram bot: %w", err)
	}

	// Set up LLM client with retry and failover
	var client llm.Client
	if cfg.LLMAPIKey != "" || cfg.OllamaBaseURL != "" {
		client = createLLMClient(cfg, logger)
	} else {
		logger.Warn("no LLM provider configured, bot will echo messages without LLM")
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

	// Set up search engine
	var searchEngine *search.Engine
	var embedCache *search.EmbedCache
	if cfg.EmbeddingAPIKey != "" {
		embedFn := createEmbeddingFunc(cfg)
		// Slice 11h: wrap the upstream embedding fn with a SHA-keyed
		// SQLite cache so unchanged wiki pages don't re-embed on every
		// restart. Same cache also serves query embeddings, so repeat
		// questions skip the Mistral round trip too.
		if cfg.DBPath != "" {
			cache, err := search.OpenEmbedCache(cfg.DBPath, cfg.EmbeddingModel, embedFn, logger)
			if err != nil {
				logger.Warn("embed cache unavailable, falling back to uncached embedding", "error", err)
			} else {
				embedFn = cache.EmbedFunc()
				embedCache = cache
			}
		}
		var se *search.Engine
		var err error
		if cfg.DBPath != "" {
			se, err = search.NewEngineWithFallback(cfg.WikiPath, embedFn, cfg.DBPath, logger)
		} else {
			se, err = search.NewEngine(cfg.WikiPath, embedFn, logger)
		}
		if err != nil {
			logger.Warn("failed to create search engine, search disabled", "error", err)
		} else {
			// Index existing wiki pages on startup
			if err := se.IndexWikiPages(context.Background()); err != nil {
				logger.Warn("failed to index wiki pages on startup", "error", err)
			}
			searchEngine = se
		}
	}

	// Source store backs PDF uploads and OCR artifacts. Always create it —
	// even when OCR is off, the bot still stores raw PDFs as immutable
	// sources so a later /reocr can run.
	sourceStore, err := source.NewStore(cfg.WikiPath, logger)
	if err != nil {
		return nil, fmt.Errorf("creating source store: %w", err)
	}

	// OCR client is optional. Required env: MISTRAL_API_KEY + OCR_ENABLED.
	var ocrClient *ocr.Client
	if cfg.OCREnabled && cfg.MistralAPIKey != "" {
		ocrClient = ocr.New(ocr.Config{
			APIKey:        cfg.MistralAPIKey,
			BaseURL:       cfg.MistralOCRBaseURL,
			Model:         cfg.MistralOCRModel,
			TableFormat:   cfg.MistralOCRTableFormat,
			ExtractHeader: cfg.MistralOCRExtractHeader,
			ExtractFooter: cfg.MistralOCRExtractFooter,
		})
	} else {
		logger.Info("OCR disabled (set OCR_ENABLED=true and MISTRAL_API_KEY to enable)")
	}

	// Ingest pipeline (slice 6) compiles ocr_complete sources into wiki
	// summary pages. Always built — it has no external deps beyond the
	// source and wiki stores, both of which are already present.
	ingestPipeline, err := ingest.New(ingest.Config{
		Sources: sourceStore,
		Wiki:    wikiStore,
		Search:  searchEngine,
		Logger:  logger,
	})
	if err != nil {
		return nil, fmt.Errorf("creating ingest pipeline: %w", err)
	}

	toolRegistry := tools.NewRegistry(logger)
	// Loader scans both the operator-curated SKILLS_PATH (./skills by
	// default) and `.claude/skills`, where `npx skills add … --agent
	// claude-code` writes catalog installs. Catalog flow stays
	// transparent: install via dashboard → file lands in .claude/skills
	// → loader picks it up on the next chat turn / dashboard refresh.
	skillLoader := auraskills.NewLoader(cfg.SkillsPath, ".claude/skills")
	skillsCatalog := auraskills.NewCatalogClient(cfg.SkillsCatalogURL)
	toolRegistry.Register(tools.NewSearchSkillCatalogTool(skillsCatalog))
	toolRegistry.Register(tools.NewListSkillsTool(skillLoader))
	toolRegistry.Register(tools.NewReadSkillTool(skillLoader))
	// web_search / web_fetch are backed by Ollama's hosted web API, which
	// authenticates with the same credential as Ollama Cloud's chat API.
	// If the operator only set LLM_API_KEY (because Ollama Cloud is their
	// chat provider too) we transparently reuse it for the web tools, so
	// the model doesn't have to truthfully report "no web search" when in
	// fact a single key would have unlocked both surfaces.
	ollamaWebKey := cfg.OllamaAPIKey
	if ollamaWebKey == "" && strings.Contains(cfg.LLMBaseURL, "ollama.com") {
		ollamaWebKey = cfg.LLMAPIKey
	}
	if ollamaWebKey != "" {
		toolRegistry.Register(tools.NewWebSearchTool(ollamaWebKey, cfg.OllamaWebBaseURL))
		toolRegistry.Register(tools.NewWebFetchTool(ollamaWebKey, cfg.OllamaWebBaseURL))
	}
	toolRegistry.Register(tools.NewWriteWikiTool(wikiStore, searchEngine))
	toolRegistry.Register(tools.NewReadWikiTool(wikiStore))
	if searchEngine != nil {
		toolRegistry.Register(tools.NewSearchWikiTool(searchEngine))
	}
	// Source tools (slice 5). store_source/read_source/list_sources/lint_sources
	// are always registered. ocr_source only when OCR is configured — otherwise
	// the LLM gets a clearer "OCR disabled" error from the tool itself, which
	// is better than tempting it to call a tool we can never satisfy.
	toolRegistry.Register(tools.NewStoreSourceTool(sourceStore))
	toolRegistry.Register(tools.NewReadSourceTool(sourceStore))
	toolRegistry.Register(tools.NewListSourcesTool(sourceStore))
	toolRegistry.Register(tools.NewLintSourcesTool(sourceStore))
	if ocrClient != nil {
		toolRegistry.Register(tools.NewOCRSourceTool(sourceStore, ocrClient))
	}
	toolRegistry.Register(tools.NewIngestSourceTool(ingestPipeline))
	// Wiki maintenance (slice 7). list_wiki/lint_wiki give the LLM
	// introspection over the page catalog; rebuild_index/append_log are
	// the explicit knobs that bypass the auto-maintained side files.
	toolRegistry.Register(tools.NewListWikiTool(wikiStore))
	toolRegistry.Register(tools.NewLintWikiTool(wikiStore))
	toolRegistry.Register(tools.NewRebuildIndexTool(wikiStore))
	toolRegistry.Register(tools.NewAppendLogTool(wikiStore))

	// Scheduler (slice 8). Persistent SQLite-backed task queue with one
	// goroutine ticking every DefaultTickInterval. Two task kinds ship:
	// reminder (delivered to the LLM-call's user via Telegram) and
	// wiki_maintenance (autonomous nightly pass). Three LLM tools wrap
	// the queue: schedule_task / list_tasks / cancel_task.
	schedDBPath := cfg.DBPath
	if schedDBPath == "" {
		schedDBPath = "./aura.db"
	}
	schedStore, err := scheduler.OpenStore(schedDBPath)
	if err != nil {
		return nil, fmt.Errorf("creating scheduler store: %w", err)
	}
	toolRegistry.Register(tools.NewScheduleTaskTool(schedStore, time.Local))
	toolRegistry.Register(tools.NewListTasksTool(schedStore))
	toolRegistry.Register(tools.NewCancelTaskTool(schedStore))
	summariesStore := summarizer.NewSummariesStore(schedStore.DB())
	if tool := tools.NewProposeWikiChangeTool(summariesStore); tool != nil {
		toolRegistry.Register(tool)
	}

	swarmStore, err := swarm.NewStoreWithDB(schedStore.DB())
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
			LLM:           client,
			Tools:         toolRegistry,
			Model:         cfg.LLMModel,
			MaxIterations: maxIterations,
			Timeout:       time.Duration(timeoutSec) * time.Second,
			ToolTimeout:   time.Duration(timeoutSec) * time.Second,
			Logger:        logger,
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
	mcpClients := make([]*mcp.Client, 0, len(mcpServers))
	for name, srv := range mcpServers {
		var client *mcp.Client
		var err error
		switch {
		case srv.Command != "":
			client, err = mcp.NewStdioClient(name, srv.Command, srv.Args)
		case srv.URL != "":
			client, err = mcp.NewHTTPClient(name, srv.URL, srv.Headers)
		}
		if err != nil {
			logger.Warn("MCP server unavailable", "server", name, "error", err)
			continue
		}
		mcpClients = append(mcpClients, client)
		for _, t := range client.Tools() {
			toolRegistry.Register(tools.NewMCPTool(client, name, t))
		}
		logger.Info("MCP server registered", "server", name, "tools", len(client.Tools()))
	}

	// Slice 10d: dashboard auth. Open the api_tokens table on the same
	// SQLite file the scheduler uses — saves a second file path config
	// and keeps everything backup-able as a single artifact.
	authStore, err := auth.OpenStore(schedDBPath)
	if err != nil {
		return nil, fmt.Errorf("creating auth store: %w", err)
	}

	b := &Bot{
		bot:         tb,
		cfg:         cfg,
		logger:      logger,
		llm:         client,
		wiki:        wikiStore,
		search:      searchEngine,
		tools:       toolRegistry,
		sources:     sourceStore,
		ocr:         ocrClient,
		skills:      skillLoader,
		schedDB:     schedStore,
		agentRunner: auraRunner,
		swarmStore:  swarmStore,
		swarmMgr:    swarmManager,
		authDB:      authStore,
		mcpClients:  mcpClients,
		budget: budget.NewTracker(budget.Config{
			SoftBudget:   cfg.SoftBudget,
			HardBudget:   cfg.HardBudget,
			CostPerToken: cfg.CostPerToken,
		}, logger),
	}
	if tool := tools.NewRunTaskNowTool(b); tool != nil {
		toolRegistry.Register(tool)
	}

	// Slice 12b/12c: conversation archive. Open the ArchiveStore on the same
	// SQLite file as the scheduler (migration is idempotent). Store the
	// *ArchiveStore directly for API reads, and wrap with a BufferedAppender
	// for non-blocking writes from the hot conversation path.
	if cfg.ConvArchiveEnabled {
		archiveStore, err := conversation.NewArchiveStore(schedStore.DB())
		if err != nil {
			logger.Warn("conversation archive unavailable", "error", err)
		} else {
			b.archiveDB = archiveStore
			b.archiver = conversation.NewBufferedAppender(archiveStore, 100)
		}
	}
	if tool := tools.NewSearchMemoryTool(searchEngine, sourceStore, b.archiveDB); tool != nil {
		toolRegistry.Register(tool)
	}

	// Slice 12h/12l.1: shared wiki_issues store. Both the API maintenance
	// handlers and the nightly maintenance job read/write the same queue.
	b.issues = scheduler.NewIssuesStore(schedStore.DB())
	if tool := tools.NewDailyBriefingTool(schedStore, sourceStore, summariesStore, b.issues, b.archiveDB, time.Local); tool != nil {
		toolRegistry.Register(tool)
	}

	// Slice 12e/12f: summarizer runner with real deduper + mode-based applier.
	if cfg.SummarizerEnabled && b.archiveDB != nil && client != nil {
		sc := summarizer.NewScorer(client, cfg.LLMModel, cfg.SummarizerMinSalience)
		// Use real wiki search engine if available, fall back to noop.
		var ws summarizer.WikiSearcher = noopWikiSearcher{}
		if searchEngine != nil {
			ws = searchEngine
		}
		dd := summarizer.NewDeduper(ws, 0.85, 0.5)
		var applier summarizer.Applier
		switch cfg.SummarizerMode {
		case "auto":
			applier = summarizer.NewAutoApplier(wikiStore)
		case "review":
			ra, err := summarizer.NewReviewApplier(schedStore.DB())
			if err != nil {
				logger.Warn("review applier unavailable", "error", err)
			} else {
				applier = ra
			}
		default:
			applier = summarizer.NewOffApplier()
		}
		b.summRunner = summarizer.NewRunner(summarizer.RunnerConfig{
			Enabled:       true,
			TurnInterval:  cfg.SummarizerTurnInterval,
			LookbackTurns: cfg.SummarizerLookbackTurns,
			CooldownSecs:  cfg.SummarizerCooldownSeconds,
			Applier:       applier,
		}, b.archiveDB, sc, dd)
	}

	// Scheduler dispatcher closes over b so reminder/wiki_maintenance
	// tasks can invoke the bot's send + the wiki store. Built after b
	// is initialized.
	sched, err := scheduler.New(scheduler.Config{
		Store:      schedStore,
		Dispatcher: b.dispatchTask,
		Logger:     logger,
		Location:   time.Local,
	})
	if err != nil {
		return nil, fmt.Errorf("creating scheduler: %w", err)
	}
	b.sched = sched

	// Bootstrap the autonomous nightly wiki-maintenance task. Idempotent
	// upsert keyed by name so restarting the bot won't duplicate it. The
	// LLM can override the schedule with schedule_task using the same
	// name, or cancel it with cancel_task.
	nightlyAt, err := scheduler.NextDailyRun("03:00", time.Local, time.Now())
	if err != nil {
		return nil, fmt.Errorf("computing nightly run: %w", err)
	}
	if _, err := schedStore.Upsert(context.Background(), &scheduler.Task{
		Name:          "nightly-wiki-maintenance",
		Kind:          scheduler.KindWikiMaintenance,
		ScheduleKind:  scheduler.ScheduleDaily,
		ScheduleDaily: "03:00",
		NextRunAt:     nightlyAt,
		Status:        scheduler.StatusActive,
	}); err != nil {
		logger.Warn("failed to bootstrap nightly maintenance task", "err", err)
	}

	b.docs = newDocHandler(docHandlerConfig{
		Bot:       tb,
		Sources:   sourceStore,
		OCR:       ocrClient,
		MaxFileMB: cfg.OCRMaxFileMB,
		Allowlist: b.isAllowlisted,
		Logger:    logger,
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
	// cwd at startup), which is the project root for any standard layout.
	// Prevents the regression where cwd=cfg.SkillsPath caused skills to
	// nest under skills/.claude/skills/ instead of <project>/.claude/skills/.
	skillsInstaller, err := auraskills.NewNPXInstaller(cfg.SkillsPath, "")
	if err != nil {
		logger.Warn("skills installer unavailable", "error", err)
	}
	// Deleter mirrors the loader's roots so catalog-installed skills
	// (in .claude/skills) are deletable too.
	skillsDeleter, err := auraskills.NewFSDeleter(cfg.SkillsPath, ".claude/skills")
	if err != nil {
		logger.Warn("skills deleter unavailable", "error", err)
	}

	b.api = api.NewRouter(api.Deps{
		Wiki:        wikiStore,
		Sources:     sourceStore,
		Scheduler:   schedStore,
		OCR:         ocrClient,
		Ingest:      ingestPipeline,
		Auth:        authStore,
		Allowlist:   b.isAllowlisted,
		MaxUploadMB: cfg.OCRMaxFileMB,
		Location:    time.Local,
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
		SkillsAdmin:     cfg.SkillsAdmin,
		// Slice 11j: surface cache hit/miss counters in /api/health.
		EmbedCache: embedCache,
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
		// Slice 17d: AuraBot swarm observability.
		Swarm: swarmStore,
	})

	// Slice 10d: request_dashboard_token tool. Registered after b is
	// constructed so the bot can satisfy tools.TokenSender via its own
	// SendToUser method. The tool delivers the freshly-minted token over
	// Telegram (not through the LLM result) so the plaintext stays out
	// of conversation history.
	if tokenTool := tools.NewRequestDashboardTokenTool(authStore, b, b.isAllowlisted); tokenTool != nil {
		toolRegistry.Register(tokenTool)
	}

	// Slice 15a: create_xlsx tool. Same post-construction registration
	// pattern as request_dashboard_token — bot satisfies tools.DocumentSender
	// via its own SendDocumentToUser method, so we wait until b exists.
	if xlsxTool := tools.NewCreateXLSXTool(sourceStore, b); xlsxTool != nil {
		toolRegistry.Register(xlsxTool)
	}

	// Slice 15b: create_docx tool. Same wiring as create_xlsx.
	if docxTool := tools.NewCreateDOCXTool(sourceStore, b); docxTool != nil {
		toolRegistry.Register(docxTool)
	}

	// Slice 15c: create_pdf tool. Same wiring as create_xlsx / create_docx.
	if pdfTool := tools.NewCreatePDFTool(sourceStore, b); pdfTool != nil {
		toolRegistry.Register(pdfTool)
	}

	b.registerHandlers()
	return b, nil
}

// createLLMClient builds the LLM client chain with failover:
// 1. OpenAI-compatible (primary) -> Ollama (offline fallback)
// Each provider is wrapped with retry logic.
func createLLMClient(cfg *config.Config, logger *slog.Logger) llm.Client {
	var providers []llm.Client
	var names []string

	if cfg.LLMAPIKey != "" {
		openaiClient := llm.NewOpenAIClient(llm.OpenAIConfig{
			APIKey:  cfg.LLMAPIKey,
			BaseURL: cfg.LLMBaseURL,
			Model:   cfg.LLMModel,
		})
		retryClient := llm.NewRetryClient(openaiClient, llm.RetryConfig{
			MaxRetries: cfg.LLMMaxRetries,
			BaseDelay:  time.Second,
			MaxDelay:   30 * time.Second,
		})
		providers = append(providers, retryClient)
		names = append(names, "openai")
	}

	if cfg.OllamaBaseURL != "" {
		ollamaClient := llm.NewOllamaClient(llm.OllamaConfig{
			BaseURL: cfg.OllamaBaseURL,
			Model:   cfg.OllamaModel,
		})
		retryClient := llm.NewRetryClient(ollamaClient, llm.RetryConfig{
			MaxRetries: 2,
			BaseDelay:  time.Second,
			MaxDelay:   10 * time.Second,
		})
		providers = append(providers, retryClient)
		names = append(names, "ollama")
	}

	if len(providers) == 1 {
		return providers[0]
	}

	failover, err := llm.NewFailoverClient(providers, names)
	if err != nil {
		logger.Error("failed to create failover client, using first provider", "error", err)
		return providers[0]
	}
	return failover
}

// createEmbeddingFunc builds a chromem embedding function from the dedicated
// embedding provider config. Aura keeps embeddings separate from LLM chat keys.
func createEmbeddingFunc(cfg *config.Config) chromem.EmbeddingFunc {
	baseURL := cfg.EmbeddingBaseURL
	model := cfg.EmbeddingModel

	normalized := true
	return chromem.NewEmbeddingFuncOpenAICompat(baseURL, cfg.EmbeddingAPIKey, model, &normalized)
}

// noopWikiSearcher satisfies summarizer.WikiSearcher with an always-empty result.
// Used when the search engine is not yet wired (slice 12e); 12f replaces it.
type noopWikiSearcher struct{}

func (noopWikiSearcher) Search(_ context.Context, _ string, _ int) ([]search.Result, error) {
	return nil, nil
}
