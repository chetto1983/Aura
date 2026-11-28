package agui

import (
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// rawToolResults maps a tool call to the raw result its live card showed, read from the
// ledger. A stored untrusted turn holds what the model read, and the frame's escaping folded
// it under NFKC (a fullwidth letter, a ligature); the ledger's end row keeps the raw preview.
// The row counts only when it is the one end row of an id carried by one tool turn, in this
// conversation, from the called tool, and is the stored result before folding. A preview
// the ledger capped at 2 KiB or redacted is not, so that turn keeps its unframed, folded text.
func rawToolResults(hist []llm.Message, conversationID string, facts []toolinvocations.Event) map[string]string {
	called := make(map[string]string)
	results := make(map[string]int)
	for _, m := range hist {
		for _, call := range m.ToolCalls {
			called[call.ID] = call.Function.Name
		}
		if m.Role == llm.RoleTool {
			results[m.ToolCallID]++
		}
	}
	ends := make(map[string]int)
	previews := make(map[string]string)
	for _, fact := range facts {
		if fact.ConversationID != conversationID || fact.Event != toolinvocations.EventEnd || fact.ToolName != called[fact.ToolCallID] {
			continue
		}
		ends[fact.ToolCallID]++
		previews[fact.ToolCallID] = fact.ResultPreview
	}
	out := make(map[string]string)
	for _, m := range hist {
		preview, found := previews[m.ToolCallID]
		if m.Role != llm.RoleTool || m.ToolCallID == "" || !found || results[m.ToolCallID] != 1 || ends[m.ToolCallID] != 1 {
			continue
		}
		if agent.EscapePromptText(preview) == agent.EscapePromptText(agent.PlainToolOutput(m.Content)) {
			out[m.ToolCallID] = preview
		}
	}
	return out
}

// shownToolResult is the text a person reads for a tool turn: its raw result when the
// ledger holds it, else the turn's own text unframed.
func shownToolResult(m llm.Message, raw map[string]string) string {
	if preview, ok := raw[m.ToolCallID]; ok {
		return preview
	}
	return agent.PlainToolOutput(m.Content)
}
