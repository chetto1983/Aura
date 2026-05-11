package agentloop

import (
	"encoding/json"
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
	name := strings.TrimSpace(call.Name)
	args, err := json.Marshal(call.Arguments)
	if err != nil {
		// json.Marshal failed (channels, funcs, NaN, or a custom type with a
		// broken MarshalJSON). fmt.Sprint of a map is non-deterministic across
		// runs — that would silently disable dedupe on the affected key. Use
		// a stable sentinel that errs on the side of dedupe instead of letting
		// the same call run twice in one batch (F-007).
		return name + "\x00unmarshalable"
	}
	return name + "\x00" + string(args)
}
