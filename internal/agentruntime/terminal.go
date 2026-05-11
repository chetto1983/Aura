package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/llm"
)

type TerminalFinalizationInput struct {
	Messages      []llm.Message
	TerminalTool  string
	RawToolResult string
	Model         string
	Send          func(context.Context, llm.Request) (llm.Response, error)
	RecordUsage   func(llm.TokenUsage)
	EstimateCost  func(llm.TokenUsage) float64
}

type TerminalFinalizationResult struct {
	Text             string
	Fallback         bool
	Err              error
	Usage            llm.TokenUsage
	TokensPrompt     int
	TokensCompletion int
	TokensTotal      int
	CostUSD          float64
}

func FinalizeTerminalTool(ctx context.Context, in TerminalFinalizationInput) TerminalFinalizationResult {
	if in.Send == nil {
		return TerminalFinalizationResult{
			Text:     TerminalToolFallbackResponse(in.TerminalTool, in.RawToolResult),
			Fallback: true,
		}
	}
	resp, err := in.Send(ctx, llm.Request{
		Messages: TerminalToolFinalizationMessages(in.Messages, in.TerminalTool),
		Model:    in.Model,
		Tools:    nil,
	})
	result := TerminalFinalizationResult{}
	if err != nil {
		result.Err = err
		result.Text = TerminalToolFallbackResponse(in.TerminalTool, in.RawToolResult)
		result.Fallback = true
		return result
	}
	result.Usage = resp.Usage
	result.TokensPrompt = resp.Usage.PromptTokens
	result.TokensCompletion = resp.Usage.CompletionTokens
	result.TokensTotal = resp.Usage.TotalTokens
	if in.EstimateCost != nil {
		result.CostUSD = in.EstimateCost(resp.Usage)
	}
	if in.RecordUsage != nil {
		in.RecordUsage(resp.Usage)
	}
	text := strings.TrimSpace(resp.Content)
	if text == "" || resp.HasToolCalls || LooksLikeToolCallMarkup(text) {
		result.Text = TerminalToolFallbackResponse(in.TerminalTool, in.RawToolResult)
		result.Fallback = true
		return result
	}
	result.Text = text
	return result
}

func TerminalToolFinalizationMessages(messages []llm.Message, terminalTool string) []llm.Message {
	out := append([]llm.Message(nil), messages...)
	toolName := strings.TrimSpace(terminalTool)
	if toolName == "" {
		toolName = "the terminal tool"
	}
	content := fmt.Sprintf("The terminal tool %q completed. Do not call tools. Do not emit JSON, XML, DSML, or tool-call markup. Summarize the completed work for the user in their language using only the tool results already present above.", toolName)
	if toolName == "search_memory" {
		content = "search_memory returned a recency-weighted hit list. Do not call tools. Do not repeat the list header or scores. Answer the user's original request in their language, using the hits as background. Cite [[slug]] for wiki pages and src_xxxx for sources when relevant. Keep it concise."
	}
	out = append(out, llm.Message{
		Role:    "user",
		Content: content,
	})
	return out
}

func LooksLikeToolCallMarkup(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "tool_calls") ||
		strings.Contains(lower, "dsml") ||
		strings.Contains(lower, "invoke name=") ||
		strings.Contains(lower, "parameter name=") ||
		strings.Contains(lower, "<tool_call") ||
		strings.Contains(lower, `"tool_calls"`)
}

func LooksLikeInternalToolResult(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"source_id",
		"tokens_prompt",
		"tokens_completion",
		"tokens_total",
		"llm_calls",
		"tool_calls",
		"elapsed_ms",
		"exit_code",
		"workspace_root",
		"top_dirs_in_workspace",
		"filesystem",
		"mounted on",
		"/var/lib/",
		"overlay",
		"tmpfs",
		"--- stderr ---",
		"stdout:",
		"stderr:",
		"cmd:",
		"cwd:",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func LooksLikeUnsafeFinalAnswer(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"memory evidence for",
		"evidence envelope:",
		"exit_code:",
		"elapsed_ms",
		"source_id",
		"tokens_total",
		`"ok":false`,
		`"tool_calls"`,
		"tool_calls",
		"workspace_root",
		"top_dirs_in_workspace",
		"/var/lib/",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	// Raw JSON body looks unsafe — the LLM should synthesize it into prose
	// rather than dumping curl/API responses to the user.
	trimmed := strings.TrimSpace(text)
	if len(trimmed) >= 4 {
		if (trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}') || (trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']') {
			return true
		}
	}
	return false
}

func TerminalToolFallbackResponse(terminalTool, rawToolResult string) string {
	raw := strings.TrimSpace(rawToolResult)
	if raw != "" && !LooksLikeToolCallMarkup(raw) && !LooksLikeInternalToolResult(raw) {
		return raw
	}
	toolName := strings.TrimSpace(terminalTool)
	if toolName == "" {
		toolName = "the terminal tool"
	}
	if toolName == "write_file" || toolName == "apply_patch" {
		return "Fatto: ho aggiornato i file richiesti e ho fermato il turno dopo il salvataggio."
	}
	if toolName == "execute_shell" {
		return "Ho controllato l'ambiente e ho una risposta, ma evito di incollarti l'output tecnico grezzo."
	}
	return fmt.Sprintf("Fatto: %s ha completato il lavoro.", toolName)
}

func FormatTerminalExecuteCodeResult(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "Sandbox execution completed, but no result was returned."
	}
	if looksLikeStructuredToolError(raw) {
		return "Il comando non e riuscito e non ha prodotto un risultato utile da mostrare."
	}
	body := raw
	if idx := strings.Index(body, "\n\n"); idx >= 0 {
		body = strings.TrimSpace(body[idx+2:])
	} else if strings.HasPrefix(body, "exit_code:") {
		return "Sandbox execution completed."
	}
	if strings.HasPrefix(strings.TrimSpace(body), "--- stderr ---") {
		return "Il comando non ha prodotto un risultato utile da mostrare."
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

func looksLikeStructuredToolError(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return strings.Contains(lower, `"ok":false`) && strings.Contains(lower, `"error":`)
}

func artifactNamesFromSandboxResult(artifacts string) []string {
	var names []string
	for _, line := range strings.Split(artifacts, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		if line == "" {
			continue
		}
		if idx := strings.Index(line, " ("); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line != "" {
			names = append(names, line)
		}
	}
	return names
}

func IsFileGenerationTool(name string) bool {
	switch name {
	case "create_docx", "create_xlsx", "create_pdf":
		return true
	default:
		return false
	}
}

func FormatTerminalFileResult(toolName, raw string) string {
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
