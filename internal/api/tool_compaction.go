package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/agent/governance"
	toolregistry "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/tokenjuice"
)

const (
	toolCompactionBodyLimit      = 4 << 20 // 4 MiB: large enough for real tool output, bounded for the API.
	maxToolCompactionInlineChars = 20_000
	maxToolCompactionCommand     = 8_192
	maxToolCompactionArgv        = 64
	maxToolCompactionArg         = 2_048
)

type toolCompactionRequest struct {
	Mode string `json:"mode,omitempty"`

	ToolName string         `json:"tool_name"`
	Args     map[string]any `json:"args,omitempty"`
	Argv     []string       `json:"argv,omitempty"`
	Command  string         `json:"command,omitempty"`
	Stdout   string         `json:"stdout,omitempty"`
	Stderr   string         `json:"stderr,omitempty"`
	Content  string         `json:"content,omitempty"`
	ExitCode *int           `json:"exit_code,omitempty"`

	MaxInlineChars     int     `json:"max_inline_chars,omitempty"`
	ToolResultMaxChars int     `json:"tool_result_max_chars,omitempty"`
	ForceRuleID        string  `json:"force_rule_id,omitempty"`
	Raw                bool    `json:"raw,omitempty"`
	MinInputBytes      int     `json:"min_input_bytes,omitempty"`
	MinRatio           float64 `json:"min_ratio,omitempty"`
}

type toolCompactionResponse struct {
	Mode       string                `json:"mode"`
	InlineText string                `json:"inline_text"`
	Applied    bool                  `json:"applied"`
	RuleID     string                `json:"rule_id"`
	Stats      toolCompactionStats   `json:"stats"`
	Warnings   []string              `json:"warnings,omitempty"`
	Stages     []toolCompactionStage `json:"stages,omitempty"`
}

type toolCompactionStage struct {
	Name    string `json:"name"`
	Text    string `json:"text"`
	Bytes   int    `json:"bytes"`
	Chars   int    `json:"chars"`
	Applied bool   `json:"applied"`
	RuleID  string `json:"rule_id,omitempty"`
}

type toolCompactionStats struct {
	ToolName       string  `json:"tool_name"`
	OriginalBytes  int     `json:"original_bytes"`
	CompactedBytes int     `json:"compacted_bytes"`
	OriginalChars  int     `json:"original_chars"`
	CompactedChars int     `json:"compacted_chars"`
	SavedBytes     int     `json:"saved_bytes"`
	SavedChars     int     `json:"saved_chars"`
	Family         string  `json:"family"`
	Confidence     float64 `json:"confidence"`
	Ratio          float64 `json:"ratio"`
}

func handleToolCompact(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req toolCompactionRequest
		if err := decodeToolCompactionBody(w, r, &req); err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid JSON body")
			return
		}
		resp, err := compactToolRequest(req)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, resp)
	}
}

func decodeToolCompactionBody(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, toolCompactionBodyLimit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("body must contain one JSON object")
	}
	return nil
}

func compactToolRequest(req toolCompactionRequest) (toolCompactionResponse, error) {
	mode, err := validateToolCompactionRequest(req)
	if err != nil {
		return toolCompactionResponse{}, err
	}

	switch mode {
	case "tokenjuice":
		return compactWithTokenJuice(req), nil
	case "tool_result":
		return compactToolResultPreview(req), nil
	case "context":
		return compactContextPipeline(req), nil
	default:
		return toolCompactionResponse{}, fmt.Errorf("unsupported mode %q", mode)
	}
}

func validateToolCompactionRequest(req toolCompactionRequest) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "tokenjuice"
	}
	if mode != "tokenjuice" && mode != "tool_result" && mode != "context" {
		return "", fmt.Errorf("unsupported mode %q", req.Mode)
	}
	if strings.TrimSpace(req.ToolName) == "" {
		return "", errors.New("tool_name is required")
	}
	argv := toolCompactionArgv(req)
	if len(argv) > maxToolCompactionArgv {
		return "", fmt.Errorf("argv has %d entries, max %d", len(argv), maxToolCompactionArgv)
	}
	for i, arg := range argv {
		if len(arg) > maxToolCompactionArg {
			return "", fmt.Errorf("argv[%d] is too long", i)
		}
	}
	if len(toolCompactionCommand(req)) > maxToolCompactionCommand {
		return "", fmt.Errorf("command is too long")
	}
	if req.MaxInlineChars < 0 || req.MaxInlineChars > maxToolCompactionInlineChars {
		return "", fmt.Errorf("max_inline_chars must be between 0 and %d", maxToolCompactionInlineChars)
	}
	if req.ToolResultMaxChars < 0 || req.ToolResultMaxChars > maxToolCompactionInlineChars {
		return "", fmt.Errorf("tool_result_max_chars must be between 0 and %d", maxToolCompactionInlineChars)
	}
	if req.MinInputBytes < 0 || req.MinInputBytes > toolCompactionBodyLimit {
		return "", fmt.Errorf("min_input_bytes must be between 0 and %d", toolCompactionBodyLimit)
	}
	if req.MinRatio < 0 || req.MinRatio > 1 {
		return "", errors.New("min_ratio must be between 0 and 1")
	}
	return mode, nil
}

