package agui

import (
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/chetto1983/aura/internal/agent/display"
	"github.com/chetto1983/aura/internal/llm"
)

// displaySnapshotEvent is a display-aware MESSAGES_SNAPSHOT (D-06). It marshals BYTE-
// COMPATIBLY with events.MessagesSnapshotEvent ({"type":"MESSAGES_SNAPSHOT","messages":
// [...]}) but each tool-call entry additionally carries the re-derived `display`
// payload the cockpit replay reads (sseAdapter_snapshot.ts toolCallsFromSnapshot reads
// `call.display`). The SDK types.ToolCall has no display field, so the projection uses
// these local mirror structs rather than forking the SDK type.
type displaySnapshotEvent struct {
	Type     events.EventType         `json:"type"`
	Messages []displaySnapshotMessage `json:"messages"`
}

// displaySnapshotMessage mirrors events.Message's wire shape (id/role/content/
// toolCallId) with display-aware tool calls.
type displaySnapshotMessage struct {
	ID         string                    `json:"id"`
	Role       types.Role                `json:"role"`
	Content    string                    `json:"content,omitempty"`
	ToolCallID string                    `json:"toolCallId,omitempty"`
	ToolCalls  []displaySnapshotToolCall `json:"toolCalls,omitempty"`
}

// displaySnapshotToolCall mirrors types.ToolCall (id/type/function) plus the additive
// re-derived Display (D-06). A nil Display omits the key, so a tool the normalizer did
// not recognize replays its raw card identically to live (D-FALLBACK).
type displaySnapshotToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function types.FunctionCall `json:"function"`
	Display  *display.Payload   `json:"display,omitempty"`
}

// projectDisplaySnapshot builds the display-aware MESSAGES_SNAPSHOT (D-06): it re-runs
// the SAME display normalizer over each persisted tool-result turn and attaches the
// re-derived payload to the matching assistant tool-call entry by tool_call_id, so a
// reopened thread renders typed displays identically to live ("one normalizer for live
// + replay"). It builds ONE registry for the whole thread and feeds it in turn order,
// mirroring the live per-run registry, so source RefIDs/Index match the live run.
func projectDisplaySnapshot(hist []llm.Message) displaySnapshotEvent {
	displays := rederiveDisplays(hist)
	msgs := make([]displaySnapshotMessage, 0, len(hist))
	for i, m := range hist {
		msgs = append(msgs, displaySnapshotMessage{
			ID:         msgID(i),
			Role:       types.Role(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			ToolCalls:  projectDisplayToolCalls(m.ToolCalls, displays),
		})
	}
	return displaySnapshotEvent{Type: events.EventTypeMessagesSnapshot, Messages: msgs}
}

// rederiveDisplays walks the persisted history in order and re-derives a display.Payload
// per recognized tool-result turn (a RoleTool turn carries ToolCallID + the persisted
// ResultPreview as Content). It returns the map keyed by tool_call_id. A single shared
// registry accumulates across all web turns so cross-turn source RefIDs are stable —
// identical to the live per-run registry (Pitfall 4 parity).
func rederiveDisplays(hist []llm.Message) map[string]*display.Payload {
	reg := display.NewRegistry()
	toolNames := toolNamesByCallID(hist)
	out := make(map[string]*display.Payload)
	for i := range hist {
		m := &hist[i]
		if m.Role != llm.RoleTool || m.ToolCallID == "" {
			continue
		}
		name := toolNames[m.ToolCallID]
		if name == "" {
			continue
		}
		if p, ok := display.NormalizeToolPreview(m.ToolCallID, name, m.Content, reg); ok {
			payload := p
			out[m.ToolCallID] = &payload
		}
	}
	return out
}

// toolNamesByCallID maps each tool_call_id to its tool name by scanning the assistant
// turns' ToolCalls — the RoleTool result turn carries only the id + preview, not the
// name, so the name is recovered from the assistant call that produced it.
func toolNamesByCallID(hist []llm.Message) map[string]string {
	names := make(map[string]string)
	for i := range hist {
		for _, c := range hist[i].ToolCalls {
			if c.ID != "" {
				names[c.ID] = c.Function.Name
			}
		}
	}
	return names
}

// projectDisplayToolCalls maps the persisted tool calls onto the display-aware shape,
// attaching the re-derived display (when the matching result turn was recognized).
// Returns nil for an empty input so the omitempty toolCalls key is absent on non-tool
// turns.
func projectDisplayToolCalls(calls []llm.ToolCall, displays map[string]*display.Payload) []displaySnapshotToolCall {
	if len(calls) == 0 {
		return nil
	}
	out := make([]displaySnapshotToolCall, 0, len(calls))
	for _, c := range calls {
		out = append(out, displaySnapshotToolCall{
			ID:   c.ID,
			Type: c.Type,
			Function: types.FunctionCall{
				Name:      c.Function.Name,
				Arguments: c.Function.Arguments,
			},
			Display: displays[c.ID],
		})
	}
	return out
}
