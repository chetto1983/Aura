package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

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
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/tools"
	"github.com/aura/aura/internal/wiki"

	"github.com/philippgille/chromem-go"
	tele "gopkg.in/telebot.v4"
)

// Bot wraps the telebot instance with allowlist access control and LLM integration.
type Bot struct {
	bot        *tele.Bot
	cfg        *config.Config
	logger     *slog.Logger
	llm        llm.Client
	wiki       *wiki.Store
	search     *search.Engine
	tools      *tools.Registry
	budget     *budget.Tracker
	sources    *source.Store
	ocr        *ocr.Client
	skills     *auraskills.Loader
	docs       *docHandler
	sched      *scheduler.Scheduler
	schedDB    *scheduler.Store
	authDB     *auth.Store                    // dashboard bearer-token store (slice 10d)
	mcpClients []*mcp.Client                  // active MCP server connections (slice 11a)
	archiveDB  *conversation.ArchiveStore     // nil when CONV_ARCHIVE_ENABLED=false
	archiver   *conversation.BufferedAppender // nil when CONV_ARCHIVE_ENABLED=false
	summRunner *summarizer.Runner             // nil when SUMMARIZER_ENABLED=false
	api        http.Handler                   // read-only JSON API for the dashboard, mounted on the health server
	active     sync.Map                       // maps userID string -> bool (active conversation tracking)
	ctxMap     sync.Map                       // maps userID string -> *conversation.Context
}

// New creates a new Telegram bot with allowlist enforcement and LLM integration.
func New(cfg *config.Config, logger *slog.Logger) (*Bot, error) {
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
		bot:        tb,
		cfg:        cfg,
		logger:     logger,
		llm:        client,
		wiki:       wikiStore,
		search:     searchEngine,
		tools:      toolRegistry,
		sources:    sourceStore,
		ocr:        ocrClient,
		skills:     skillLoader,
		schedDB:    schedStore,
		authDB:     authStore,
		mcpClients: mcpClients,
		budget: budget.NewTracker(budget.Config{
			SoftBudget:   cfg.SoftBudget,
			HardBudget:   cfg.HardBudget,
			CostPerToken: cfg.CostPerToken,
		}, logger),
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
		Summaries:     summarizer.NewSummariesStore(schedStore.DB()),
		SummariesWiki: wikiStore,
	})

	// Slice 10d: request_dashboard_token tool. Registered after b is
	// constructed so the bot can satisfy tools.TokenSender via its own
	// SendToUser method. The tool delivers the freshly-minted token over
	// Telegram (not through the LLM result) so the plaintext stays out
	// of conversation history.
	if tokenTool := tools.NewRequestDashboardTokenTool(authStore, b, b.isAllowlisted); tokenTool != nil {
		toolRegistry.Register(tokenTool)
	}

	b.registerHandlers()
	return b, nil
}

// Username returns the bot's Telegram username.
func (b *Bot) Username() string {
	return b.bot.Me.Username
}

// APIHandler returns the read-only JSON dashboard API. Caller is expected
// to wrap with http.StripPrefix and mount under /api/ on the health server.
func (b *Bot) APIHandler() http.Handler { return b.api }

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

// Start begins polling for Telegram messages and the scheduler tick loop.
func (b *Bot) Start() {
	b.logger.Info("telegram bot started")
	if b.sched != nil {
		b.sched.Start(context.Background())
	}
	b.bot.Start()
}

// Stop gracefully stops the bot and the scheduler.
func (b *Bot) Stop() {
	if b.archiver != nil {
		_ = b.archiver.Close(context.Background())
	}
	if b.sched != nil {
		b.sched.Stop()
	}
	if b.schedDB != nil {
		_ = b.schedDB.Close()
	}
	if b.authDB != nil {
		_ = b.authDB.Close()
	}
	for _, c := range b.mcpClients {
		_ = c.Close()
	}
	b.bot.Stop()
}