func compactWithTokenJuice(req toolCompactionRequest) toolCompactionResponse {
	stdout, stderr := toolCompactionStreams(req)
	result := tokenjuice.Compact(tokenjuice.Input{
		ToolName: req.ToolName,
		Argv:     toolCompactionArgv(req),
		Command:  toolCompactionCommand(req),
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: req.ExitCode,
	}, tokenjuice.Options{
		MaxInlineChars: req.MaxInlineChars,
		ForceRuleID:    strings.TrimSpace(req.ForceRuleID),
		Raw:            req.Raw,
		MinInputBytes:  req.MinInputBytes,
		MinRatio:       req.MinRatio,
	})

	return toolCompactionResponse{
		Mode:       "tokenjuice",
		InlineText: result.InlineText,
		Applied:    result.Applied,
		RuleID:     result.RuleID,
		Stats:      statsFromTokenJuice(result.Stats),
	}
}

func compactToolResultPreview(req toolCompactionRequest) toolCompactionResponse {
	input := toolCompactionInput(req)
	maxChars := toolResultCompactionMaxChars(req)
	output := conversation.CompactToolResultContent(req.ToolName, input, maxChars)
	return toolCompactionResponse{
		Mode:       "tool_result",
		InlineText: output,
		Applied:    output != input,
		RuleID:     "conversation/tool-result-preview",
		Stats:      statsFromStrings(req.ToolName, input, output, "tool-result", 1),
	}
}

func compactContextPipeline(req toolCompactionRequest) toolCompactionResponse {
	stdout, stderr := toolCompactionStreams(req)
	input := combineToolCompactionStreams(stdout, stderr)
	stages := []toolCompactionStage{
		newToolCompactionStage("raw_input", input, false, ""),
	}

	tj := tokenjuice.Compact(tokenjuice.Input{
		ToolName: req.ToolName,
		Argv:     toolCompactionArgv(req),
		Command:  toolCompactionCommand(req),
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: req.ExitCode,
	}, tokenjuice.Options{
		MaxInlineChars: req.MaxInlineChars,
		ForceRuleID:    strings.TrimSpace(req.ForceRuleID),
		Raw:            req.Raw,
		MinInputBytes:  req.MinInputBytes,
		MinRatio:       req.MinRatio,
	})
	afterTokenJuice := tj.InlineText
	tokenJuiceRuleID := ""
	if tj.Applied {
		tokenJuiceRuleID = tj.RuleID
	}
	stages = append(stages, newToolCompactionStage("tokenjuice", afterTokenJuice, tj.Applied, tokenJuiceRuleID))

	freshBudget := toolregistry.FreshToolResultBudgetBytes
	if governance.IsEvidenceClassTool(req.ToolName, req.Args) {
		freshBudget = toolregistry.FreshEvidenceToolResultBudgetBytes
	}
	afterFreshBudget, freshOutcome := toolregistry.ApplyFreshToolResultBudget(afterTokenJuice, freshBudget)
	freshRuleID := ""
	if freshOutcome.Truncated {
		freshRuleID = "agent/fresh-tool-result-budget"
	}
	stages = append(stages, newToolCompactionStage("fresh_tool_budget", afterFreshBudget, freshOutcome.Truncated, freshRuleID))

	wrapped := agent.WrapUntrustedToolResult(req.ToolName, afterFreshBudget)
	wrappedApplied := wrapped != afterFreshBudget
	wrappedRuleID := ""
	if wrappedApplied {
		wrappedRuleID = "agent/untrusted-tool-envelope"
	}
	stages = append(stages, newToolCompactionStage("wrapped_context", wrapped, wrappedApplied, wrappedRuleID))

	maxChars := toolResultCompactionMaxChars(req)
	finalText, completedChanged := compactCompletedToolResultContext(req.ToolName, wrapped, maxChars)
	completedApplied := completedChanged > 0
	completedRuleID := ""
	if completedApplied {
		completedRuleID = "conversation/tool-result-preview"
	}
	stages = append(stages, newToolCompactionStage("completed_tool_compaction", finalText, completedApplied, completedRuleID))

	ruleID := contextPipelineRuleID(tj, freshOutcome.Truncated, wrappedApplied, completedApplied)
	return toolCompactionResponse{
		Mode:       "context",
		InlineText: finalText,
		Applied:    tj.Applied || freshOutcome.Truncated || wrappedApplied || completedApplied,
		RuleID:     ruleID,
		Stats:      statsFromStrings(req.ToolName, input, finalText, "context-pipeline", 1),
		Stages:     stages,
	}
}

