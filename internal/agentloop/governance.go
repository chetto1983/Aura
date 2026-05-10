// governance.go contains the small set of message-list transforms applied
// to conversation history just before each LLM call. They prevent three
// concrete failure modes seen in conversation logs:
//
//   1. Orphan tool results — a tool message whose tool_call_id no longer
//      matches any assistant tool_calls. The OpenAI/compatible APIs reject
//      this with a 400. Happens when a turn is restored from checkpoint,
//      or when the assistant message is dropped (history sliding window).
//
//   2. Missing tool results — the inverse: an assistant message announces
//      N tool calls but only K results follow. The API also rejects this.
//      Backfill inserts a synthetic "[Tool result unavailable]" placeholder
//      so the conversation stays valid.
//
//   3. Context rot — once enough turns accumulate, old read_file / exec /
//      web_search / web_fetch / list_files results dominate the prompt and
//      drown the recent user intent. Microcompact replaces older copies
//      with a one-line "[<name> result omitted]" stub, keeping only the
//      most recent K instances of each tool. The user-visible conversation
//      is untouched; only the prompt sent to the model is reshaped.
//
// All four functions are pure: they read messages, return a new slice if
// they change anything, and never mutate the input.
package agentloop

import (
	"github.com/aura/aura/internal/llm"
)

const (
	// MicrocompactKeepRecent is the number of recent compactable tool
	// results kept verbatim. Older results of the same tool family get
	// replaced with a stub. Mirrors nanobot's default of 10.
	MicrocompactKeepRecent = 10

	// MicrocompactMinChars skips microcompacting any tool result smaller
	// than this — there is nothing to gain compressing a one-line OK.
	MicrocompactMinChars = 500

	// DefaultMaxToolResultChars caps each tool result at this many bytes
	// before going to the LLM. 8 KB is a balance between giving the model
	// enough to work with and avoiding the model thrashing on max_bytes
	// (the previous `read_file: above max_bytes 500` retry storm).
	DefaultMaxToolResultChars = 8000

	// truncationMarker is appended in place of trimmed bytes so the model
	// knows content was elided rather than absent.
	truncationMarker = "\n…[truncated by runtime]"
)

// compactableTools is the closed set of tool names whose results are
// considered "context-cheap": they can be summarized to a stub once newer
// equivalents exist. Other tool results (e.g. wiki writes, mcp_mail
// confirmations) carry decisions and stay verbatim.
var compactableTools = map[string]bool{
	"read_file":     true,
	"search_files":  true,
	"list_files":    true,
	"execute_code":  true,
	"execute_shell": true,
	"web_search":    true,
	"web_fetch":     true,
	"search_memory": true,
}

// dropOrphanToolResults removes any tool message whose tool_call_id was
// never announced by an earlier assistant message. The OpenAI-compatible
// chat API rejects such histories with a 400, so we filter them here
// rather than rely on the LLM to skip them.
func dropOrphanToolResults(messages []llm.Message) []llm.Message {
	declared := map[string]bool{}
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, tc := range msg.ToolCalls {
			if tc.ID != "" {
				declared[tc.ID] = true
			}
		}
	}
	var out []llm.Message
	clean := true
	for i, msg := range messages {
		if msg.Role == "tool" && msg.ToolCallID != "" && !declared[msg.ToolCallID] {
			if clean {
				out = append([]llm.Message(nil), messages[:i]...)
				clean = false
			}
			continue
		}
		if !clean {
			out = append(out, msg)
		}
	}
	if clean {
		return messages
	}
	return out
}

// backfillMissingToolResults inserts a synthetic tool message for every
// assistant tool_call that has no matching tool result. The placeholder
// content marks the call as interrupted, which lets the model recover
// instead of the API rejecting the history.
func backfillMissingToolResults(messages []llm.Message) []llm.Message {
	type pendingCall struct {
		assistantIdx int
		id           string
		name         string
	}
	var pending []pendingCall
	fulfilled := map[string]bool{}
	for i, msg := range messages {
		switch msg.Role {
		case "assistant":
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					pending = append(pending, pendingCall{assistantIdx: i, id: tc.ID, name: tc.Name})
				}
			}
		case "tool":
			if msg.ToolCallID != "" {
				fulfilled[msg.ToolCallID] = true
			}
		}
	}
	var missing []pendingCall
	for _, p := range pending {
		if !fulfilled[p.id] {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return messages
	}
	out := make([]llm.Message, 0, len(messages)+len(missing))
	out = append(out, messages...)
	offset := 0
	for _, p := range missing {
		insertAt := p.assistantIdx + 1 + offset
		for insertAt < len(out) && out[insertAt].Role == "tool" {
			insertAt++
		}
		stub := llm.Message{
			Role:       "tool",
			ToolCallID: p.id,
			Content:    "[Tool result unavailable — call was interrupted or lost]",
		}
		out = append(out[:insertAt], append([]llm.Message{stub}, out[insertAt:]...)...)
		offset++
	}
	return out
}

