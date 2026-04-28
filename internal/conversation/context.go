package conversation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aura/aura/internal/llm"
)

// Context manages the conversation state for a single conversation.
type Context struct {
	messages        []llm.Message
	summary         string
	transcript      []string
	maxTokens       int
	summarizer      llm.Client
	logger          *slog.Logger
	totalTokensUsed int
}

// Config holds configuration for conversation context.
type Config struct {
	MaxTokens  int
	Summarizer llm.Client
	Logger     *slog.Logger
}

// NewContext creates a new conversation context.
func NewContext(cfg Config) *Context {
	maxTokens := cfg.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4000
	}
	return &Context{
		maxTokens:  maxTokens,
		summarizer: cfg.Summarizer,
		logger:     cfg.Logger,
	}
}

// AddUserMessage appends a user message and manages context if needed.
func (c *Context) AddUserMessage(content string) {
	c.messages = append(c.messages, llm.Message{Role: "user", Content: content})
	c.transcript = append(c.transcript, "user: "+content)
}

// AddAssistantMessage appends an assistant message.
func (c *Context) AddAssistantMessage(content string) {
	c.messages = append(c.messages, llm.Message{Role: "assistant", Content: content})
	c.transcript = append(c.transcript, "assistant: "+content)
}

// EnforceLimit ensures the context stays within MAX_CONTEXT_TOKENS.
// If over 80%, it triggers summarization. If still over the hard limit,
// it repeatedly trims oldest messages. As a last resort, it truncates
// individual messages to fit within the limit.
func (c *Context) EnforceLimit(ctx context.Context) error {
	if !c.IsOverLimit() && !c.ShouldSummarize() {
		return nil
	}

	// First attempt: summarize if we can
	if c.ShouldSummarize() {
		if err := c.Summarize(ctx); err != nil {
			return err
		}
	}

	// If still over the hard limit after summarization, trim aggressively
	trimPasses := 0
	prevTokenCount := c.EstimatedTokens()
	for c.IsOverLimit() && trimPasses < 10 {
		c.trimOldest()
		trimPasses++
		// If trimming made no progress, break to avoid infinite loop
		currentTokens := c.EstimatedTokens()
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
			"estimated_tokens", c.EstimatedTokens(),
			"max_tokens", c.maxTokens,
		)
	}

	return nil
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
		if m.Role == "system" {
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

// SetSystemMessage sets or replaces the system message at the start.
func (c *Context) SetSystemMessage(content string) {
	if len(c.messages) > 0 && c.messages[0].Role == "system" {
		c.messages[0].Content = content
		return
	}
	c.messages = append([]llm.Message{{Role: "system", Content: content}}, c.messages...)
}

// Messages returns the current message list, prepending the summary if one exists.
func (c *Context) Messages() []llm.Message {
	if c.summary == "" {
		return c.messages
	}
	summaryMsg := llm.Message{Role: "system", Content: "Summary of earlier conversation:\n" + c.summary}
	// Replace the system message if one exists, otherwise prepend
	if len(c.messages) > 0 && c.messages[0].Role == "system" {
		result := make([]llm.Message, 0, len(c.messages)+1)
		result = append(result, c.messages[0])
		result = append(result, summaryMsg)
		result = append(result, c.messages[1:]...)
		return result
	}
	return append([]llm.Message{summaryMsg}, c.messages...)
}

// EstimatedTokens returns a rough token estimate (4 chars per token).
func (c *Context) EstimatedTokens() int {
	total := 0
	for _, m := range c.Messages() {
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
	return float64(c.EstimatedTokens()) > threshold
}

// IsOverLimit returns true when context exceeds the hard token limit.
func (c *Context) IsOverLimit() bool {
	return c.EstimatedTokens() > c.maxTokens
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
	split := len(toSummarize) / 2
	if split == 0 {
		split = 1
	}
	olderMessages := toSummarize[:split]

	var sb strings.Builder
	sb.WriteString("Summarize the following conversation concisely, preserving key facts and decisions:\n\n")
	for _, m := range olderMessages {
		sb.WriteString(fmt.Sprintf("%s: %s\n", m.Role, m.Content))
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

	split := len(toSummarize) / 2
	newMessages := []llm.Message{}
	if len(c.messages) > 0 && c.messages[0].Role == "system" {
		newMessages = append(newMessages, c.messages[0])
	}
	newMessages = append(newMessages, toSummarize[split:]...)
	c.messages = newMessages
	c.logger.Info("trimmed oldest messages", "remaining", len(toSummarize)-split)
}

// Transcript returns the full conversation transcript.
func (c *Context) Transcript() []string {
	return c.transcript
}
