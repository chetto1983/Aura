package conversation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/aura/aura/internal/llm"
)

// Context manages the conversation state for a single conversation.
// mu serializes all reads and writes so StreamDispatcher goroutines can call
// AddToolResultMessage concurrently without a data race.
type Context struct {
	mu               sync.RWMutex
	messages         []llm.Message
	summary          string
	transcript       []string
	maxTokens        int
	maxMessages      int
	summarizer       llm.Client
	logger           *slog.Logger
	totalTokensUsed  int
	baseSystemPrompt string
	agentNoteContent string
	searchContext    string
}

// Config holds configuration for conversation context.
//
// MaxMessages caps the number of in-flight messages (Picobot-style hard cap).
// When >0, the oldest non-system messages are trimmed before any token-based
// summarization fires. This is cheap (no LLM call) and self-cleans stale tool
// results so the wiki/sources tools — not the chat history — carry durable
// memory. Set to 0 to disable and fall back to pure token-based limits.
type Config struct {
	MaxTokens   int
	MaxMessages int
	Summarizer  llm.Client
	Logger      *slog.Logger
}

// NewContext creates a new conversation context.
func NewContext(cfg Config) *Context {
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4000
	}
	return &Context{
		maxTokens:   maxTokens,
		maxMessages: cfg.MaxMessages,
		summarizer:  cfg.Summarizer,
		logger:      cfg.Logger,
	}
}

// AddUserMessage appends a user message and manages context if needed.
func (c *Context) AddUserMessage(content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, llm.Message{Role: "user", Content: content})
	c.transcript = append(c.transcript, "user: "+content)
}

// AddAssistantMessage appends an assistant message.
func (c *Context) AddAssistantMessage(content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, llm.Message{Role: "assistant", Content: content})
	c.transcript = append(c.transcript, "assistant: "+content)
}

// AddAssistantToolCallMessage appends an assistant message containing tool calls.
func (c *Context) AddAssistantToolCallMessage(content string, toolCalls []llm.ToolCall) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, llm.Message{Role: "assistant", Content: content, ToolCalls: toolCalls})
	if content != "" {
		c.transcript = append(c.transcript, "assistant: "+content)
	}
}

// AddToolResultMessage appends a tool result correlated to an assistant tool call.
func (c *Context) AddToolResultMessage(toolCallID string, content string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, llm.Message{Role: "tool", Content: content, ToolCallID: toolCallID})
}

// EnforceLimit keeps the mutable conversation history bounded.
//
// Strategy (cheapest action first):
//  1. If maxMessages>0 and we exceed it, drop oldest non-system messages
//     down to the cap with a tool-safe boundary. No LLM call. This handles
//     normal growth — tool blobs and chat turns from earlier sessions get
//     evicted long before they cause trouble.
//  2. If the non-system history is still over the token soft threshold (80%)
//     AND a summarizer is configured, summarize. Stable system prompt content
//     is rebuilt each turn and must not trigger a hidden post-turn LLM call.
//  3. If we're over the hard token limit, trim oldest until we fit.
//  4. Last resort: truncate individual messages.
func (c *Context) EnforceLimit(ctx context.Context) error {
	c.enforceMessageCap()

	if !c.IsOverLimit() && !c.ShouldSummarize() {
		return nil
	}

	// Soft threshold breached and a summarizer is configured: summarize.
	if c.ShouldSummarize() {
		if err := c.Summarize(ctx); err != nil {
			return err
		}
	}

	// If still over the hard limit after summarization, trim aggressively
	trimPasses := 0
	prevTokenCount := c.EstimatedHistoryTokens()
	for c.IsOverLimit() && trimPasses < 10 {
		c.trimOldest()
		trimPasses++
		// If trimming made no progress, break to avoid infinite loop
		currentTokens := c.EstimatedHistoryTokens()
		if currentTokens == prevTokenCount {
			break
		}
		prevTokenCount = currentTokens
	}

	// Last resort: truncate individual messages to fit within limit
	if c.IsOverLimit() {
		c.truncateMessages()
	}

	if trimPasses > 0 || c.IsOverLimit() {
		c.logger.Info("enforced context limit",
			"trim_passes", trimPasses,
			"estimated_history_tokens", c.EstimatedHistoryTokens(),
			"estimated_tokens", c.EstimatedTokens(),
			"max_tokens", c.maxTokens,
		)
	}

	return nil
}

