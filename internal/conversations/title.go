package conversations

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

// titlePrompt is the system instruction for the best-effort auto-title call. It is
// written in English and asks for a short label in the user's language.
const titlePrompt = "You generate a concise 4-6 word title summarizing a conversation from its first user message. " +
	"Use the language of that message. " +
	"Reply with the title ONLY: no quotes, no trailing punctuation, no preamble."

// titleInputCap bounds the message sent to the title model, so a pasted document cannot
// blow the title-call budget: its opening is enough signal for a label.
const titleInputCap = 500

// titleMaxChars bounds the stored title defensively (a misbehaving model could
// stream a paragraph; the column is text but the list UI wants a short label).
const titleMaxChars = 80

// GenerateTitle is the best-effort auto-title call (D-A5-01): one LLM request over the
// person's message. The Runner owns the worker lifecycle (the goroutine, the bounded
// ctx, the WaitGroup join) and persists FallbackTitle when this returns an error.
//
// It takes the message, never the history: the history's first user-role turn can be
// the injected always-on skills and profile block (injectAlwaysBlock), which named a
// member's conversation "Active skill instructions" (measured 2026-09-11).
//
// It drains the llm.Client.Stream channel: the interface contract says a consumer that
// stops early leaks the implementation's goroutine.
func GenerateTitle(ctx context.Context, client llm.Client, model, userMessage string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("generate title: nil client")
	}
	if len(userMessage) > titleInputCap {
		userMessage = userMessage[:runeStart(userMessage, titleInputCap)]
	}
	req := llm.Request{
		Model: model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: titlePrompt},
			{Role: llm.RoleUser, Content: userMessage},
		},
		Temperature: 0.3,
		MaxTokens:   32,
		Reasoning:   llm.ReasoningConfig{Effort: llm.ReasoningEffortNone},
		ToolChoice:  "none",
	}
	ch, err := client.Stream(ctx, req)
	if err != nil {
		return "", fmt.Errorf("generate title: stream: %w", err)
	}
	var b strings.Builder
	var finishReason string
	for chunk := range ch { // drain fully (interface contract)
		if chunk.Err != nil {
			return "", fmt.Errorf("generate title: stream: %w", chunk.Err)
		}
		b.WriteString(chunk.Text)
		if chunk.FinishReason != "" {
			finishReason = chunk.FinishReason
		}
	}
	if finishReason != "stop" {
		return "", fmt.Errorf("generate title: incomplete stream (finish_reason=%q)", finishReason)
	}
	title := sanitizeTitle(b.String())
	if title == "" {
		return "", fmt.Errorf("generate title: empty result")
	}
	return title, nil
}

// sanitizeTitle strips quoting/whitespace and clamps the length so a stray model
// flourish never poisons the list UI.
func sanitizeTitle(raw string) string {
	t := strings.TrimSpace(raw)
	t = strings.Trim(t, "\"'`")
	t = strings.TrimSpace(t)
	if runes := []rune(t); len(runes) > titleMaxChars {
		t = strings.TrimSpace(string(runes[:titleMaxChars]))
	}
	return t
}

// FallbackTitle names a conversation from the person's message when the title model is
// unavailable: the message with its whitespace folded, clamped like a generated title.
func FallbackTitle(userMessage string) string {
	return sanitizeTitle(strings.Join(strings.Fields(userMessage), " "))
}
