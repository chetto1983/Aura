package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	governance "github.com/aura/aura/internal/agent/governance"
	"github.com/aura/aura/internal/conversation"
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

// FinalizeTerminalTool asks the LLM to summarize the just-completed terminal
// tool turn in natural prose. There is NO hardcoded fallback: if the first
// synthesis fails (empty, tool-call markup, or HasToolCalls), we retry once
// with a stricter prompt. If both attempts fail, Text is returned empty and
// Fallback=true — callers decide what to do (Telegram simply sends nothing,
// the user already sees any delivered artifacts).
//
// The agent is meant to sound like a copilot, not a robot reciting canned
// strings. Trusting the LLM with a strong prompt produces better UX than
// any handwritten template ever could.
func FinalizeTerminalTool(ctx context.Context, in TerminalFinalizationInput) TerminalFinalizationResult {
	result := TerminalFinalizationResult{}
	if in.Send == nil {
		result.Fallback = true
		return result
	}

	attempts := []llm.Message{
		terminalFinalizationPrompt(in.TerminalTool, false),
		terminalFinalizationPrompt(in.TerminalTool, true),
	}
	baseMessages := governance.Apply(in.Messages, 0, 0, 0)

	for i, prompt := range attempts {
		messages := append(append([]llm.Message(nil), baseMessages...), prompt)
		resp, err := in.Send(ctx, llm.Request{Messages: messages, Model: in.Model, Tools: nil})
		if err != nil {
			result.Err = err
			continue
		}
		result.Usage = resp.Usage
		result.TokensPrompt += resp.Usage.PromptTokens
		result.TokensCompletion += resp.Usage.CompletionTokens
		result.TokensTotal += resp.Usage.TotalTokens
		if in.EstimateCost != nil {
			result.CostUSD += in.EstimateCost(resp.Usage)
		}
		if in.RecordUsage != nil {
			in.RecordUsage(resp.Usage)
		}
		text := strings.TrimSpace(resp.Content)
		if text == "" || resp.HasToolCalls || LooksLikeToolCallMarkup(text) {
			// Synthesis failed on this attempt; retry with stricter prompt
			// unless we just exhausted attempts.
			if i+1 < len(attempts) {
				continue
			}
			result.Fallback = true
			result.Text = ""
			return result
		}
		result.Text = text
		return result
	}
	result.Fallback = true
	return result
}

// terminalFinalizationPrompt returns the user-message that asks the LLM to
// synthesize a final answer from the tool results already in the context.
// strict=true tightens the language requirements on retry.
func terminalFinalizationPrompt(terminalTool string, strict bool) llm.Message {
	toolName := strings.TrimSpace(terminalTool)
	if toolName == "" {
		toolName = "the terminal tool"
	}
	content := fmt.Sprintf("The %q tool just finished. Do not call tools. Answer the user in their language, conversationally, using the tool results above. Describe what you did and what you found. No JSON, no tool-call markup, no internal markers like exit_code or source_id — just natural prose.", toolName)
	if strict {
		content = "RETRY. " + content + " Your previous attempt was empty or contained tool-call markup. Reply now in plain prose; the user is waiting."
	}
	return llm.Message{Role: "user", Content: content}
}

var toolCallMarkupMarkers = []string{
	"tool_calls",
	`"tool_calls"`,
	"<tool_call",
	"dsml",
	"invoke name=",
	"parameter name=",
}

func LooksLikeToolCallMarkup(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range toolCallMarkupMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// extractTextResponsePseudoCall handles the common model slip where the
// terminal tool is emitted as plain text instead of a provider tool call:
//
//	text_response(text="final reply")
//
// It is intentionally narrow and only accepts a standalone pseudo-call line.
// Other tool names still flow through the phantom guard.
func extractTextResponsePseudoCall(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", false
	}
	if text, ok := parseTextResponsePseudoCallLine(trimmed); ok {
		return text, true
	}
	lines := strings.Split(strings.ReplaceAll(trimmed, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		return parseTextResponsePseudoCallLine(line)
	}
	return "", false
}

func parseTextResponsePseudoCallLine(line string) (string, bool) {
	const prefix = "text_response("
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, prefix) || !strings.HasSuffix(line, ")") {
		return "", false
	}
	inner := strings.TrimSpace(line[len(prefix) : len(line)-1])
	if strings.HasPrefix(inner, "{") {
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(inner), &payload); err != nil {
			return "", false
		}
		text := strings.TrimSpace(payload.Text)
		return text, text != ""
	}
	if !strings.HasPrefix(inner, "text=") {
		return "", false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(inner, "text="))
	if raw == "" {
		return "", false
	}
	text, err := strconv.Unquote(raw)
	if err != nil {
		if len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\'' {
			text = strings.ReplaceAll(raw[1:len(raw)-1], `\'`, `'`)
		} else {
			return "", false
		}
	}
	text = strings.TrimSpace(text)
	return text, text != ""
}

// IsFileGenerationTool reports whether a tool name produces a user-facing
// file artifact (xlsx/docx/pdf).
func IsFileGenerationTool(name string) bool {
	switch name {
	case "create_docx", "create_xlsx", "create_pdf":
		return true
	default:
		return false
	}
}

// TerminalFinalizer provides the LLM and cost-accounting capabilities needed
// to finalize a terminal tool turn without a channel-specific dependency.
type TerminalFinalizer interface {
	FinalizeModel() string
	FinalizeChat(ctx context.Context, req llm.Request) (llm.Response, error)
	FinalizeRecordUsage(usage llm.TokenUsage)
	FinalizeEstimateCost(usage llm.TokenUsage) float64
}

// FinalizeAfterTerminalTool handles the agent-loop part of terminal tool
// finalization: increments stats, calls the LLM for a prose summary, and
// updates convCtx. Returns the trimmed response text and true when
// finalization produced a non-empty reply.
//
// text_response is special: Execute() already returned the verbatim reply, so
// no LLM synthesis is needed and no LLM stats are incremented.
func FinalizeAfterTerminalTool(ctx context.Context, runner TerminalFinalizer, convCtx *conversation.Context, rawToolResult string, stats *TurnStats) (string, bool) {
	if stats.TerminalTool == "text_response" {
		response := strings.TrimSpace(rawToolResult)
		convCtx.AddAssistantMessage(response)
		return response, response != ""
	}
	stats.LLMCalls++
	stats.LoopSteps++
	finalized := FinalizeTerminalTool(ctx, TerminalFinalizationInput{
		Messages:      convCtx.Messages(),
		TerminalTool:  stats.TerminalTool,
		RawToolResult: rawToolResult,
		Model:         runner.FinalizeModel(),
		Send:          runner.FinalizeChat,
		RecordUsage:   runner.FinalizeRecordUsage,
		EstimateCost:  runner.FinalizeEstimateCost,
	})
	convCtx.TrackTokens(finalized.Usage)
	stats.TokensPrompt += finalized.TokensPrompt
	stats.TokensCompletion += finalized.TokensCompletion
	stats.TokensTotal += finalized.TokensTotal
	stats.CostUSD += finalized.CostUSD
	response := strings.TrimSpace(finalized.Text)
	convCtx.AddAssistantMessage(response)
	return response, finalized.Err == nil && !finalized.Fallback && response != ""
}
