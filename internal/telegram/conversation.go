package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/search"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/tools"

	tele "gopkg.in/telebot.v4"
)

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

	// Capture the user text locally so we can always archive it even if
	// EnforceLimit (below) trims it out of convCtx.
	userText := c.Text()
	convCtx.AddUserMessage(userText)

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

	// Snapshot count AFTER EnforceLimit so any trimming is already absorbed.
	// Loop messages added by runToolCallingLoop occupy [preLoopIdx, end).
	preLoopIdx := convCtx.MessageCount()

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

	// Slice 12b + 12u.7 (HR-04): archive the user message and every
	// message produced during this turn. turn_index is allocated from the
	// archive's MAX(turn_index) for this chat so it stays correct even
	// when EnforceLimit trims convCtx (which would have made an
	// in-memory MessageCount snapshot unreliable). The user message is
	// captured locally above so we always have the original even if
	// EnforceLimit dropped it from convCtx.
	if b.archiver != nil && b.archiveDB != nil {
		chatID := c.Chat().ID
		ctx := context.Background()

		nextIdx := int64(0)
		if maxIdx, err := b.archiveDB.MaxTurnIndex(ctx, chatID); err == nil {
			nextIdx = maxIdx + 1
		} else {
			b.logger.Warn("archive: max turn_index lookup failed",
				"chat_id", chatID, "error", err)
		}

		// User message: archived first from the locally-captured text.
		_ = b.archiver.Append(ctx, conversation.Turn{
			ChatID:    chatID,
			UserID:    c.Sender().ID,
			TurnIndex: nextIdx,
			Role:      "user",
			Content:   userText,
		})
		nextIdx++

		// Loop messages: assistant tool-calls, tool results, final answer.
		// Snapshot taken after EnforceLimit so the slice is the messages
		// runToolCallingLoop appended this turn.
		loopMsgs := convCtx.MessagesSince(preLoopIdx)
		elapsedMS := time.Since(turnStart).Milliseconds()
		for i, msg := range loopMsgs {
			turn := conversation.Turn{
				ChatID:     chatID,
				UserID:     c.Sender().ID,
				TurnIndex:  nextIdx,
				Role:       msg.Role,
				Content:    msg.Content,
				ToolCallID: msg.ToolCallID,
			}
			if len(msg.ToolCalls) > 0 {
				if raw, err := json.Marshal(msg.ToolCalls); err == nil {
					turn.ToolCalls = string(raw)
				} else {
					b.logger.Warn("archive: tool_calls marshal failed",
						"chat_id", chatID, "turn_index", nextIdx, "error", err)
				}
			}
			if msg.Role == "assistant" && i == len(loopMsgs)-1 {
				turn.LLMCalls = stats.llmCalls
				turn.ToolCallsCount = stats.toolCalls
				turn.ElapsedMS = elapsedMS
				turn.TokensIn = convCtx.TotalTokensUsed()
			}
			_ = b.archiver.Append(ctx, turn)
			nextIdx++
		}

		// Slice 12e: post-turn summarizer extraction (log-only; apply in 12f).
		if b.summRunner != nil {
			if _, _, err := b.summRunner.MaybeExtract(ctx, chatID); err != nil {
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
				result = tools.FormatToolError(err)
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
