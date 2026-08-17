package agui

import (
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/chetto1983/aura/internal/agent"
)

// A view descriptor rides the SAME event as the tool result, so it must be
// emitted inline with the tool lifecycle. The standalone artifact/display
// branches are unreachable for a tool result — this is the regression that
// dropped the artifact descriptor on the live SSE once already.
func TestTranslate_ViewDeltaRidesTheToolResult(t *testing.T) {
	ev := &agent.Event{}
	ev.Actions.ToolInvocation = &agent.ToolInvocation{
		Event: agent.ToolInvocationEnd, ToolCallID: "tc1", ToolName: "whatsapp__list_messages",
		ResultPreview: "2 messages",
	}
	ev.Actions.ViewDelta = map[string]any{
		"server": "whatsapp", "resource_uri": "ui://whatsapp/thread.html", "tool_call_id": "tc1",
	}

	var names []string
	var view events.Event
	for e, err := range Translate("thr", "run", NewIDGenerator(), agentSeq([]*agent.Event{ev}), false) {
		if err != nil {
			t.Fatalf("translate: %v", err)
		}
		names = append(names, string(e.Type()))
		if e.Type() == events.EventTypeCustom {
			view = e
		}
	}
	if view == nil {
		t.Fatalf("no CUSTOM event emitted; got %v", names)
	}
	custom, ok := view.(*events.CustomEvent)
	if !ok {
		t.Fatalf("event = %T", view)
	}
	if custom.Name != ViewEventName {
		t.Fatalf("name = %q, want %q", custom.Name, ViewEventName)
	}
	value, ok := custom.Value.(map[string]any)
	if !ok || value["tool_call_id"] != "tc1" {
		t.Fatalf("value = %#v", custom.Value)
	}
	// The document itself is never on the stream: it is static per server and is
	// fetched once from GET /api/mcp/view.
	if _, carried := value["html"]; carried {
		t.Fatal("the stream must carry the descriptor, never the document")
	}
}

// An event with no view delta is byte-identical to what it was before: the whole
// feature is additive on the wire.
func TestTranslate_NoViewDeltaEmitsNoCustomEvent(t *testing.T) {
	ev := &agent.Event{}
	ev.Actions.ToolInvocation = &agent.ToolInvocation{
		Event: agent.ToolInvocationEnd, ToolCallID: "tc1", ToolName: "shell_exec",
	}
	for e, err := range Translate("thr", "run", NewIDGenerator(), agentSeq([]*agent.Event{ev}), false) {
		if err != nil {
			t.Fatalf("translate: %v", err)
		}
		if e.Type() == events.EventTypeCustom {
			t.Fatalf("unexpected CUSTOM event: %#v", e)
		}
	}
}
