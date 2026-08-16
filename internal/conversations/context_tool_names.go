package conversations

import (
	"encoding/json"
	"fmt"
	"strings"
)

// The eviction pointer's wording, and the lookup that makes it possible.
//
// hermes-agent's compressor replaces a pruned tool result with a line like
// "[terminal] ran `npm test` -> exit 0, 47 lines output" and its comment states the reason
// plainly: a generic placeholder "carries zero information". Aura's pointer said only that
// SOMETHING had been evicted, so a model looking back at a long session saw a column of
// identical markers and had to page one back to learn which was which -- a tool call to
// discover whether a tool call was worth reading.

// evictedResultMarker is the phrase every eviction pointer contains, and the ONE thing
// downstream code may match on. It exists because the wording and the predicate that
// recognises it lived apart: renaming the pointer silently stopped
// repairManagedToolMessagePairs from recognising an orphan, and the pointer vanished from
// the history instead of becoming an assistant note.
const evictedResultMarker = "result evicted"

// legacyEvictedPointerPrefix is the wording used until 2026-08-16. Conversation turns are
// durable, so rows carrying it are still loaded every day and must keep being recognised.
const legacyEvictedPointerPrefix = "[tool output evicted"

// toolNamesByCallID maps each tool_call_id to the tool that produced it, read from the
// assistant turns that requested them. The name lives only there: a role=tool turn carries
// the id and the output, never the name.
func toolNamesByCallID(turns []Turn) map[string]string {
	names := make(map[string]string)
	for _, turn := range turns {
		if len(turn.ToolCalls) == 0 {
			continue
		}
		var calls []struct {
			ID       string `json:"id"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		// A history row that will not parse is not a reason to fail the turn: the pointer
		// simply loses its name, which is what it had before this existed.
		if err := json.Unmarshal(turn.ToolCalls, &calls); err != nil {
			continue
		}
		for _, call := range calls {
			if call.ID != "" && call.Function.Name != "" {
				names[call.ID] = call.Function.Name
			}
		}
	}
	return names
}

// describeEvictedResult is the one-line replacement for an evicted tool result: which tool
// ran, how big its output was, and how to read it back.
//
// Size is in the line because it is the cheapest signal of whether paging is worth a round
// trip -- 300 bytes back is rarely worth a call, 27 KB usually is -- and because it is
// exactly what the model can no longer see for itself.
func describeEvictedResult(toolName string, originalBytes int, spillID string) string {
	if toolName == "" {
		toolName = "tool"
	}
	if spillID == "" {
		return fmt.Sprintf("[%s %s from context (%s); not retrievable]",
			toolName, evictedResultMarker, humanBytes(originalBytes))
	}
	return fmt.Sprintf(
		"[%s %s to save context (%s); page it back via read_tool_output(tool_call_id=%q)]",
		toolName, evictedResultMarker, humanBytes(originalBytes), spillID)
}

// humanBytes renders a size the way the operator reads it in a log, not with a fractional
// kilobyte nobody needs.
func humanBytes(n int) string {
	switch {
	case n <= 0:
		return "empty"
	case n < 1024:
		return fmt.Sprintf("%d bytes", n)
	default:
		return fmt.Sprintf("%d KB", (n+512)/1024)
	}
}

// evictedResultToolName resolves the tool for a turn, preferring the map built from the
// assistant turns and falling back to a name embedded in the content by a tool that
// labelled its own output.
func evictedResultToolName(t Turn, names map[string]string) string {
	if name := names[t.ToolCallID]; name != "" {
		return name
	}
	if idx := strings.Index(t.Content, "]"); idx > 1 && strings.HasPrefix(t.Content, "[") {
		if candidate := t.Content[1:idx]; !strings.ContainsAny(candidate, " \t\n") {
			return candidate
		}
	}
	return ""
}
