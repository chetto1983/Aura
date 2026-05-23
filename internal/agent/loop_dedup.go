// loop_dedup.go holds budget-cap and dedup helper functions extracted from
// loop.go (US-CL01). Same package; all types (ToolCallState, BeforeToolCallback,
// etc.) remain in loop.go.
package agent

import (
	"fmt"
	"sort"
	"strings"
	"time"

	governance "github.com/aura/aura/internal/agent/governance"
	"github.com/aura/aura/internal/llm"
)

// IsRetryableToolResult reports whether a prior tool-result text leaves the
// loop willing to re-execute the same call.
func IsRetryableToolResult(prev string) bool {
	trimmed := strings.TrimSpace(prev)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "Error:")
}

func duplicateToolResult(call llm.ToolCall, opts Options) string {
	if opts.DuplicateToolResult != nil {
		return opts.DuplicateToolResult(call)
	}
	return fmt.Sprintf("You already called %s with these exact arguments earlier this turn. The result is above — read it instead of calling again. If you need different data, call with different arguments or pick another tool.", call.Name)
}

func maxCallsToolResult(call llm.ToolCall) string {
	return fmt.Sprintf("You have hit the per-turn call budget for %s. Move on with what you already have, or call a different tool.", call.Name)
}

func budgetCapToolResult(maxToolCalls int) string {
	return fmt.Sprintf("Tool call skipped: per-Run tool budget (%d) reached. Return a concise partial result from the evidence already gathered.", maxToolCalls)
}

func toolBudgetFinalInstruction(maxToolCalls int) string {
	return fmt.Sprintf("Tool budget reached (%d calls). Do not call tools again. Finish the assigned work now with a concise final report from the evidence above: answer, evidence/URLs when available, gaps, and next action.", maxToolCalls)
}

func interruptedAssistantContent(err error, lastToolResult string) string {
	content := fmt.Sprintf("Agent interrupted before a final answer: %v.", err)
	if trimmed := strings.TrimSpace(lastToolResult); trimmed != "" {
		content += "\n\nLast tool result:\n" + trimmed
	}
	return content
}

func DuplicateOrMaxCallsPolicy(maxCallsPerTool map[string]int, result func(llm.ToolCall, ToolCallState) string) BeforeToolCallback {
	return func(call llm.ToolCall, state ToolCallState) ToolCallDecision {
		limitReached := false
		if maxCalls := maxCallsPerTool[call.Name]; maxCalls > 0 && state.CallsForTool >= maxCalls {
			limitReached = true
		}
		if !state.PriorIdentical && !limitReached {
			return ToolCallDecision{}
		}
		out := ""
		if result != nil {
			out = result(call, state)
		}
		if out == "" {
			if limitReached {
				out = fmt.Sprintf("You have hit the per-turn call budget for %s. Move on with what you already have, or call a different tool.", call.Name)
			} else {
				out = fmt.Sprintf("You already called %s with these exact arguments earlier this turn. Re-read the previous result above. If you need different data, change the arguments or pick another tool.", call.Name)
			}
		}
		return ToolCallDecision{Skip: true, Result: out}
	}
}

// finalAnswerOnBudgetWithContext returns a contextual budget-exhaustion message
// that includes which cap fired, the last tool name, and a retry hint. It is
// used by gracefulFinalize when AllowNoToolFinalization is false or the
// finalization LLM round fails.
func finalAnswerOnBudgetWithContext(_, lastToolName, stopReason string, opts Options) string {
	if lastToolName == "" {
		return "Per-turn cap reached without invoking any tool. Try rephrasing -- Aura could not pick a tool for this request."
	}
	switch stopReason {
	case governance.StopReasonMaxIterations:
		return fmt.Sprintf("Per-turn step cap reached (%d iterations). Last tool: %s. Try a more specific request.", opts.MaxIterations, lastToolName)
	case governance.StopReasonWallClock:
		elapsed := opts.MaxElapsed.Round(time.Second)
		return fmt.Sprintf("Per-turn time cap reached (%s). Last tool: %s. Try a smaller scope or break it up.", elapsed, lastToolName)
	case governance.StopReasonTokenBudget:
		return fmt.Sprintf("Per-turn token cap reached (%d tokens). Last tool: %s. Try a more targeted request.", opts.MaxTokens, lastToolName)
	case governance.StopReasonCostBudget:
		return fmt.Sprintf("Per-turn cost cap reached ($%.2f). Last tool: %s. Try a more targeted request.", opts.MaxCostUSD, lastToolName)
	default:
		return fmt.Sprintf("Per-turn cap reached. Last tool: %s. Try rephrasing.", lastToolName)
	}
}

func argKeysFromCall(call llm.ToolCall) []string {
	keys := make([]string, 0, len(call.Arguments))
	for k := range call.Arguments {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func toolResultPreview(result string) string {
	if len(result) <= MaxToolResultPreviewChars {
		return result
	}
	runes := []rune(result)
	if len(runes) <= MaxToolResultPreviewChars {
		return result
	}
	return string(runes[:MaxToolResultPreviewChars])
}

// MaxToolResultPreviewChars caps the preview string emitted on OnToolEnd.
const MaxToolResultPreviewChars = 200

// findAskUserCall returns the index of the first ask_user or
// ask_user_clarification call in calls, or -1.
func findAskUserCall(calls []llm.ToolCall) int {
	for i, call := range calls {
		if call.Name == "ask_user" || call.Name == "ask_user_clarification" {
			return i
		}
	}
	return -1
}
