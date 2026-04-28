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

	// Set up LLM client with retry
	var client llm.Client
	if cfg.LLMAPIKey != "" {
		openaiClient := llm.NewOpenAIClient(llm.OpenAIConfig{
			APIKey:  cfg.LLMAPIKey,
			BaseURL: cfg.LLMBaseURL,
			Model:   cfg.LLMModel,
		})
		client = llm.NewRetryClient(openaiClient, llm.RetryConfig{
			MaxRetries: cfg.LLMMaxRetries,
			BaseDelay:  time.Second,
			MaxDelay:   30 * time.Second,
		})
	} else {
		logger.Warn("LLM_API_KEY not set, bot will echo messages without LLM")
	}

	// Set up wiki store and writer
	wikiStore, err := wiki.NewStore(cfg.WikiPath, logger)
	if err != nil {
		return nil, fmt.Errorf("creating wiki store: %w", err)
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
		if cfg.PgConnString != "" {
			se, err = search.NewEngineWithFallback(cfg.WikiPath, embedFn, cfg.PgConnString, logger)
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

func (b *Bot) handleConversation(c tele.Context) {
	userID := strconv.FormatInt(c.Sender().ID, 10)

	// Track active conversation
	b.active.Store(userID, true)
	defer b.active.Delete(userID)

	// Get or create conversation context
	ctxVal, _ := b.ctxMap.LoadOrStore(userID, conversation.NewContext(conversation.Config{
		MaxTokens:  b.cfg.MaxContextTokens,
		Summarizer: b.llm,
		Logger:     b.logger,
	}))
	convCtx := ctxVal.(*conversation.Context)

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
			convCtx.SetSystemMessage(search.FormatResults(results))
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
	if !looksLikeWikiYAML(response) {
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

// looksLikeWikiYAML checks if a response might contain wiki YAML.
func looksLikeWikiYAML(s string) bool {
	return strings.Contains(s, "title:") &&
		strings.Contains(s, "content:") &&
		strings.Contains(s, "schema_version:")
}

// createEmbeddingFunc builds a chromem embedding function from config.
// Falls back to LLM API key if no dedicated embedding key is set.
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
	}

	return c.Send(sb.String())
}

// BudgetStatus returns the current budget status for external consumers.
func (b *Bot) BudgetStatus() budget.Status {
	if b.budget == nil {
		return budget.Status{}
	}
	return b.budget.Status()
}