// skillsDeleterAdapter bridges auraskills.FSDeleter (which surfaces a
// package-internal not-found sentinel) to api.SkillDeleter (which
// expects api.ErrSkillNotFound for 404 routing). Keeping the cycle out
// of the two packages costs us this 8-line shim.
type skillsDeleterAdapter struct {
	inner *auraskills.FSDeleter
}

func (a skillsDeleterAdapter) Delete(name string) error {
	if a.inner == nil {
		return api.ErrSkillNotFound
	}
	if err := a.inner.Delete(name); err != nil {
		if auraskills.IsSkillNotFound(err) {
			return api.ErrSkillNotFound
		}
		return err
	}
	return nil
}

// dispatchTask is the scheduler.Dispatcher implementation. It routes a
// fired task to the right side-effect: reminders go to Telegram via the
// stored RecipientID, wiki_maintenance runs the autonomous pass.
// Errors are returned so the scheduler records last_error; the row is
// always persisted regardless of outcome so the LLM can introspect.
func (b *Bot) dispatchTask(ctx context.Context, task *scheduler.Task) error {
	switch task.Kind {
	case scheduler.KindReminder:
		return b.dispatchReminder(task)
	case scheduler.KindWikiMaintenance:
		return b.dispatchWikiMaintenance(ctx)
	default:
		return fmt.Errorf("dispatchTask: unknown kind %q", task.Kind)
	}
}

func (b *Bot) dispatchReminder(task *scheduler.Task) error {
	if task.RecipientID == "" {
		return fmt.Errorf("reminder %q has no recipient", task.Name)
	}
	chatID, err := strconv.ParseInt(task.RecipientID, 10, 64)
	if err != nil {
		return fmt.Errorf("parse recipient %q: %w", task.RecipientID, err)
	}
	body := task.Payload
	if body == "" {
		body = "Reminder: " + task.Name
	} else {
		body = "⏰ " + body
	}
	if _, err := b.bot.Send(tele.ChatID(chatID), body); err != nil {
		return fmt.Errorf("send reminder: %w", err)
	}
	return nil
}

// dispatchWikiMaintenance runs the autonomous nightly wiki pass via
// MaintenanceJob: rebuilds index, lints, auto-fixes single-candidate
// broken links (Levenshtein ≤ 2), and defers the rest to 12h.
func (b *Bot) dispatchWikiMaintenance(ctx context.Context) error {
	if b.wiki == nil {
		return fmt.Errorf("wiki maintenance: wiki store unavailable")
	}
	b.wiki.RebuildIndex(ctx)
	job := scheduler.NewMaintenanceJob(b.wiki, b.logger).
		WithIssuesStore(scheduler.NewIssuesStore(b.schedDB.DB())).
		WithOwnerNotifier(func(ctx context.Context, msg string) {
			for _, ownerID := range b.collectOwnerIDs() {
				if err := b.SendToUser(ownerID, msg); err != nil {
					b.logger.Warn("maintenance notify failed", "owner", ownerID, "error", err)
				}
			}
		})
	fixed, deferred, err := job.Run(ctx)
	if err != nil {
		return fmt.Errorf("wiki maintenance: %w", err)
	}
	b.wiki.AppendLog(ctx, "nightly-maintenance", "")
	b.logger.Info("nightly wiki maintenance complete",
		"auto_fixed", fixed, "deferred", deferred)
	return nil
}

func (b *Bot) registerHandlers() {
	b.bot.Handle("/start", b.onStart)
	b.bot.Handle("/login", b.onLogin)
	b.bot.Handle("/token", b.onLogin)
	b.bot.Handle(tele.OnText, b.onMessage)
	b.bot.Handle("/status", b.onStatus)
	if b.docs != nil {
		b.bot.Handle(tele.OnDocument, b.docs.onDocument)
	}
}

func (b *Bot) onMessage(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)

	if !b.isAllowlisted(userID) {
		b.logger.Warn("message from non-allowlisted user",
			"user_id", userID,
			"username", c.Sender().Username,
		)
		return nil
	}

	// Launch conversation in its own goroutine
	go b.handleConversation(c)

	return nil
}

