package agent

import (
	"maps"

	tools "github.com/aura/aura/internal/agent/tools/registry"
)

// ToolExecutionSummary holds the outcome of executing a batch of tool calls.
// Channel-neutral: the DiscoveredTools field lets callers expand the active
// tool set without knowing which channel produced the summary.
type ToolExecutionSummary struct {
	LastResult        string
	FatalResult       string
	ReadSkillNames    []string
	TerminalTool      string
	DiscoveredTools   []string
	Results           map[string]string
	AwaitingUserInput *tools.ErrAwaitingUserInput
}

// ToolArgumentsForTool injects channel-specific context into tool arguments.
// For search_memory it adds chat_id so the memory store can scope results
// to the current conversation. All other tools pass args through unchanged.
func ToolArgumentsForTool(name string, args map[string]any, chatID int64) map[string]any {
	if name != "search_memory" {
		return args
	}
	out := make(map[string]any, len(args)+1)
	maps.Copy(out, args)
	if chatID > 0 {
		out["chat_id"] = float64(chatID)
	}
	return out
}

// IsTerminalTool reports whether a tool name is treated as a "terminal" tool —
// one whose output is delivered directly to the user without a follow-up LLM round.
func IsTerminalTool(name string) bool {
	return name == "execute_code" || name == "execute_shell" || name == "text_response" || IsFileGenerationTool(name)
}
