package agent

import (
	"strings"

	"github.com/aura/aura/internal/llm"
)

// UserRequestedRawOutput detects when the user explicitly asked for raw
// command output (vs a summarized answer). Used by the terminal-tool finalizer
// to decide whether to render execute_shell output verbatim or to summarize it.
func UserRequestedRawOutput(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	for _, marker := range []string{
		"output grezzo",
		"raw output",
		"output raw",
		"mostrami l'output",
		"mostra l'output",
		"stampa l'output",
		"incolla l'output",
		"esegui il comando",
		"lancia il comando",
		"run the command",
		"show me the output",
		"show raw",
		"print the output",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return strings.Contains(text, "`")
}

// LatestUserText returns the trimmed content of the last user message,
// or empty string if no user message exists.
func LatestUserText(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return ""
}
