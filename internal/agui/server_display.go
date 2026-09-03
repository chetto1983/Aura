package agui

import (
	"strings"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/chetto1983/aura/internal/agent/display"
	"github.com/chetto1983/aura/internal/conversations"
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
// toolCallId) with display-aware tool calls. Reasoning + ReasoningDurationMs are
// the amendment #91 (fix-plan 1.12) display-rehydration fields, set only on
// assistant answer messages whose persisted turn carries CoT (omitted otherwise —
// the wire is byte-unchanged for reasoning-less threads). Camel casing follows the
// SDK convention already used by toolCallId/toolCalls; the cockpit
// snapshotToThreadMessages maps them onto the {type:'reasoning'} part (RS-07).
type displaySnapshotMessage struct {
	ID                  string                    `json:"id"`
	Role                types.Role                `json:"role"`
	Content             string                    `json:"content,omitempty"`
	ToolCallID          string                    `json:"toolCallId,omitempty"`
	ToolCalls           []displaySnapshotToolCall `json:"toolCalls,omitempty"`
	Reasoning           string                    `json:"reasoning,omitempty"`
	ReasoningDurationMs int64                     `json:"reasoningDurationMs,omitempty"`
	// AttachmentIDs are the assets this user turn was sent with (migration 0116). Absent
	// on every other role, and on user turns that predate the column — which is what
	// lets the cockpit keep its old positional fold as the fallback for those alone.
	AttachmentIDs []string `json:"attachmentIds,omitempty"`
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
			Content:    snapshotContent(m),
			ToolCallID: m.ToolCallID,
			ToolCalls:  projectDisplayToolCalls(m.ToolCalls, displays),
		})
	}
	return displaySnapshotEvent{Type: events.EventTypeMessagesSnapshot, Messages: msgs}
}

func snapshotContent(m llm.Message) string {
	if m.Role != llm.RoleUser {
		return m.Content
	}
	return stripAuraContextEnvelope(m.Content)
}

func stripAuraContextEnvelope(content string) string {
	marker, markerLen := userMessageMarker(content)
	if marker < 0 {
		return content
	}
	if !isAuraContextEnvelope(content[:marker]) {
		return content
	}
	return content[marker+markerLen:]
}

func userMessageMarker(content string) (int, int) {
	for _, marker := range []string{"User message:\n", "User message:\r\n"} {
		if idx := strings.Index(content, marker); idx >= 0 {
			return idx, len(marker)
		}
	}
	return -1, 0
}

func isAuraContextEnvelope(prefix string) bool {
	rest := prefix
	seen := false
	for {
		rest = strings.TrimLeft(rest, " \t\r\n")
		switch {
		case strings.HasPrefix(rest, `<knowledge_base trust="operator_pinned_context">`):
			next, ok := consumeAuraContextBlock(rest, "</knowledge_base>")
			if !ok {
				return false
			}
			rest = next
			seen = true
		case strings.HasPrefix(rest, `<attachments trust="untrusted_user_uploads">`):
			next, ok := consumeAuraContextBlock(rest, "</attachments>")
			if !ok {
				return false
			}
			rest = next
			seen = true
		default:
			return seen && strings.TrimSpace(rest) == ""
		}
	}
}

func consumeAuraContextBlock(value, closeTag string) (string, bool) {
	_, after, ok := strings.Cut(value, closeTag)
	if !ok {
		return "", false
	}
	return after, true
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

// attachTurnReasoning merges the persisted display-only reasoning rows (amendment
// #91 / fix-plan 1.12) onto the snapshot's assistant answer messages. Pairing is
// positional: rows carries ONE entry per answer-shaped assistant turn (role
// assistant, no tool_calls — conversations.ListTurnReasoning) in seq order, and the
// repaired LoadHistory projection preserves exactly those messages in the same
// order (repairToolMessagePairs never drops, synthesizes, or reorders an assistant
// message without tool calls), so the k-th row belongs to the k-th assistant
// no-tool-call message. Rows with empty Reasoning (NULL column: pre-migration,
// redacted, disabled) attach nothing — the drawer stays absent, the correct
// degrade. Fail-soft on any count mismatch: surplus rows are ignored, surplus
// messages stay bare.
func attachTurnReasoning(snap *displaySnapshotEvent, rows []conversations.TurnReasoning) {
	if len(rows) == 0 {
		return
	}
	next := 0
	for i := range snap.Messages {
		m := &snap.Messages[i]
		if m.Role != types.Role(llm.RoleAssistant) || len(m.ToolCalls) > 0 {
			continue
		}
		if next >= len(rows) {
			return
		}
		row := rows[next]
		next++
		if row.Reasoning == "" {
			continue
		}
		m.Reasoning = row.Reasoning
		m.ReasoningDurationMs = row.DurationMS
	}
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

// attachTurnAttachments merges each user turn's attachment ids onto the user messages of
// the snapshot.
//
// Rows are sparse — only turns that carry attachments — so they are addressed by
// UserOrdinal rather than by their position in the slice. That ordinal is an index among
// USER turns, and user turns rebuild one-for-one and in order (repairToolMessagePairs
// touches only tool/assistant pairing), so the k-th user message is the k-th user turn.
//
// This is the whole point of the column: before it, the cockpit could only zip a thread's
// assets onto user turns by position, putting an image sent with the third message against
// the first. Fail-soft in both directions — an ordinal past the end of the messages is
// ignored, a message no row names keeps nothing.
func attachTurnAttachments(snap *displaySnapshotEvent, rows []conversations.TurnAttachments) {
	if len(rows) == 0 {
		return
	}
	userMessages := make([]*displaySnapshotMessage, 0, len(snap.Messages))
	for i := range snap.Messages {
		if snap.Messages[i].Role == types.Role(llm.RoleUser) {
			userMessages = append(userMessages, &snap.Messages[i])
		}
	}
	for _, row := range rows {
		if row.UserOrdinal < 0 || row.UserOrdinal >= len(userMessages) {
			continue
		}
		userMessages[row.UserOrdinal].AttachmentIDs = row.IDs
	}
}
