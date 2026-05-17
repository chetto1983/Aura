package agent

import (
	"errors"
	"slices"
	"strings"

	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/llm"
)

func initialMessages(task Task) ([]llm.Message, error) {
	if len(task.Messages) > 0 {
		return llm.CloneMessages(task.Messages), nil
	}
	prompt := strings.TrimSpace(task.Prompt)
	if prompt == "" {
		return nil, errors.New("agent runner: prompt or messages required")
	}
	messages := make([]llm.Message, 0, 2)
	if system := strings.TrimSpace(task.SystemPrompt); system != "" {
		messages = append(messages, llm.Message{Role: "system", Content: system})
	}
	messages = append(messages, llm.Message{Role: "user", Content: prompt})
	return messages, nil
}

// cleanToolList trims, lowercases, and dedupes the LLM-or-config-supplied
// tool allowlist. Lowercasing is defense-in-depth: tool names in the registry
// are canonical snake_case, but if a future schema mismatch or operator typo
// produces "Web_Fetch" the allowlist would otherwise silently miss
// "web_fetch" emitted by the LLM (F-018).
func cleanToolList(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

// limitToolContent truncates content to maxChars runes. Zero means unlimited.
func limitToolContent(content string, maxChars int) string {
	if maxChars <= 0 {
		return content
	}
	runes := []rune(content)
	if len(runes) <= maxChars {
		return content
	}
	if maxChars <= 16 {
		return string(runes[:maxChars])
	}
	return string(runes[:maxChars-15]) + "\n...[truncated]"
}

// runTaskToolDefs returns the subset of tool definitions from reg that appear
// in the allowlist. RunTask uses it to filter tools without shared task runner
// state.
func runTaskToolDefs(reg *tools.Registry, allowlist []string) []llm.ToolDefinition {
	if reg == nil || len(allowlist) == 0 {
		return nil
	}
	defs := reg.Definitions()
	out := make([]llm.ToolDefinition, 0, len(defs))
	for _, def := range defs {
		if slices.Contains(allowlist, strings.ToLower(def.Name)) {
			out = append(out, def)
		}
	}
	return out
}
