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
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/search"
	"github.com/aura/aura/internal/wiki"

	"github.com/philippgille/chromem-go"
	tele "gopkg.in/telebot.v4"
)

// Bot wraps the telebot instance with allowlist access control and LLM integration.
type Bot struct {
	bot    *tele.Bot
	cfg    *config.Config
	logger *slog.Logger
	llm    llm.Client
	wiki   *wiki.Writer
	search *search.Engine
	budget *budget.Tracker
	active sync.Map // maps userID string -> bool (active conversation tracking)
	ctxMap sync.Map // maps userID string -> *conversation.Context
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

	var wikiWriter *wiki.Writer
	if client != nil {
		wikiWriter = wiki.NewWriter(wikiStore, client, logger)
	}

	// Set up search engine
	var searchEngine *search.Engine
	if cfg.EmbeddingAPIKey != "" || cfg.LLMAPIKey != "" {
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

	b := &Bot{
		bot:    tb,
		cfg:    cfg,
		logger: logger,
		llm:    client,
		wiki:   wikiWriter,
		search: searchEngine,
		budget: budget.NewTracker(budget.Config{
			SoftBudget:   cfg.SoftBudget,
			HardBudget:   cfg.HardBudget,
			CostPerToken: cfg.CostPerToken,
		}, logger),
	}

	b.registerHandlers()
	return b, nil
}

// Username returns the bot's Telegram username.
func (b *Bot) Username() string {
	return b.bot.Me.Username
}

// Start begins polling for Telegram messages.
func (b *Bot) Start() {
	b.logger.Info("telegram bot started")
	b.bot.Start()
}

// Stop gracefully stops the bot.
func (b *Bot) Stop() {
	b.bot.Stop()
}

func (b *Bot) registerHandlers() {
	b.bot.Handle("/start", b.onStart)
	b.bot.Handle(tele.OnText, b.onMessage)
	b.bot.Handle("/status", b.onStatus)
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

	// Set system prompt on first message (new context)
	if !loaded {
		convCtx.SetSystemMessage(conversation.DefaultSystemPrompt())
	}

	b.logger.Info("conversation started",
		"user_id", userID,
		"username", c.Sender().Username,
		"message", c.Text(),
	)

	// Add user message to context
	convCtx.AddUserMessage(c.Text())

	// Inject relevant wiki knowledge into context
	if b.search != nil && b.search.IsIndexed() {
		results, err := b.search.Search(context.Background(), c.Text(), 5)
		if err != nil {
			b.logger.Warn("wiki search failed", "user_id", userID, "error", err)
		} else if len(results) > 0 {
			convCtx.SetSearchContext(search.FormatResults(results))
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

	req := llm.Request{
		Messages: convCtx.Messages(),
		Model:    b.cfg.LLMModel,
	}

	// Try streaming first, fall back to non-streaming
	ch, err := b.llm.Stream(context.Background(), req)
	if err != nil {
		b.logger.Warn("streaming failed, falling back to send", "error", err)
		resp, sendErr := b.llm.Send(context.Background(), req)
		if sendErr != nil {
			b.logger.Error("LLM send failed", "user_id", userID, "error", sendErr)
			c.Send("Sorry, I couldn't process your message. Please try again.")
			return
		}
		convCtx.AddAssistantMessage(resp.Content)
		convCtx.TrackTokens(resp.Usage)
		if b.budget != nil {
			b.budget.RecordUsage(resp.Usage.TotalTokens)
			b.notifySoftBudget(c, userID)
		}
		c.Send(resp.Content)
		b.tryStoreWiki(context.Background(), resp.Content, userID)
		return
	}

	// Collect streaming response
	var sb strings.Builder
	var streamTokenEstimate int
	for token := range ch {
		if token.Err != nil {
			b.logger.Error("stream error", "user_id", userID, "error", token.Err)
			break
		}
		if token.Done {
			break
		}
		sb.WriteString(token.Content)
		streamTokenEstimate += len(token.Content) / 4
	}

	response := sb.String()
	if response == "" {
		response = "I couldn't generate a response."
	}

	convCtx.AddAssistantMessage(response)
	// Record budget usage for streaming (estimated tokens: context + output)
	if b.budget != nil {
		totalEstimate := convCtx.EstimatedTokens() + streamTokenEstimate
		b.budget.RecordUsage(totalEstimate)
		b.notifySoftBudget(c, userID)
	}
	c.Send(response)

	// Attempt to store wiki knowledge from the response
	b.tryStoreWiki(context.Background(), response, userID)

	b.logger.Info("conversation complete",
		"user_id", userID,
		"tokens_used", convCtx.TotalTokensUsed(),
	)
}

// tryStoreWiki attempts to parse an LLM response as wiki content and store it.
// If the response doesn't look like YAML, this is a no-op.
func (b *Bot) tryStoreWiki(ctx context.Context, response string, userID string) {
	if b.wiki == nil {
		return
	}
	if !looksLikeWikiContent(response) {
		return
	}
	page, err := b.wiki.WriteFromLLMOutput(ctx, response, "ingest_v1")
	if err != nil {
		b.logger.Warn("failed to store wiki page from LLM output",
			"user_id", userID,
			"error", err,
		)
		return
	}
	b.logger.Info("stored wiki page from conversation",
		"user_id", userID,
		"title", page.Title,
	)

	// Re-index the newly written page
	if b.search != nil {
		slug := wiki.Slug(page.Title)
		if err := b.search.ReindexWikiPage(ctx, slug); err != nil {
			b.logger.Warn("failed to re-index wiki page", "slug", slug, "error", err)
		}
	}
}

// looksLikeWikiContent checks if a response might contain wiki content.
// Detects both markdown-with-frontmatter format and legacy YAML format.
func looksLikeWikiContent(s string) bool {
	trimmed := strings.TrimSpace(s)
	// MD format: starts with --- and has frontmatter
	if strings.HasPrefix(trimmed, "---") {
		return strings.Contains(s, "title:") && strings.Contains(s, "schema_version:")
	}
	// Legacy YAML format: has title and schema_version markers
	return strings.Contains(s, "title:") && strings.Contains(s, "schema_version:")
}

// createEmbeddingFunc builds a chromem embedding function from config.
// Falls back to LLM API key if no dedicated embedding key is set.
// createLLMClient builds the LLM client chain with failover:
// 1. OpenAI-compatible (primary) → Ollama (offline fallback)
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

func createEmbeddingFunc(cfg *config.Config) chromem.EmbeddingFunc {
	apiKey := cfg.EmbeddingAPIKey
	if apiKey == "" {
		apiKey = cfg.LLMAPIKey
	}
	baseURL := cfg.EmbeddingBaseURL
	model := cfg.EmbeddingModel

	normalized := true
	return chromem.NewEmbeddingFuncOpenAICompat(baseURL, apiKey, model, &normalized)
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
		sb.WriteString(fmt.Sprintf("Tokens used: %d\n", status.TotalTokens))
		sb.WriteString(fmt.Sprintf("Estimated cost: $%.4f\n", status.TotalCost))
		if status.SoftBudget > 0 {
			sb.WriteString(fmt.Sprintf("Soft budget: $%.2f\n", status.SoftBudget))
		}
		if status.HardBudget > 0 {
			sb.WriteString(fmt.Sprintf("Hard budget: $%.2f\n", status.HardBudget))
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
		sb.WriteString(fmt.Sprintf("\nContext tokens: %d / %d\n", convCtx.EstimatedTokens(), convCtx.MaxTokens()))
		sb.WriteString(fmt.Sprintf("Conversation tokens used: %d\n", convCtx.TotalTokensUsed()))
		if b.budget != nil {
			predictedCost := b.budget.PredictCost(convCtx.EstimatedTokens(), 500)
			sb.WriteString(fmt.Sprintf("Next call est. cost: $%.4f\n", predictedCost))
		}
	}

	return c.Send(sb.String())
}

// notifySoftBudget sends a one-time warning to the user when soft budget is first exceeded.
func (b *Bot) notifySoftBudget(c tele.Context, userID string) {
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
