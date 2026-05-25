// Package governance contains the message-list transforms applied to
// conversation history just before each LLM call in the agent loop.
// They repair malformed tool history and clear old tool-result bodies before
// context rot dominates the prompt.
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
//     replaces older copies with ClearedPlaceholder.
//
// The transforms never mutate their input slices. Apply also records aggregate
// counters when a transform clears content.
package governance

import (
	"slices"
	"unicode/utf8"

	"github.com/aura/aura/internal/ctxmetrics"
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

	// ClearedPlaceholder replaces old tool result content while preserving the
	// role/tool_call_id envelope required by provider APIs.
	ClearedPlaceholder = "[Old tool result content cleared]"

	// truncationMarker is appended in place of trimmed bytes so the model
	// knows content was elided rather than absent.
	truncationMarker = "\n…[truncated by runtime]"
)

// MicrocompactStats describes one old-tool-result compaction pass.
type MicrocompactStats struct {
	Candidates   int
	KeptRecent   int
	Cleared      int
	BytesCleared int
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
	var microstats MicrocompactStats
	messages, microstats = Microcompact(messages, microcompactKeepRecent, microcompactMinChars)
	if microstats.Cleared > 0 {
		ctxmetrics.Global.MicrocompactRunsTotal.Add(1)
	}
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

// Microcompact replaces old tool results with ClearedPlaceholder while keeping
// the most recent keepRecent historical tool results and the current fresh
// tool-result batch verbatim.
func Microcompact(messages []llm.Message, keepRecent, minChars int) ([]llm.Message, MicrocompactStats) {
	if keepRecent <= 0 {
		keepRecent = MicrocompactKeepRecent
	}
	if minChars <= 0 {
		minChars = MicrocompactMinChars
	}
	freshIDs := freshTailToolResultIDs(messages)
	var candidates []int
	for i, msg := range messages {
		if msg.Role != "tool" {
			continue
		}
		if _, fresh := freshIDs[msg.ToolCallID]; fresh {
			continue
		}
		candidates = append(candidates, i)
	}
	stats := MicrocompactStats{
		Candidates: len(candidates),
		KeptRecent: minInt(keepRecent, len(candidates)),
	}
	if len(candidates) <= keepRecent {
		return messages, stats
	}
	stale := candidates[:len(candidates)-keepRecent]
	var out []llm.Message
	clean := true
	for _, idx := range stale {
		msg := messages[idx]
		if len(msg.Content) < minChars || msg.Content == ClearedPlaceholder {
			continue
		}
		if clean {
			out = append([]llm.Message(nil), messages...)
			clean = false
		}
		out[idx] = llm.Message{
			Role:       "tool",
			ToolCallID: msg.ToolCallID,
			Content:    ClearedPlaceholder,
		}
		stats.Cleared++
		stats.BytesCleared += len(msg.Content) - len(ClearedPlaceholder)
	}
	if clean {
		return messages, stats
	}
	return out, stats
}

func microcompactToolResults(messages []llm.Message, keepRecent, minChars int) []llm.Message {
	out, _ := Microcompact(messages, keepRecent, minChars)
	return out
}

func freshTailToolResultIDs(messages []llm.Message) map[string]struct{} {
	lastAssistant := -1
	for i, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			lastAssistant = i
		}
	}
	if lastAssistant < 0 || lastAssistant == len(messages)-1 {
		return nil
	}
	for i := lastAssistant + 1; i < len(messages); i++ {
		if messages[i].Role != "tool" {
			return nil
		}
	}
	ids := make(map[string]struct{}, len(messages[lastAssistant].ToolCalls))
	for _, call := range messages[lastAssistant].ToolCalls {
		if call.ID != "" {
			ids[call.ID] = struct{}{}
		}
	}
	return ids
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

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
