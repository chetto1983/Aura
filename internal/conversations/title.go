package conversations

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

// titlePrompt is the system instruction for the best-effort auto-title call. It is
// English-only (project rule) and asks for a short noun phrase with no quoting or
// punctuation so the result drops straight into `aura chat list`.
const titlePrompt = "You generate a concise 4-6 word title summarizing a conversation. " +
	"Reply with the title ONLY: no quotes, no trailing punctuation, no preamble."

// titleMaxChars bounds the stored title defensively (a misbehaving model could
// stream a paragraph; the column is text but the list UI wants a short label).
const titleMaxChars = 80

// GenerateTitle is the exported entry the Runner (04-05) invokes from its
// WithoutCancel/WithTimeout/WaitGroup auto-title worker (D-A5-01). It delegates to
// the package-internal generateTitle body; the worker lifecycle (the goroutine, the
// bounded ctx, the WaitGroup join) is the Runner's, not this package's. Errors are
// returned for the caller to discard (a NULL title renders "(untitled ...)").
func GenerateTitle(ctx context.Context, client llm.Client, model string, history []llm.Message) (string, error) {
	return generateTitle(ctx, client, model, history)
}

// generateTitle is the best-effort auto-title worker BODY (D-A5-01). The Runner
// owns the WaitGroup + WithoutCancel/WithTimeout wiring and invokes this; here we
// only do the single LLM call and shape the result. Errors NEVER block chat — the
// caller discards them, leaving the title NULL (rendered "(untitled ...)").
//
// It targets the provider-neutral llm.Client.Stream and drains the channel (the
// interface contract: consumers MUST drain or the impl leaks). The prompt is a
// compact rendering of the first few turns — enough signal for a label without
// resending the whole history.
func generateTitle(ctx context.Context, client llm.Client, model string, history []llm.Message) (string, error) {
	if client == nil {
		return "", fmt.Errorf("generate title: nil client")
	}
	req := llm.Request{
		Model: model,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: titlePrompt},
			{Role: llm.RoleUser, Content: renderHistoryForTitle(history)},
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

// renderHistoryForTitle compacts the leading turns into a single prompt body. Only
// user/assistant content is used (system + tool turns are noise for a title); each
// is truncated so a giant turn cannot blow the title-call budget.
func renderHistoryForTitle(history []llm.Message) string {
	var b strings.Builder
	const perTurnCap = 500
	used := 0
	for _, m := range history {
		if m.Role != llm.RoleUser && m.Role != llm.RoleAssistant {
			continue
		}
		content := m.Content
		if len(content) > perTurnCap {
			content = content[:perTurnCap]
		}
		if content == "" {
			continue
		}
		b.WriteString(m.Role)
		b.WriteString(": ")
		b.WriteString(content)
		b.WriteString("\n")
		used++
		if used >= 6 { // a title needs only the opening of the conversation
			break
		}
	}
	return strings.TrimSpace(b.String())
}

// sanitizeTitle strips quoting/whitespace and clamps the length so a stray model
// flourish never poisons the list UI.
func sanitizeTitle(raw string) string {
	t := strings.TrimSpace(raw)
	t = strings.Trim(t, "\"'`")
	t = strings.TrimSpace(t)
	if len(t) > titleMaxChars {
		t = strings.TrimSpace(t[:titleMaxChars])
	}
	return t
}