// onStart handles the /start command. Three branches:
//
//  1. user already allowlisted → mint a fresh dashboard token (welcome back).
//  2. user unknown AND system has zero allowlisted users → first-run TOFU
//     bootstrap; this user claims the install. This is the only path that
//     auto-grants access, and it's bounded to "before any owner exists" —
//     otherwise the dashboard would have nobody to log in and approve future
//     requests.
//  3. user unknown AND someone is already allowlisted → enqueue the request
//     into pending_users, notify every allowlisted user via Telegram, and
//     reply that approval is required. The dashboard owner approves or
//     denies via /api/pending-users/{id}/{approve,deny}.
func (b *Bot) onStart(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)
	username := c.Sender().Username

	if b.isAllowlisted(userID) {
		return b.sendLoginToken(c, userID, "Welcome back to Aura.")
	}

	claimed, err := b.tryBootstrapUser(userID)
	if err != nil {
		b.logger.Error("bootstrap allowlist failed", "user_id", userID, "error", err)
		return c.Send("Aura could not complete first-time setup. Check the app logs and try /start again.")
	}
	if claimed {
		b.logger.Info("first-run telegram user bootstrapped",
			"user_id", userID,
			"username", username,
		)
		return b.sendLoginToken(c, userID, "Welcome to Aura. You claimed this first-run install.")
	}

	if b.authDB == nil {
		b.logger.Warn("start from non-allowlisted user (no auth store)",
			"user_id", userID,
			"username", username,
		)
		return c.Send("Aura is private. Ask the owner to add your Telegram user ID to TELEGRAM_ALLOWLIST: " + userID)
	}
	fresh, err := b.authDB.RequestAccess(context.Background(), userID, username)
	if err != nil {
		b.logger.Error("pending request enqueue failed", "user_id", userID, "error", err)
		return c.Send("Aura could not record your access request. Try /start again in a moment.")
	}
	if fresh {
		b.logger.Info("pending access request recorded",
			"user_id", userID,
			"username", username,
		)
		b.notifyOwnersOfPendingRequest(userID, username)
	} else {
		b.logger.Info("repeat pending access request",
			"user_id", userID,
			"username", username,
		)
	}
	return c.Send("Your access request is pending approval. The owner has been notified — you'll get a message here once you're approved.")
}

// notifyOwnersOfPendingRequest fans out a Telegram message to every
// allowlisted user (env allowlist + persisted bootstrap/approved set)
// telling them a stranger just hit /start. Best-effort — a delivery
// failure to one owner shouldn't block the rest. Only triggered for
// fresh requests so a user spamming /start can't pingstorm the owner.
func (b *Bot) notifyOwnersOfPendingRequest(userID, username string) {
	owners := b.collectOwnerIDs()
	if len(owners) == 0 {
		return
	}
	display := username
	if display == "" {
		display = "(no username)"
	}
	body := fmt.Sprintf(
		"New Aura access request:\n\n• User: @%s\n• Telegram ID: %s\n\nReview in the dashboard under Pending requests.",
		display, userID,
	)
	for _, owner := range owners {
		if owner == userID {
			continue
		}
		if err := b.SendToUser(owner, body); err != nil {
			b.logger.Warn("pending request notification failed",
				"owner_id", owner, "requester_id", userID, "error", err)
		}
	}
}

