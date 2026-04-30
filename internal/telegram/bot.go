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
	authDB     *auth.Store    // dashboard bearer-token store (slice 10d)
	mcpClients []*mcp.Client  // active MCP server connections (slice 11a)
	api        http.Handler   // read-only JSON API for the dashboard, mounted on the health server
	active     sync.Map       // maps userID string -> bool (active conversation tracking)
	ctxMap     sync.Map       // maps userID string -> *conversation.Context
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
	if cfg.OllamaAPIKey != "" {
		toolRegistry.Register(tools.NewWebSearchTool(cfg.OllamaAPIKey, cfg.OllamaWebBaseURL))
		toolRegistry.Register(tools.NewWebFetchTool(cfg.OllamaAPIKey, cfg.OllamaWebBaseURL))
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

// dispatchWikiMaintenance runs the autonomous nightly wiki pass:
// regenerate index.md, lint for broken links / missing categories, and
// append a log entry summarizing the result. Pure deterministic; no
// LLM round-trip. Logs lint findings so the operator can spot drift
// without checking log.md by hand.
func (b *Bot) dispatchWikiMaintenance(ctx context.Context) error {
	if b.wiki == nil {
		return fmt.Errorf("wiki maintenance: wiki store unavailable")
	}
	b.wiki.RebuildIndex(ctx)
	issues, err := b.wiki.Lint(ctx)
	if err != nil {
		return fmt.Errorf("wiki lint: %w", err)
	}
	if len(issues) > 0 {
		b.logger.Warn("nightly wiki maintenance found issues", "count", len(issues))
		for _, issue := range issues {
			b.logger.Warn("wiki lint issue", "slug", issue.Slug, "msg", issue.Message)
		}
	}
	b.wiki.AppendLog(ctx, "nightly-maintenance", "")
	b.logger.Info("nightly wiki maintenance complete", "lint_issues", len(issues))
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

// onStart handles the /start command. On a blank first-run allowlist, the
// first Telegram user to start the bot claims the private bootstrap slot.
// After that, normal allowlist checks apply.
func (b *Bot) onStart(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)

	if !b.isAllowlisted(userID) {
		if claimed, err := b.tryBootstrapUser(userID); err != nil {
			b.logger.Error("bootstrap allowlist failed", "user_id", userID, "error", err)
			return c.Send("Aura could not complete first-time setup. Check the app logs and try /start again.")
		} else if !claimed {
			b.logger.Warn("start from non-allowlisted user",
				"user_id", userID,
				"username", c.Sender().Username,
			)
			return c.Send("Aura is private. Ask the owner to add your Telegram user ID to TELEGRAM_ALLOWLIST: " + userID)
		}

		b.logger.Info("first-run telegram user bootstrapped",
			"user_id", userID,
			"username", c.Sender().Username,
		)
		return b.sendLoginToken(c, userID, "Welcome to Aura. You claimed this first-run install.")
	}

	return b.sendLoginToken(c, userID, "Welcome back to Aura.")
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

	// Add user message to context
	convCtx.AddUserMessage(c.Text())

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

	response := b.runToolCallingLoop(context.Background(), c, convCtx, userID)
	if response != "" {
		c.Send(response)
	}

	b.logger.Info("conversation complete",
		"user_id", userID,
		"tokens_used", convCtx.TotalTokensUsed(),
	)
}

func (b *Bot) runToolCallingLoop(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string) string {
	maxIterations := b.cfg.MaxToolIterations
	if maxIterations <= 0 {
		maxIterations = 10
	}

	var lastToolResult string
	toolDefs := b.tools.Definitions()
	for iteration := 0; iteration < maxIterations; iteration++ {
		// Context bounding happens once at the start of handleConversation.
		// Re-enforcing on every tool iteration triggered a summarizer LLM
		// call mid-response, which both burned latency and degraded fidelity.
		// MaxToolIterations already caps growth within a single user turn.

		if b.budget != nil && b.budget.IsHardBudgetExceeded() {
			b.logger.Warn("hard budget exceeded during tool loop", "user_id", userID)
			return "Budget limit reached. LLM calls are temporarily halted."
		}

		req := llm.Request{
			Messages: convCtx.Messages(),
			Model:    b.cfg.LLMModel,
			Tools:    toolDefs,
		}

		resp, err := b.llm.Send(ctx, req)
		if err != nil {
			b.logger.Error("LLM send failed", "user_id", userID, "error", err)
			return "Sorry, I couldn't process your message. Please try again."
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
			return response
		}

		convCtx.AddAssistantToolCallMessage(resp.Content, resp.ToolCalls)
		lastToolResult = b.executeToolCalls(ctx, c, convCtx, userID, resp.ToolCalls)
	}

	fallback := "Tool loop stopped after reaching the maximum iteration limit."
	if lastToolResult != "" {
		fallback += "\n\nLast tool result:\n" + lastToolResult
	}
	convCtx.AddAssistantMessage(fallback)
	return fallback
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
