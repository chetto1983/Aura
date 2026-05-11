package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/agentloop"
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
	// Apply the same governance passes the main loop runs on every LLM call
	// — microcompact long tool results, truncate oversized payloads, drop
	// orphan tool messages — before appending the finalization prompt. Without
	// this the finalize call sees the full accumulated context exactly when
	// it is largest, blowing the token budget for no extra signal (F-031).
	out := agentloop.ApplyGovernance(messages, 0, 0, 0)
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

// markerCategory bitmask classifies known unsafe text markers. One canonical
// table replaces three near-identical functions so adding a new marker only
// touches one place (F-033). Each marker tags which detectors trip on it;
// the public LooksLike* helpers filter by category.
type markerCategory uint8

const (
	categoryToolCall  markerCategory = 1 << iota // tool-call markup
	categoryInternal                             // internal/diagnostic noise
	categoryFinal                                // unsafe final-answer content
)

var unsafeMarkers = []struct {
	marker     string
	categories markerCategory
}{
	{"tool_calls", categoryToolCall | categoryInternal | categoryFinal},
	{`"tool_calls"`, categoryToolCall | categoryFinal},
	{"<tool_call", categoryToolCall},
	{"dsml", categoryToolCall},
	{"invoke name=", categoryToolCall},
	{"parameter name=", categoryToolCall},

	{"source_id", categoryInternal | categoryFinal},
	{"tokens_prompt", categoryInternal},
	{"tokens_completion", categoryInternal},
	{"tokens_total", categoryInternal | categoryFinal},
	{"llm_calls", categoryInternal},
	{"elapsed_ms", categoryInternal | categoryFinal},
	{"exit_code", categoryInternal},
	{"exit_code:", categoryFinal},
	{"workspace_root", categoryInternal | categoryFinal},
	{"top_dirs_in_workspace", categoryInternal | categoryFinal},
	{"/var/lib/", categoryInternal | categoryFinal},
	{"filesystem", categoryInternal},
	{"mounted on", categoryInternal},
	{"overlay", categoryInternal},
	{"tmpfs", categoryInternal},
	{"--- stderr ---", categoryInternal},
	{"stdout:", categoryInternal},
	{"stderr:", categoryInternal},
	{"cmd:", categoryInternal},
	{"cwd:", categoryInternal},

	{"memory evidence for", categoryFinal},
	{"evidence envelope:", categoryFinal},
	{`"ok":false`, categoryFinal},
}

func containsAnyMarker(text string, cat markerCategory) bool {
	lower := strings.ToLower(text)
	for _, entry := range unsafeMarkers {
		if entry.categories&cat == 0 {
			continue
		}
		if strings.Contains(lower, entry.marker) {
			return true
		}
	}
	return false
}

func LooksLikeToolCallMarkup(text string) bool {
	return containsAnyMarker(text, categoryToolCall)
}

func LooksLikeInternalToolResult(text string) bool {
	return containsAnyMarker(text, categoryInternal)
}

// LooksLikeUnsafeFinalAnswer reports content that should not reach the end
// user as-is — internal markers OR JSON-shaped bodies that also carry one
// of the internal markers (F-032). The earlier "any text shaped like JSON"
// check produced false positives when the user explicitly asked for JSON.
func LooksLikeUnsafeFinalAnswer(text string) bool {
	if containsAnyMarker(text, categoryFinal) {
		return true
	}
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 4 {
		return false
	}
	if (trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}') || (trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']') {
		// JSON shape alone is not enough; require co-occurrence with an
		// internal marker so "give me your answer as JSON" round-trips.
		return containsAnyMarker(trimmed, categoryInternal|categoryToolCall)
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
	// Neutral English fallback (F-034). The previous Italian copy was a
	// holdover from early single-language development; Aura runs in any
	// language and the fallback should not assume one. The LLM's own
	// natural-language synthesis is the higher-fidelity path; this string
	// only ships when synthesis is unavailable or unsafe.
	if toolName == "write_file" || toolName == "apply_patch" {
		return "Done. I updated the requested files and stopped the turn after the save."
	}
	if toolName == "execute_shell" {
		return "I checked the environment and have an answer, but I'm skipping the raw technical output."
	}
	return fmt.Sprintf("Done. %s completed the work.", toolName)
}

func FormatTerminalExecuteCodeResult(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "Sandbox execution completed, but no result was returned."
	}
	if looksLikeStructuredToolError(raw) {
		return "The command failed and produced no useful result to show."
	}
	body := raw
	if idx := strings.Index(body, "\n\n"); idx >= 0 {
		body = strings.TrimSpace(body[idx+2:])
	} else if strings.HasPrefix(body, "exit_code:") {
		return "Sandbox execution completed."
	}
	if strings.HasPrefix(strings.TrimSpace(body), "--- stderr ---") {
		return "The command produced no useful result to show."
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
		return body + "\n\nGenerated the requested attachments."
	}
	return body + "\n\nGenerated files: " + strings.Join(names, ", ") + "."
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
		return "File created and saved."
	}
	kind := strings.TrimPrefix(toolName, "create_")
	if kind == "" {
		kind = "file"
	}
	var sb strings.Builder
	if strings.TrimSpace(resp.Filename) != "" {
		fmt.Fprintf(&sb, "Created %s file `%s`", strings.ToUpper(kind), resp.Filename)
	} else {
		fmt.Fprintf(&sb, "Created %s file", strings.ToUpper(kind))
	}
	if resp.SizeBytes > 0 {
		fmt.Fprintf(&sb, " (%d bytes)", resp.SizeBytes)
	}
	if resp.Delivered {
		sb.WriteString(" and sent it here")
	}
	if resp.Duplicate {
		sb.WriteString(" (already existed)")
	}
	sb.WriteString(".")
	return sb.String()
}
