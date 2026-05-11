package agentruntime

import (
	"context"
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
	baseMessages := agentloop.ApplyGovernance(in.Messages, 0, 0, 0)

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
// strict=true tightens the language requirements on retry — "you JUST emitted
// invalid output, do not repeat it".
func terminalFinalizationPrompt(terminalTool string, strict bool) llm.Message {
	toolName := strings.TrimSpace(terminalTool)
	if toolName == "" {
		toolName = "the terminal tool"
	}
	if toolName == "search_memory" {
		content := "search_memory returned a recency-weighted hit list above. Do not call tools. Answer the user's original request in their language, using the hits as background. Cite [[slug]] for wiki pages and src_xxxx for sources when relevant. Be concise and conversational."
		if strict {
			content = "RETRY. " + content + " Your previous attempt emitted tool-call markup or empty text — do not do that. Plain prose only."
		}
		return llm.Message{Role: "user", Content: content}
	}
	content := fmt.Sprintf("The %q tool just finished. Do not call tools. Answer the user in their language, conversationally, using the tool results above. Describe what you did and what you found. No JSON, no tool-call markup, no internal markers like exit_code or source_id — just natural prose.", toolName)
	if strict {
		content = "RETRY. " + content + " Your previous attempt was empty or contained tool-call markup. Reply now in plain prose; the user is waiting."
	}
	return llm.Message{Role: "user", Content: content}
}

// TerminalToolFinalizationMessages is retained for callers that compose the
// finalize prompt themselves (tests, debug harnesses). FinalizeTerminalTool
// no longer uses this helper — it builds the message list inline so the
// retry path can swap prompts.
func TerminalToolFinalizationMessages(messages []llm.Message, terminalTool string) []llm.Message {
	out := agentloop.ApplyGovernance(messages, 0, 0, 0)
	return append(out, terminalFinalizationPrompt(terminalTool, false))
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

// IsFileGenerationTool reports whether a tool name produces a user-facing
// file artifact (xlsx/docx/pdf). Kept as a small routing helper; the
// per-tool canned response strings were removed in favor of LLM synthesis.
func IsFileGenerationTool(name string) bool {
	switch name {
	case "create_docx", "create_xlsx", "create_pdf":
		return true
	default:
		return false
	}
}
