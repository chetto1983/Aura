package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/budget"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/ingest"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/ocr"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/search"
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/tools"
	"github.com/aura/aura/internal/wiki"

	"github.com/philippgille/chromem-go"
	tele "gopkg.in/telebot.v4"
)

// Bot wraps the telebot instance with allowlist access control and LLM integration.
type Bot struct {
	bot     *tele.Bot
	cfg     *config.Config
	logger  *slog.Logger
	llm     llm.Client
	wiki    *wiki.Store
	search  *search.Engine
	tools   *tools.Registry
	budget  *budget.Tracker
	sources *source.Store
	ocr     *ocr.Client
	docs    *docHandler
	sched   *scheduler.Scheduler
	schedDB *scheduler.Store
	active  sync.Map // maps userID string -> bool (active conversation tracking)
	ctxMap  sync.Map // maps userID string -> *conversation.Context
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
	if cfg.EmbeddingAPIKey != "" {
		embedFn := createEmbeddingFunc(cfg)
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

	b := &Bot{
		bot:     tb,
		cfg:     cfg,
		logger:  logger,
		llm:     client,
		wiki:    wikiStore,
		search:  searchEngine,
		tools:   toolRegistry,
		sources: sourceStore,
		ocr:     ocrClient,
		schedDB: schedStore,
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
		Allowlist: cfg.IsAllowlisted,
		Logger:    logger,
		// Slice 6: auto-ingest hook. Compile every freshly-OCR'd source
		// into a wiki summary page so the user sees a [[source-src-...]]
		// link in the final progress message instead of "ready for ingest".
		AfterOCR: ingestPipeline.AfterOCR,
	})

	b.registerHandlers()
	return b, nil
}

// Username returns the bot's Telegram username.
func (b *Bot) Username() string {
	return b.bot.Me.Username
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
	b.bot.Stop()
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
	b.bot.Handle(tele.OnText, b.onMessage)
	b.bot.Handle("/status", b.onStatus)
	if b.docs != nil {
		b.bot.Handle(tele.OnDocument, b.docs.onDocument)
	}
}

func (b *Bot) onMessage(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)

	if !b.cfg.IsAllowlisted(userID) {
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

// onStart handles the /start command. When a user opens the bot via the invite QR code
// (t.me/bot?start=invite), they are automatically added to the allowlist.
func (b *Bot) onStart(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)

	if !b.cfg.IsAllowlisted(userID) {
		b.cfg.AddToAllowlist(userID)
		b.logger.Info("user auto-allowlisted via invite",
			"user_id", userID,
			"username", c.Sender().Username,
		)
		c.Send("Welcome to Aura! You've been granted access. Send me a message to start chatting.")
	} else {
		c.Send("Welcome back! Send me a message to continue.")
	}

	return nil
}

func (b *Bot) handleConversation(c tele.Context) {
	userID := strconv.FormatInt(c.Sender().ID, 10)

	// Track active conversation
	b.active.Store(userID, true)
	defer b.active.Delete(userID)

	// Get or create conversation context
	ctxVal, loaded := b.ctxMap.LoadOrStore(userID, conversation.NewContext(conversation.Config{
		MaxTokens:  b.cfg.MaxContextTokens,
		Summarizer: b.llm,
		Logger:     b.logger,
	}))
	convCtx := ctxVal.(*conversation.Context)
	_ = loaded // kept for clarity; system prompt now refreshes every turn

	// Refresh the system prompt on every turn so the Runtime Context
	// (current time + timezone) stays accurate. The LLM uses these values
	// when scheduling reminders, so a stale snapshot is worse than the
	// per-turn cost of re-rendering a few hundred bytes.
	convCtx.SetSystemMessage(conversation.RenderSystemPrompt(time.Now(), time.Local))

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
		if err := convCtx.EnforceLimit(ctx); err != nil {
			b.logger.Error("context enforcement failed", "user_id", userID, "error", err)
		}

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
		for _, tc := range resp.ToolCalls {
			c.Send(toolActivityMessage(tc.Name))

			// schedule_task (and any future user-aware tool) needs the
			// caller's Telegram ID so reminders go back to the right
			// chat. tools.WithUserID is a no-op for tools that ignore it.
			toolCtx := tools.WithUserID(ctx, userID)
			result, err := b.tools.Execute(toolCtx, tc.Name, tc.Arguments)
			if err != nil {
				result = "(tool error) " + err.Error()
				b.logger.Warn("tool call failed", "user_id", userID, "tool", tc.Name, "error", err)
			}
			lastToolResult = result
			convCtx.AddToolResultMessage(tc.ID, result)
		}
	}

	fallback := "Tool loop stopped after reaching the maximum iteration limit."
	if lastToolResult != "" {
		fallback += "\n\nLast tool result:\n" + lastToolResult
	}
	convCtx.AddAssistantMessage(fallback)
	return fallback
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
	if !b.cfg.IsAllowlisted(userID) {
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