// enforceMessageCap drops the oldest non-system messages when the message
// count exceeds maxMessages. The split point is moved through tool-call /
// tool-result pairs so we never strand a tool result without its assistant
// call (which makes the LLM API reject the next request).
//
// Picobot-equivalent: internal/session/manager.go:trim. The wiki/sources
// tools provide durable memory, so dropping old in-flight history is safe.
func (c *Context) enforceMessageCap() {
	if c.maxMessages <= 0 {
		return
	}

	// Don't count the system message against the cap — it's stable identity.
	hasSystem := len(c.messages) > 0 && c.messages[0].Role == "system"
	body := c.messages
	if hasSystem {
		body = body[1:]
	}

	if len(body) <= c.maxMessages {
		return
	}

	// We want to keep the LAST maxMessages messages. The split point is the
	// index in `body` of the first message we keep.
	split := len(body) - c.maxMessages
	split = toolSafeBoundary(body, split)
	if split <= 0 {
		return
	}

	dropped := split
	kept := body[split:]
	if hasSystem {
		c.messages = append([]llm.Message{c.messages[0]}, kept...)
	} else {
		c.messages = append([]llm.Message{}, kept...)
	}

	if c.logger != nil {
		c.logger.Info("trimmed by message cap",
			"dropped", dropped,
			"kept", len(kept),
			"max_messages", c.maxMessages,
		)
	}
}

// truncateMessages truncates individual messages to bring context under the limit.
func (c *Context) truncateMessages() {
	maxChars := c.maxTokens * 4
	totalChars := 0
	for _, m := range c.messages {
		totalChars += len(m.Content)
	}

	// Proportionally truncate all non-system messages
	for i, m := range c.messages {
		if m.Role == "system" || len(m.ToolCalls) > 0 {
			continue
		}
		if totalChars <= maxChars {
			break
		}
		msgChars := len(m.Content)
		if msgChars == 0 {
			continue
		}
		// Calculate how many chars this message should keep
		ratio := float64(maxChars) / float64(totalChars)
		newLen := int(float64(msgChars) * ratio)
		if newLen < 1 {
			newLen = 1
		}
		c.messages[i].Content = m.Content[:newLen]
		totalChars = totalChars - msgChars + newLen
	}
}

// SetSystemMessage sets the base system prompt. This is the fixed identity
// part that persists across the conversation.
func (c *Context) SetSystemMessage(content string) {
	c.baseSystemPrompt = content
	c.rebuildSystemMessage()
}

// SetAgentNote injects the agent's working-memory note into the system message.
// Content is placed between the base system prompt and the search context so
// the cache prefix (base + note) is stable within a turn. Pass "" to remove.
func (c *Context) SetAgentNote(content string) {
	c.agentNoteContent = content
	c.rebuildSystemMessage()
}

// SetSearchContext refreshes the dynamic search context appended to the system message.
// Called on each message with new search results — replaces the previous search context.
func (c *Context) SetSearchContext(content string) {
	c.searchContext = content
	c.rebuildSystemMessage()
}

// rebuildSystemMessage combines baseSystemPrompt + agentNoteContent + searchContext into the actual system message.
func (c *Context) rebuildSystemMessage() {
	base := c.baseSystemPrompt
	if c.agentNoteContent != "" {
		base += "\n\n## Your current note (working memory)\n\n" + c.agentNoteContent + "\n"
	}

	var content string
	if base != "" && c.searchContext != "" {
		content = base + "\n\n" + c.searchContext
	} else if base != "" {
		content = base
	} else if c.searchContext != "" {
		content = c.searchContext
	}
	if content == "" {
		return
	}
	if len(c.messages) > 0 && c.messages[0].Role == "system" {
		c.messages[0].Content = content
		return
	}
	c.messages = append([]llm.Message{{Role: "system", Content: content}}, c.messages...)
}

// LatestUserMessageText returns the Content of the most recent user-role
// message in the conversation, or "" if no user message exists yet
// (cold start — only system prompt loaded). Used by the per-turn
// ToolsProvider closure to drive Qdrant tool retrieval (Phase 2 D-23).
func (c *Context) LatestUserMessageText() string {
	msgs := c.Messages()
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	return ""
}

// Messages returns the current message list. When a rolling summary exists it is
// folded into the system message at [0] (appended after a blank line) so the
// LLM sees exactly ONE role=system message per turn — a requirement for maximum
// prompt-cache hit rate (InjectSystemExtras / picobot §3).
func (c *Context) Messages() []llm.Message {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.summary == "" {
		return c.messages
	}
	summaryText := "Summary of earlier conversation:\n" + c.summary
	if len(c.messages) > 0 && c.messages[0].Role == "system" {
		result := make([]llm.Message, len(c.messages))
		copy(result, c.messages)
		result[0].Content += "\n\n" + summaryText
		return result
	}
	return append([]llm.Message{{Role: "system", Content: summaryText}}, c.messages...)
}

// EstimatedTokens returns a rough token estimate (4 chars per token).
func (c *Context) EstimatedTokens() int {
	total := 0
	for _, m := range c.Messages() {
		total += len(m.Content) / 4
	}
	return total
}

// EstimatedHistoryTokens estimates only mutable conversation history. The
// stable system prompt is excluded because it is rebuilt from durable prompt
// modules each turn and should not drive between-turn summarization.
func (c *Context) EstimatedHistoryTokens() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.estimatedHistoryTokensLocked()
}

func (c *Context) estimatedHistoryTokensLocked() int {
	total := 0
	if c.summary != "" {
		total += len("Summary of earlier conversation:\n"+c.summary) / 4
	}
	for i, m := range c.messages {
		if i == 0 && m.Role == "system" {
			continue
		}
		total += len(m.Content) / 4
	}
	return total
}

