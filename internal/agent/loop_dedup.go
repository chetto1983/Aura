// loop_dedup.go holds budget-cap and dedup helper functions extracted from
// loop.go (US-CL01). Same package; all types (ToolCallState, BeforeToolCallback,
// etc.) remain in loop.go.
package agent

import (
	"fmt"
	"sort"
	"strings"

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

func finalAnswerOnBudget(lastToolResult string) string {
	if result := strings.TrimSpace(lastToolResult); result != "" {
		return result
	}
	return "I reached the per-turn budget without a usable result."
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

// findAskUserCall returns the index of the first ask_user call in calls, or -1.
func findAskUserCall(calls []llm.ToolCall) int {
	for i, call := range calls {
		if call.Name == "ask_user" {
			return i
		}
	}
	return -1
}
