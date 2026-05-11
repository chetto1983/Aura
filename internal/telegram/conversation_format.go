package telegram

import (
	"github.com/aura/aura/internal/agentruntime"
	"github.com/aura/aura/internal/llm"
)

// terminalToolFinalizationMessages composes the message list the LLM sees on
// terminal-tool finalize. Retained as a passthrough so tests can exercise the
// prompt shape without reaching into agentruntime directly.
func terminalToolFinalizationMessages(messages []llm.Message, terminalTool string) []llm.Message {
	return agentruntime.TerminalToolFinalizationMessages(messages, terminalTool)
}

func looksLikeToolCallMarkup(text string) bool {
	return agentruntime.LooksLikeToolCallMarkup(text)
}

// toolActivityMessage is the placeholder text shown while a tool turn is in
// flight. Italian because it is a fixed UX string the user sees during
// long-running operations and Aura's primary deployment language is Italian.
func toolActivityMessage(name string) string {
	return "Sto lavorando alla richiesta..."
}

func isFileGenerationTool(name string) bool {
	return agentruntime.IsFileGenerationTool(name)
}