// collectOwnerIDs returns the union of env-configured allowlist IDs and
// persisted bootstrap/approved IDs, deduplicated so a user that lives in
// both places is not notified twice.
func (b *Bot) collectOwnerIDs() []string {
	seen := make(map[string]struct{})
	var out []string
	if b.cfg != nil {
		for _, id := range b.cfg.Allowlist {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	if b.authDB != nil {
		ids, err := b.authDB.AllowedUserIDs(context.Background())
		if err != nil {
			b.logger.Warn("collect owner ids: db lookup failed", "error", err)
		}
		for _, id := range ids {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

// ApproveAccess satisfies api.PendingApprover. It approves the pending
// row, mints a fresh dashboard token, and ships it to the requester over
// Telegram so the plaintext never round-trips through the dashboard.
// Returns auth.ErrInvalid when no open pending request exists for userID.
func (b *Bot) ApproveAccess(ctx context.Context, userID string) error {
	if b.authDB == nil {
		return fmt.Errorf("auth store unavailable")
	}
	if err := b.authDB.Approve(ctx, userID); err != nil {
		return err
	}
	token, err := b.authDB.Issue(ctx, userID)
	if err != nil {
		return fmt.Errorf("issue token: %w", err)
	}
	body := "Aura access approved.\n\nDashboard token (paste into the Aura web login):\n\n" +
		token + "\n\nKeep this private. You can ask /login anytime for a fresh token."
	if err := b.SendToUser(userID, body); err != nil {
		b.logger.Warn("approval notification failed", "user_id", userID, "error", err)
	}
	return nil
}

// DenyAccess satisfies api.PendingApprover. It rejects the pending row
// and sends the requester a courtesy Telegram note. Failure to deliver
// the note is logged but doesn't fail the deny.
func (b *Bot) DenyAccess(ctx context.Context, userID string) error {
	if b.authDB == nil {
		return fmt.Errorf("auth store unavailable")
	}
	if err := b.authDB.Deny(ctx, userID); err != nil {
		return err
	}
	if err := b.SendToUser(userID, "Your Aura access request was declined."); err != nil {
		b.logger.Warn("deny notification failed", "user_id", userID, "error", err)
	}
	return nil
}

func (b *Bot) onLogin(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)
	if !b.isAllowlisted(userID) {
		b.logger.Warn("login token requested by non-allowlisted user",
			"user_id", userID,
			"username", c.Sender().Username,
		)
		return c.Send("Aura is private. Ask the owner to add your Telegram user ID to TELEGRAM_ALLOWLIST: " + userID)
	}
	return b.sendLoginToken(c, userID, "Here is a fresh dashboard token.")
}

func (b *Bot) tryBootstrapUser(userID string) (bool, error) {
	if b.cfg.AllowlistConfigured || b.authDB == nil {
		return false, nil
	}
	return b.authDB.BootstrapUser(context.Background(), userID)
}

func (b *Bot) isAllowlisted(userID string) bool {
	if b.cfg != nil && b.cfg.IsAllowlisted(userID) {
		return true
	}
	if b.cfg == nil || b.cfg.AllowlistConfigured || b.authDB == nil {
		return false
	}
	ok, err := b.authDB.IsUserAllowed(context.Background(), userID)
	if err != nil {
		b.logger.Warn("bootstrap allowlist lookup failed", "user_id", userID, "error", err)
		return false
	}
	return ok
}

func (b *Bot) sendLoginToken(c tele.Context, userID, prefix string) error {
	if b.authDB == nil {
		return c.Send(prefix + "\n\nDashboard auth is not available in this run.")
	}
	token, err := b.authDB.Issue(context.Background(), userID)
	if err != nil {
		b.logger.Error("dashboard token issue failed", "user_id", userID, "error", err)
		return c.Send("I could not create a dashboard token. Check the app logs and try /login again.")
	}
	body := prefix + "\n\nDashboard token (paste into the Aura web login):\n\n" +
		token + "\n\nKeep this private. You can ask /login anytime for a fresh token."
	return c.Send(body)
}

func (b *Bot) handleConversation(c tele.Context) {
	userID := strconv.FormatInt(c.Sender().ID, 10)
	turnStart := time.Now()

	// Track active conversation
	b.active.Store(userID, true)
	defer b.active.Delete(userID)

	// Get or create conversation context
	ctxVal, loaded := b.ctxMap.LoadOrStore(userID, conversation.NewContext(conversation.Config{
		MaxTokens:   b.cfg.MaxContextTokens,
		MaxMessages: b.cfg.MaxHistoryMessages,
		Summarizer:  b.llm,
		Logger:      b.logger,
	}))
	convCtx := ctxVal.(*conversation.Context)
	_ = loaded // kept for clarity; system prompt now refreshes every turn

	// Refresh the system prompt on every turn so the Runtime Context
	// (current time + timezone) stays accurate. The LLM uses these values
	// when scheduling reminders, so a stale snapshot is worse than the
	// per-turn cost of re-rendering a few hundred bytes.
	systemPrompt := conversation.RenderSystemPrompt(time.Now(), time.Local)
	// Slice 11q: read SOUL.md / AGENTS.md / USER.md / TOOLS.md from the
	// configured overlay dir. Picobot pattern: lets the operator tune
	// personality, durable user facts, and tool guidance by editing a
	// file — the next user turn picks up the change with no recompile or
	// restart. Files are optional; missing ones are skipped silently.
	if overlay := conversation.LoadPromptOverlay(b.cfg.PromptOverlayPath); overlay != "" {
		systemPrompt += "\n\n" + overlay
	}
	if b.skills != nil {
		loadedSkills, err := b.skills.LoadAll()
		if err != nil {
			b.logger.Warn("failed to load local skills", "error", err)
		} else if block := auraskills.PromptBlock(loadedSkills); block != "" {
			systemPrompt += "\n\n" + block
		}
	}
	convCtx.SetSystemMessage(systemPrompt)

	b.logger.Info("conversation started",
		"user_id", userID,
		"username", c.Sender().Username,
		"message", c.Text(),
	)

	// Capture message index before this turn so we can archive only new messages.
	turnMsgIdx := convCtx.MessageCount()

	// Add user message to context
	convCtx.AddUserMessage(c.Text())

	// Slice 11p: speculative wiki retrieval. The model used to discover
	// durable memory only by emitting a search_wiki tool call, which cost
	// a full extra LLM round-trip per turn ("reason → emit tool call →
	// read result → re-reason → answer"). We now run the search up-front
	// and inject the top hits into the system prompt so the very first
	// inference already has relevant context. The embedding cache (slice
	// 11h) makes repeat queries effectively free; cold queries pay one
	// embed call but save the round-trip. The explicit search_wiki tool
	// stays available for follow-up queries the model wants to refine.
	// Picobot equivalent: internal/agent/context.go ranker injection.
	if b.search != nil && b.search.IsIndexed() {
		if results, err := b.search.Search(context.Background(), c.Text(), 5); err == nil && len(results) > 0 {
			convCtx.SetSearchContext(search.FormatResults(results))
		} else if err != nil {
			b.logger.Debug("speculative wiki search failed", "user_id", userID, "error", err)
		}
	}

	// Enforce context limits: summarize at 80%, trim at hard limit
	if err := convCtx.EnforceLimit(context.Background()); err != nil {
		b.logger.Error("context enforcement failed", "user_id", userID, "error", err)
	}

	// No LLM configured — echo mode
	if b.llm == nil {
		echo := "Echo: " + c.Text()
		if err := c.Send(echo); err != nil {
			b.logger.Error("failed to send echo", "user_id", userID, "error", err)
		}
		convCtx.AddAssistantMessage(echo)
		return
	}

	// Check hard budget before LLM call
	if b.budget != nil && b.budget.IsHardBudgetExceeded() {
		b.logger.Warn("hard budget exceeded, halting LLM call", "user_id", userID)
		c.Send("Budget limit reached. LLM calls are temporarily halted.")
		return
	}

	// Predict cost and check affordability
	if b.budget != nil && !b.budget.CanAfford(convCtx.EstimatedTokens(), 500) {
		b.logger.Warn("predicted cost exceeds hard budget, halting LLM call", "user_id", userID)
		c.Send("Predicted cost would exceed budget. Please adjust your budget or wait.")
		return
	}

	response, stats := b.runToolCallingLoop(context.Background(), c, convCtx, userID)
	if response != "" {
		b.sendAssistant(c, response)
	}

	// Slice 12b: archive new messages produced during this turn.
	if b.archiver != nil {
		chatID := c.Chat().ID
		for i, msg := range convCtx.MessagesSince(turnMsgIdx) {
			_ = b.archiver.Append(context.Background(), conversation.Turn{
				ChatID:    chatID,
				UserID:    c.Sender().ID,
				TurnIndex: int64(turnMsgIdx + i),
				Role:      msg.Role,
				Content:   msg.Content,
			})
		}

		// Slice 12e: post-turn summarizer extraction (log-only; apply in 12f).
		if b.summRunner != nil {
			if _, _, err := b.summRunner.MaybeExtract(context.Background(), chatID); err != nil {
				b.logger.Warn("summarizer extraction failed", "chat_id", chatID, "error", err)
			}
		}
	}

	// Slice 11r: per-turn telemetry. elapsed_ms is wall-clock from
	// receive to "ready to send"; llm_calls and tool_calls expose where
	// time went so we can correlate slow turns to the responsible
	// subsystem without sprinkling timers everywhere.
	b.logger.Info("conversation complete",
		"user_id", userID,
		"tokens_used", convCtx.TotalTokensUsed(),
		"elapsed_ms", time.Since(turnStart).Milliseconds(),
		"llm_calls", stats.llmCalls,
		"tool_calls", stats.toolCalls,
	)
}

// turnStats aggregates per-turn counters returned from runToolCallingLoop
// so handleConversation can emit a single structured log line covering
// total latency, LLM round-trips, and tool calls.
type turnStats struct {
	llmCalls  int
	toolCalls int
}

func (b *Bot) runToolCallingLoop(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string) (string, turnStats) {
	maxIterations := b.cfg.MaxToolIterations
	if maxIterations <= 0 {
		maxIterations = 10
	}

	var stats turnStats
	var lastToolResult string
	toolDefs := b.tools.Definitions()
	for iteration := 0; iteration < maxIterations; iteration++ {
		// Context bounding happens once at the start of handleConversation.
		// Re-enforcing on every tool iteration triggered a summarizer LLM
		// call mid-response, which both burned latency and degraded fidelity.
		// MaxToolIterations already caps growth within a single user turn.

		if b.budget != nil && b.budget.IsHardBudgetExceeded() {
			b.logger.Warn("hard budget exceeded during tool loop", "user_id", userID)
			return "Budget limit reached. LLM calls are temporarily halted.", stats
		}

		req := llm.Request{
			Messages: convCtx.Messages(),
			Model:    b.cfg.LLMModel,
			Tools:    toolDefs,
		}

		stats.llmCalls++
		ch, err := b.llm.Stream(ctx, req)
		if err != nil {
			b.logger.Error("LLM stream failed", "user_id", userID, "error", err)
			return "Sorry, I couldn't process your message. Please try again.", stats
		}

		resp, delivered, err := b.consumeStream(c, ch, userID)
		if err != nil {
			b.logger.Error("LLM stream read failed", "user_id", userID, "error", err)
			return "Sorry, I couldn't process your message. Please try again.", stats
		}

		convCtx.TrackTokens(resp.Usage)
		if b.budget != nil {
			b.budget.RecordUsage(resp.Usage.TotalTokens)
		}

		if !resp.HasToolCalls {
			response := strings.TrimSpace(resp.Content)
			if response == "" {
				if lastToolResult != "" {
					response = lastToolResult
				} else {
					response = "I completed the request but do not have anything else to add."
				}
			}
			convCtx.AddAssistantMessage(response)
			b.notifySoftBudget(c, userID)
			// If consumeStream already progressively edited a Telegram
			// message with the full content, suppress the caller's
			// c.Send to avoid double-delivery. Empty response signals
			// "already delivered" to handleConversation.
			if delivered {
				return "", stats
			}
			return response, stats
		}

		convCtx.AddAssistantToolCallMessage(resp.Content, resp.ToolCalls)
		stats.toolCalls += len(resp.ToolCalls)
		lastToolResult = b.executeToolCalls(ctx, c, convCtx, userID, resp.ToolCalls)
	}

	fallback := "Tool loop stopped after reaching the maximum iteration limit."
	if lastToolResult != "" {
		fallback += "\n\nLast tool result:\n" + lastToolResult
	}
	convCtx.AddAssistantMessage(fallback)
	return fallback, stats
}

// sendAssistant delivers an LLM-generated message to the user with
// Markdown rendered to Telegram's HTML subset. Plain operator strings
// (auth errors, bootstrap messages) keep using c.Send directly so we
// don't double-escape them. If HTML send fails (e.g. malformed render),
// fall back to a plain c.Send so the user still sees the response.
func (b *Bot) sendAssistant(c tele.Context, text string) {
	rendered := renderForTelegram(text)
	if err := c.Send(rendered, tele.ModeHTML); err != nil {
		b.logger.Warn("HTML send failed, falling back to plain text", "error", err)
		_ = c.Send(text)
	}
}

// streamingMinThreshold is the buffered-content size at which we stop
// hiding and send the placeholder Telegram message. Below this the
// model may still be deciding whether to call tools (in which case
// any text would be a discardable preface), so we wait until we have
// enough text that progressive display is clearly worth it.
const streamingMinThreshold = 30

// streamingEditThrottle bounds how often we call Telegram's editMessage
// API. Telegram rate-limits edits to ~1/sec per chat; 800ms keeps us
// safely under the limit while still feeling responsive.
const streamingEditThrottle = 800 * time.Millisecond

// consumeStream reads tokens from ch and progressively edits a Telegram
// message as text accumulates. Returns an llm.Response shaped like the
// one Send would have produced plus a flag indicating whether a
// Telegram message has already been delivered for this iteration. When
// delivered=true, the caller should suppress c.Send to avoid double-
// posting. Slice 11s populates Token.Usage and Token.ToolCalls only on
// the final Done token, so we can build a complete Response here.
func (b *Bot) consumeStream(c tele.Context, ch <-chan llm.Token, userID string) (llm.Response, bool, error) {
	var sb strings.Builder
	var msg *tele.Message
	var lastEdit time.Time
	var resp llm.Response

	flush := func() {
		text := renderForTelegram(sb.String())
		if msg == nil {
			if sb.Len() < streamingMinThreshold {
				return
			}
			sent, err := c.Bot().Send(c.Recipient(), text, tele.ModeHTML)
			if err != nil {
				b.logger.Warn("streaming initial send failed", "user_id", userID, "error", err)
				return
			}
			msg = sent
			lastEdit = time.Now()
			return
		}
		if time.Since(lastEdit) < streamingEditThrottle {
			return
		}
		if _, err := c.Bot().Edit(msg, text, tele.ModeHTML); err != nil {
			// Rate limit or transient: skip this edit, the next one will retry.
			b.logger.Debug("streaming edit failed", "user_id", userID, "error", err)
			return
		}
		lastEdit = time.Now()
	}

	for tok := range ch {
		if tok.Err != nil {
			return llm.Response{}, msg != nil, tok.Err
		}
		if tok.Content != "" {
			sb.WriteString(tok.Content)
			flush()
		}
		if tok.Done {
			resp = llm.Response{
				Content:      sb.String(),
				HasToolCalls: len(tok.ToolCalls) > 0,
				ToolCalls:    tok.ToolCalls,
				Usage:        tok.Usage,
			}
			// Final edit so the message reflects the complete text even
			// if the throttle skipped the last delta.
			if msg != nil && !resp.HasToolCalls {
				rendered := renderForTelegram(sb.String())
				if _, err := c.Bot().Edit(msg, rendered, tele.ModeHTML); err != nil {
					b.logger.Warn("streaming final edit failed", "user_id", userID, "error", err)
				}
			}
			break
		}
	}
	return resp, msg != nil && !resp.HasToolCalls, nil
}

// executeToolCalls runs an assistant turn's tool calls concurrently and
// appends results in original order. The LLM batches independent calls into
// one assistant turn (e.g. search_wiki + web_search side-by-side); running
// them sequentially serialized N round-trips of latency for no reason.
//
// Concurrency safety: Registry.Execute is RWMutex-guarded for lookup, and
// individual tools run outside the lock. Wiki/source writes serialize on
// SQLite at the storage layer. Activity pings are emitted up-front so the
// user sees all running tools immediately rather than drip-fed.
//
// Returns the last result content (in original order), used by the caller
// as a fallback when the model returns an empty final response.
func (b *Bot) executeToolCalls(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, calls []llm.ToolCall) string {
	if len(calls) == 0 {
		return ""
	}

	for _, tc := range calls {
		c.Send(toolActivityMessage(tc.Name))
	}

	type outcome struct {
		id      string
		content string
	}
	results := make([]outcome, len(calls))

	var wg sync.WaitGroup
	for i, tc := range calls {
		wg.Add(1)
		go func(i int, tc llm.ToolCall) {
			defer wg.Done()
			toolCtx := tools.WithUserID(ctx, userID)
			result, err := b.tools.Execute(toolCtx, tc.Name, tc.Arguments)
			if err != nil {
				result = "(tool error) " + err.Error()
				b.logger.Warn("tool call failed", "user_id", userID, "tool", tc.Name, "error", err)
			}
			results[i] = outcome{id: tc.ID, content: result}
		}(i, tc)
	}
	wg.Wait()

	var lastToolResult string
	for _, r := range results {
		convCtx.AddToolResultMessage(r.id, r.content)
		lastToolResult = r.content
	}
	return lastToolResult
}

func toolActivityMessage(name string) string {
	if strings.TrimSpace(name) == "" {
		return "Running tool"
	}
	return fmt.Sprintf("Running: %s", name)
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

// onStatus handles the /status command, returning budget and context info.
func (b *Bot) onStatus(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)
	if !b.isAllowlisted(userID) {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("Aura Status\n\n")

	// Budget info
	if b.budget != nil {
		status := b.budget.Status()
		fmt.Fprintf(&sb, "Tokens used: %d\n", status.TotalTokens)
		fmt.Fprintf(&sb, "Estimated cost: $%.4f\n", status.TotalCost)
		if status.SoftBudget > 0 {
			fmt.Fprintf(&sb, "Soft budget: $%.2f\n", status.SoftBudget)
		}
		if status.HardBudget > 0 {
			fmt.Fprintf(&sb, "Hard budget: $%.2f\n", status.HardBudget)
		}
		if status.SoftBudgetExceeded && !status.BudgetExceeded {
			sb.WriteString("Status: SOFT BUDGET REACHED\n")
		}
		if status.BudgetExceeded {
			sb.WriteString("Status: HARD BUDGET EXCEEDED\n")
		}
	} else {
		sb.WriteString("Budget: not configured\n")
	}

	// Per-conversation context info
	ctxVal, ok := b.ctxMap.Load(userID)
	if ok {
		convCtx := ctxVal.(*conversation.Context)
		fmt.Fprintf(&sb, "\nContext tokens: %d / %d\n", convCtx.EstimatedTokens(), convCtx.MaxTokens())
		fmt.Fprintf(&sb, "Conversation tokens used: %d\n", convCtx.TotalTokensUsed())
		if b.budget != nil {
			predictedCost := b.budget.PredictCost(convCtx.EstimatedTokens(), 500)
			fmt.Fprintf(&sb, "Next call est. cost: $%.4f\n", predictedCost)
		}
	}

	return c.Send(sb.String())
}

// notifySoftBudget sends a one-time warning to the user when soft budget
// is first exceeded. userID is kept on the signature so future per-user
// throttling has a hook without changing call sites.
func (b *Bot) notifySoftBudget(c tele.Context, _ string) {
	if b.budget != nil && b.budget.ShouldNotifySoftBudget() {
		status := b.budget.Status()
		c.Send(fmt.Sprintf("Soft budget reached ($%.2f / $%.2f). LLM calls continue until hard budget is hit.", status.TotalCost, status.SoftBudget))
	}
}

// BudgetStatus returns the current budget status for external consumers.
func (b *Bot) BudgetStatus() budget.Status {
	if b.budget == nil {
		return budget.Status{}
	}
	return b.budget.Status()
}

// noopWikiSearcher satisfies summarizer.WikiSearcher with an always-empty result.
// Used when the search engine is not yet wired (slice 12e); 12f replaces it.
type noopWikiSearcher struct{}

func (noopWikiSearcher) Search(_ context.Context, _ string, _ int) ([]search.Result, error) {
	return nil, nil
}
