package agentloop

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/llm"
)

// DedupeToolCalls keeps the first call for each tool name + canonical argument
// pair and returns later identical calls for recoverable skip results.
func DedupeToolCalls(calls []llm.ToolCall) (kept []llm.ToolCall, duplicates []llm.ToolCall) {
	kept = make([]llm.ToolCall, 0, len(calls))
	seen := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		key := duplicateToolCallKey(call)
		if _, ok := seen[key]; ok {
			duplicates = append(duplicates, call)
			continue
		}
		seen[key] = struct{}{}
		kept = append(kept, call)
	}
	return kept, duplicates
}

func duplicateToolCallKey(call llm.ToolCall) string {
	args, err := json.Marshal(call.Arguments)
	if err != nil {
		args = []byte(fmt.Sprint(call.Arguments))
	}
	return strings.TrimSpace(call.Name) + "\x00" + string(args)
}
