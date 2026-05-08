package telegram

import (
	"github.com/aura/aura/internal/agentruntime"
	"github.com/aura/aura/internal/llm"
)

func terminalToolFinalizationMessages(messages []llm.Message, terminalTool string) []llm.Message {
	return agentruntime.TerminalToolFinalizationMessages(messages, terminalTool)
}

func looksLikeToolCallMarkup(text string) bool {
	return agentruntime.LooksLikeToolCallMarkup(text)
}

func terminalToolFallbackResponse(terminalTool, rawToolResult string) string {
	return agentruntime.TerminalToolFallbackResponse(terminalTool, rawToolResult)
}

func toolActivityMessage(name string) string {
	return "Sto lavorando alla richiesta..."
}

func formatTerminalExecuteCodeResult(raw string) string {
	return agentruntime.FormatTerminalExecuteCodeResult(raw)
}

func isFileGenerationTool(name string) bool {
	return agentruntime.IsFileGenerationTool(name)
}

func formatTerminalFileResult(toolName, raw string) string {
	return agentruntime.FormatTerminalFileResult(toolName, raw)
}
