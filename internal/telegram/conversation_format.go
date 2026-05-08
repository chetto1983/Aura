package telegram

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/llm"
)

func terminalToolFinalizationMessages(messages []llm.Message, terminalTool string) []llm.Message {
	out := append([]llm.Message(nil), messages...)
	toolName := strings.TrimSpace(terminalTool)
	if toolName == "" {
		toolName = "the terminal tool"
	}
	out = append(out, llm.Message{
		Role:    "user",
		Content: fmt.Sprintf("The terminal tool %q completed. Do not call tools. Do not emit JSON, XML, DSML, or tool-call markup. Summarize the completed work for the user in their language using only the tool results already present above.", toolName),
	})
	return out
}

func looksLikeToolCallMarkup(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "tool_calls") ||
		strings.Contains(lower, "dsml") ||
		strings.Contains(lower, "invoke name=") ||
		strings.Contains(lower, "parameter name=") ||
		strings.Contains(lower, "<tool_call") ||
		strings.Contains(lower, `"tool_calls"`)
}

func looksLikeInternalToolResult(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "source_id") ||
		strings.Contains(lower, "tokens_prompt") ||
		strings.Contains(lower, "tokens_completion") ||
		strings.Contains(lower, "tokens_total") ||
		strings.Contains(lower, "llm_calls") ||
		strings.Contains(lower, "tool_calls") ||
		strings.Contains(lower, "elapsed_ms") ||
		strings.Contains(lower, "exit_code")
}

func terminalToolFallbackResponse(terminalTool, rawToolResult string) string {
	raw := strings.TrimSpace(rawToolResult)
	if raw != "" && !looksLikeToolCallMarkup(raw) && !looksLikeInternalToolResult(raw) {
		return raw
	}
	toolName := strings.TrimSpace(terminalTool)
	if toolName == "" {
		toolName = "the terminal tool"
	}
	if toolName == "write_wiki" {
		return "Fatto: ho aggiornato la wiki e ho fermato il turno dopo il salvataggio."
	}
	return fmt.Sprintf("Fatto: %s ha completato il lavoro.", toolName)
}

func formatTerminalExecuteCodeResult(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "Sandbox execution completed, but no result was returned."
	}
	body := raw
	if idx := strings.Index(body, "\n\n"); idx >= 0 {
		body = strings.TrimSpace(body[idx+2:])
	}
	artifacts := ""
	if idx := strings.Index(body, "\n\nartifacts:"); idx >= 0 {
		artifacts = strings.TrimSpace(body[idx+len("\n\nartifacts:"):])
		body = strings.TrimSpace(body[:idx])
	}
	if body == "" {
		body = "Sandbox execution completed."
	}
	if artifacts == "" {
		return body
	}
	names := artifactNamesFromSandboxResult(artifacts)
	if len(names) == 0 {
		return body + "\n\nHo generato gli allegati richiesti."
	}
	return body + "\n\nFile generati: " + strings.Join(names, ", ") + "."
}

func isFileGenerationTool(name string) bool {
	switch name {
	case "create_docx", "create_xlsx", "create_pdf":
		return true
	default:
		return false
	}
}

func formatTerminalFileResult(toolName, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "File created, but no metadata was returned."
	}
	var resp struct {
		SourceID  string `json:"source_id"`
		Filename  string `json:"filename"`
		SizeBytes int64  `json:"size_bytes"`
		Delivered bool   `json:"delivered"`
		Duplicate bool   `json:"duplicate"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return "File creato e salvato."
	}
	kind := strings.TrimPrefix(toolName, "create_")
	if kind == "" {
		kind = "file"
	}
	var sb strings.Builder
	if strings.TrimSpace(resp.Filename) != "" {
		fmt.Fprintf(&sb, "Ho creato il file %s `%s`", strings.ToUpper(kind), resp.Filename)
	} else {
		fmt.Fprintf(&sb, "Ho creato il file %s", strings.ToUpper(kind))
	}
	if resp.SizeBytes > 0 {
		fmt.Fprintf(&sb, " (%d bytes)", resp.SizeBytes)
	}
	if resp.Delivered {
		sb.WriteString(" e l'ho inviato qui")
	}
	if resp.Duplicate {
		sb.WriteString(" (era gia' presente)")
	}
	sb.WriteString(".")
	return sb.String()
}