// TrackTokens adds to the running total of tokens used.
func (c *Context) TrackTokens(usage llm.TokenUsage) {
	c.totalTokensUsed += usage.TotalTokens
}

// TotalTokensUsed returns the cumulative token count.
func (c *Context) TotalTokensUsed() int {
	return c.totalTokensUsed
}

// ShouldSummarize returns true when context exceeds 80% of the token limit.
func (c *Context) ShouldSummarize() bool {
	threshold := float64(c.maxTokens) * 0.8
	return float64(c.EstimatedHistoryTokens()) > threshold
}

// IsOverLimit returns true when context exceeds the hard token limit.
func (c *Context) IsOverLimit() bool {
	return c.EstimatedHistoryTokens() > c.maxTokens
}

// MaxTokens returns the configured max context token limit.
func (c *Context) MaxTokens() int {
	return c.maxTokens
}

// Summarize triggers rolling summarization using the LLM.
// It summarizes older messages and trims them from the active context.
func (c *Context) Summarize(ctx context.Context) error {
	if c.summarizer == nil {
		c.logger.Warn("no summarizer configured, trimming oldest messages")
		c.trimOldest()
		return nil
	}

	// Build messages to summarize (skip system message)
	toSummarize := c.messages
	if len(toSummarize) > 0 && toSummarize[0].Role == "system" {
		toSummarize = toSummarize[1:]
	}

	if len(toSummarize) == 0 {
		return nil
	}

	// Summarize roughly the first half of messages
	split := toolSafeBoundary(toSummarize, len(toSummarize)/2)
	if split == 0 {
		split = 1
	}
	olderMessages := toSummarize[:split]

	var sb strings.Builder
	sb.WriteString("Summarize the following conversation concisely, preserving key facts and decisions:\n\n")
	for _, m := range olderMessages {
		fmt.Fprintf(&sb, "%s: %s\n", m.Role, m.Content)
	}

	priorSummary := ""
	if c.summary != "" {
		priorSummary = "Prior summary: " + c.summary + "\n\n"
	}

	req := llm.Request{
		Messages: []llm.Message{
			{Role: "user", Content: priorSummary + sb.String()},
		},
	}

	resp, err := c.summarizer.Send(ctx, req)
	if err != nil {
		c.logger.Error("summarization failed, trimming oldest messages instead", "error", err)
		c.trimOldest()
		return nil
	}

	c.summary = resp.Content
	c.totalTokensUsed += resp.Usage.TotalTokens

	// Keep only the messages after the split point (plus system message)
	newMessages := []llm.Message{}
	if len(c.messages) > 0 && c.messages[0].Role == "system" {
		newMessages = append(newMessages, c.messages[0])
	}
	remaining := toSummarize[split:]
	newMessages = append(newMessages, remaining...)
	c.messages = newMessages

	c.logger.Info("conversation summarized",
		"messages_summarized", len(olderMessages),
		"messages_remaining", len(remaining),
	)

	return nil
}

func (c *Context) trimOldest() {
	toSummarize := c.messages
	if len(toSummarize) > 0 && toSummarize[0].Role == "system" {
		toSummarize = toSummarize[1:]
	}

	if len(toSummarize) <= 2 {
		return
	}

	split := toolSafeBoundary(toSummarize, len(toSummarize)/2)
	newMessages := []llm.Message{}
	if len(c.messages) > 0 && c.messages[0].Role == "system" {
		newMessages = append(newMessages, c.messages[0])
	}
	newMessages = append(newMessages, toSummarize[split:]...)
	c.messages = newMessages
	c.logger.Info("trimmed oldest messages", "remaining", len(toSummarize)-split)
}

func toolSafeBoundary(messages []llm.Message, split int) int {
	if split <= 0 || split >= len(messages) {
		return split
	}

	for split < len(messages) && messages[split].Role == "tool" {
		split++
	}
	for split > 0 && split < len(messages) {
		prev := messages[split-1]
		if prev.Role != "assistant" || len(prev.ToolCalls) == 0 {
			break
		}
		remainingResults := len(prev.ToolCalls)
		for i := split; i < len(messages) && messages[i].Role == "tool" && remainingResults > 0; i++ {
			remainingResults--
			split = i + 1
		}
		if remainingResults > 0 {
			split--
			continue
		}
		break
	}
	return split
}

// Transcript returns the full conversation transcript.
func (c *Context) Transcript() []string {
	return c.transcript
}

// MessageCount returns the current number of messages in the context
// (including any system message). Use this before a turn to capture a
// baseline index, then call MessagesSince to retrieve only the new messages.
func (c *Context) MessageCount() int {
	return len(c.messages)
}

// MessagesSince returns the messages appended after the given baseline index.
// Safe to call with any index; out-of-range values return an empty slice.
func (c *Context) MessagesSince(fromIndex int) []llm.Message {
	if fromIndex >= len(c.messages) || fromIndex < 0 {
		return nil
	}
	return llm.CloneMessages(c.messages[fromIndex:])
}
