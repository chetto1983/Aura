package agent

import "github.com/aura/aura/internal/llm"

// PendingAskUserCall walks the conversation history backward and returns
// information about the most recent ask_user tool call that has no matching
// tool_result (i.e. the pause is still active). Mirrors nanobot's
// pending_ask_user_id pattern but operates on the LLM message slice that is
// always available to agent-loop callers.
//
// Returns ok=false when:
//   - there is no ask_user call in the history, or
//   - the most recent ask_user call already has a matching tool_result.
func PendingAskUserCall(messages []llm.Message) (toolCallID string, options []string, kind string, ok bool) {
	// Collect resolved tool_call IDs from tool-result messages.
	resolved := make(map[string]bool)
	for _, msg := range messages {
		if msg.Role == "tool" && msg.ToolCallID != "" {
			resolved[msg.ToolCallID] = true
		}
	}

	// Walk backward through assistant messages looking for an unresolved ask_user
	// or ask_user_clarification call.
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		for _, call := range msg.ToolCalls {
			if call.Name != "ask_user" && call.Name != "ask_user_clarification" {
				continue
			}
			if resolved[call.ID] {
				continue
			}
			// Found the pending ask_user call.
			var opts []string
			if raw, ok := call.Arguments["options"]; ok {
				switch v := raw.(type) {
				case []string:
					opts = append(opts, v...)
				case []any:
					for _, item := range v {
						if s, ok := item.(string); ok {
							opts = append(opts, s)
						}
					}
				}
			}
			k := "clarification"
			if kk, ok := call.Arguments["kind"].(string); ok && kk != "" {
				k = kk
			}
			return call.ID, opts, k, true
		}
	}
	return "", nil, "", false
}