func compactCompletedToolResultContext(toolName, content string, maxChars int) (string, int) {
	ctx := conversation.NewContext(conversation.Config{})
	const toolCallID = "call_1"
	ctx.AddUserMessage("tool compaction probe")
	ctx.AddAssistantToolCallMessage("", []llm.ToolCall{{ID: toolCallID, Name: toolName}})
	ctx.AddToolResultMessage(toolCallID, content)
	ctx.AddAssistantMessage("tool result consumed")
	changed := ctx.CompactCompletedToolResults(conversation.ToolResultCompactionPolicy{
		MaxChars:       maxChars,
		KeepRecentFull: 0,
	})
	for _, msg := range ctx.Messages() {
		if msg.Role == "tool" && msg.ToolCallID == toolCallID {
			return msg.Content, changed
		}
	}
	return content, changed
}

func toolCompactionStreams(req toolCompactionRequest) (string, string) {
	stdout := req.Stdout
	stderr := req.Stderr
	if stdout == "" && stderr == "" && req.Content != "" {
		stdout = req.Content
	}
	return stdout, stderr
}

func toolCompactionInput(req toolCompactionRequest) string {
	stdout, stderr := toolCompactionStreams(req)
	return combineToolCompactionStreams(stdout, stderr)
}

func toolResultCompactionMaxChars(req toolCompactionRequest) int {
	if req.ToolResultMaxChars > 0 {
		return req.ToolResultMaxChars
	}
	if req.MaxInlineChars > 0 {
		return req.MaxInlineChars
	}
	return 1200
}

func combineToolCompactionStreams(stdout, stderr string) string {
	input := stdout
	if stderr != "" {
		if input != "" {
			input += "\n"
		}
		input += stderr
	}
	return input
}

func toolCompactionArgv(req toolCompactionRequest) []string {
	if len(req.Argv) > 0 {
		return req.Argv
	}
	if req.Args == nil {
		return nil
	}
	if command, ok := req.Args["command"].(string); ok && strings.TrimSpace(command) != "" {
		return strings.Fields(command)
	}
	if action, ok := req.Args["action"].(string); ok && strings.TrimSpace(action) != "" {
		return []string{strings.TrimSpace(action)}
	}
	return nil
}

func toolCompactionCommand(req toolCompactionRequest) string {
	if strings.TrimSpace(req.Command) != "" {
		return req.Command
	}
	if req.Args == nil {
		return ""
	}
	command, _ := req.Args["command"].(string)
	return command
}

func contextPipelineRuleID(tj tokenjuice.Result, freshTruncated, wrapped, completedCompacted bool) string {
	var parts []string
	if tj.Applied && tj.RuleID != "" {
		parts = append(parts, tj.RuleID)
	}
	if freshTruncated {
		parts = append(parts, "agent/fresh-tool-result-budget")
	}
	if wrapped {
		parts = append(parts, "agent/untrusted-tool-envelope")
	}
	if completedCompacted {
		parts = append(parts, "conversation/tool-result-preview")
	}
	if len(parts) == 0 {
		return "context/no-op"
	}
	return strings.Join(parts, "+")
}

func newToolCompactionStage(name, text string, applied bool, ruleID string) toolCompactionStage {
	return toolCompactionStage{
		Name:    name,
		Text:    text,
		Bytes:   len([]byte(text)),
		Chars:   utf8.RuneCountInString(text),
		Applied: applied,
		RuleID:  ruleID,
	}
}

func statsFromTokenJuice(stats tokenjuice.Stats) toolCompactionStats {
	return toolCompactionStats{
		ToolName:       stats.ToolName,
		OriginalBytes:  stats.OriginalBytes,
		CompactedBytes: stats.CompactedBytes,
		OriginalChars:  stats.OriginalChars,
		CompactedChars: stats.CompactedChars,
		SavedBytes:     stats.OriginalBytes - stats.CompactedBytes,
		SavedChars:     stats.OriginalChars - stats.CompactedChars,
		Family:         stats.Family,
		Confidence:     stats.Confidence,
		Ratio:          stats.Ratio(),
	}
}

func statsFromStrings(toolName, input, output, family string, confidence float64) toolCompactionStats {
	originalBytes := len([]byte(input))
	compactedBytes := len([]byte(output))
	originalChars := utf8.RuneCountInString(input)
	compactedChars := utf8.RuneCountInString(output)
	ratio := 1.0
	if originalBytes > 0 {
		ratio = float64(compactedBytes) / float64(originalBytes)
	}
	return toolCompactionStats{
		ToolName:       toolName,
		OriginalBytes:  originalBytes,
		CompactedBytes: compactedBytes,
		OriginalChars:  originalChars,
		CompactedChars: compactedChars,
		SavedBytes:     originalBytes - compactedBytes,
		SavedChars:     originalChars - compactedChars,
		Family:         family,
		Confidence:     confidence,
		Ratio:          ratio,
	}
}
