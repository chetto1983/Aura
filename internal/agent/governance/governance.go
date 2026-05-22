// Package governance contains the message-list transforms applied to
// conversation history just before each LLM call in the agent loop.
// They prevent three concrete failure modes seen in conversation logs:
//
//  1. Orphan tool results — a tool message whose tool_call_id no longer
//     matches any assistant tool_calls. The OpenAI/compatible APIs reject
//     this with a 400.
//
//  2. Missing tool results — the inverse: an assistant message announces
//     N tool calls but only K results follow. Backfill inserts a synthetic
//     "[Tool result unavailable]" placeholder so the conversation stays valid.
//
//  3. Context rot — once enough turns accumulate, old read_file / exec /
//     web_search / web_fetch results dominate the prompt. Microcompact
//     replaces older copies with a one-line "[<name> result omitted]" stub.
//
// All functions are pure: they read messages, return a new slice if they
// change anything, and never mutate the input.
package governance

import (
	"slices"
	"unicode/utf8"

	"github.com/aura/aura/internal/llm"
)

const (
	// MicrocompactKeepRecent is the number of recent compactable tool
	// results kept verbatim. Older results of the same tool family get
	// replaced with a stub.
	MicrocompactKeepRecent = 10

	// MicrocompactMinChars skips microcompacting any tool result smaller
	// than this — there is nothing to gain compressing a one-line OK.
	// Raised from 500 in Phase-F: middling-size tool results stay verbatim;
	// only large outputs (>2 KB) compact.
	MicrocompactMinChars = 2000

	// DefaultMaxToolResultChars caps each tool result at this many bytes
	// before going to the LLM. Raised from 8000 in Phase-F: modern long-context
	// LLMs can handle larger observations, and clipping rich tool output is
	// the capability-throttle pattern documented in
	// docs/aura-main-loop-limits-audit.md §3.5.
	DefaultMaxToolResultChars = 24000

	// truncationMarker is appended in place of trimmed bytes so the model
	// knows content was elided rather than absent.
	truncationMarker = "\n…[truncated by runtime]"
)

// compactableTools is the closed set of tool names whose results are
// considered "context-cheap" and can be summarized to a stub once newer
// equivalents exist. Other tool results carry decisions and stay verbatim.
var compactableTools = map[string]bool{
	"file":         true,
	"execute_code": true,
	"execute_shell": true,
	"web":          true,
	"search":       true,
}

// Apply runs the full governance transform chain in the order required for
// correctness: drop orphans first, then backfill, then microcompact, then
// truncate. Zero values for microcompactKeepRecent / microcompactMinChars /
// maxToolResultChars fall back to the package defaults.
func Apply(messages []llm.Message, maxToolResultChars, microcompactKeepRecent, microcompactMinChars int) []llm.Message {
	if microcompactKeepRecent <= 0 {
		microcompactKeepRecent = MicrocompactKeepRecent
	}
	if microcompactMinChars <= 0 {
		microcompactMinChars = MicrocompactMinChars
	}
	messages = dropOrphanToolResults(messages)
	messages = backfillMissingToolResults(messages)
	messages = microcompactToolResults(messages, microcompactKeepRecent, microcompactMinChars)
	messages = truncateOversizedToolResults(messages, maxToolResultChars)
	return messages
}

// dropOrphanToolResults removes any tool message whose tool_call_id was
// never announced by an earlier assistant message.
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
// assistant tool_call that has no matching tool result.
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
		out = slices.Insert(out, insertAt, stub)
		offset++
	}
	return out
}

// microcompactToolResults replaces older compactable tool results with a
// short stub, keeping the most recent `keepRecent` ones verbatim.
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
		for cut > 0 && cut < len(msg.Content) && !utf8.RuneStart(msg.Content[cut]) {
			cut--
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

// toolNameForMessage finds the tool name for a tool-role message by looking
// back at the most recent assistant tool_calls.
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