// microcompactToolResults replaces older compactable tool results with a
// short stub, keeping the most recent `keepRecent` ones verbatim. Tool
// messages outside the compactable set are never touched. Returns the
// input unchanged when nothing crosses the keepRecent threshold.
func microcompactToolResults(messages []llm.Message, keepRecent, minChars int) []llm.Message {
	if keepRecent <= 0 {
		keepRecent = MicrocompactKeepRecent
	}
	if minChars <= 0 {
		minChars = MicrocompactMinChars
	}
	var compactableIndices []int
	for i, msg := range messages {
		if msg.Role != "tool" {
			continue
		}
		if !compactableTools[toolNameForMessage(msg, messages, i)] {
			continue
		}
		compactableIndices = append(compactableIndices, i)
	}
	if len(compactableIndices) <= keepRecent {
		return messages
	}
	stale := compactableIndices[:len(compactableIndices)-keepRecent]
	var out []llm.Message
	clean := true
	for _, idx := range stale {
		msg := messages[idx]
		if len(msg.Content) < minChars {
			continue
		}
		if clean {
			out = append([]llm.Message(nil), messages...)
			clean = false
		}
		name := toolNameForMessage(msg, messages, idx)
		out[idx] = llm.Message{
			Role:       "tool",
			ToolCallID: msg.ToolCallID,
			Content:    "[" + name + " result omitted from context]",
		}
	}
	if clean {
		return messages
	}
	return out
}

// truncateOversizedToolResults caps each tool message at maxChars bytes.
// Anything beyond the cap is replaced with truncationMarker so the model
// knows content was elided. Non-tool messages are untouched.
func truncateOversizedToolResults(messages []llm.Message, maxChars int) []llm.Message {
	if maxChars <= 0 {
		maxChars = DefaultMaxToolResultChars
	}
	var out []llm.Message
	clean := true
	for i, msg := range messages {
		if msg.Role != "tool" || len(msg.Content) <= maxChars {
			continue
		}
		if clean {
			out = append([]llm.Message(nil), messages...)
			clean = false
		}
		cut := maxChars - len(truncationMarker)
		if cut < 0 {
			cut = 0
		}
		out[i] = llm.Message{
			Role:       "tool",
			ToolCallID: msg.ToolCallID,
			Content:    msg.Content[:cut] + truncationMarker,
		}
	}
	if clean {
		return messages
	}
	return out
}

// toolNameForMessage finds the tool name for a tool-role message by
// looking back at the most recent assistant tool_calls. llm.Message does
// not carry the tool name on tool-role messages (only the tool_call_id),
// so we resolve it from the assistant that emitted the call.
func toolNameForMessage(msg llm.Message, messages []llm.Message, idx int) string {
	if msg.ToolCallID == "" {
		return "tool"
	}
	for i := idx - 1; i >= 0; i-- {
		prev := messages[i]
		if prev.Role != "assistant" {
			continue
		}
		for _, tc := range prev.ToolCalls {
			if tc.ID == msg.ToolCallID {
				if tc.Name != "" {
					return tc.Name
				}
				return "tool"
			}
		}
	}
	return "tool"
}

// applyGovernance runs the full transform chain in the order required for
// correctness: drop orphans first (so backfill does not over-eagerly insert
// stubs for IDs we are about to remove), then backfill (so subsequent passes
// see a valid call/result alternation), then microcompact (which needs all
// tool messages present to count "recent" correctly), then truncate.
func applyGovernance(messages []llm.Message, maxToolResultChars int) []llm.Message {
	messages = dropOrphanToolResults(messages)
	messages = backfillMissingToolResults(messages)
	messages = microcompactToolResults(messages, MicrocompactKeepRecent, MicrocompactMinChars)
	messages = truncateOversizedToolResults(messages, maxToolResultChars)
	return messages
}
