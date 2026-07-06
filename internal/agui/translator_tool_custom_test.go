package agui

import (
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/display"
	"github.com/google/uuid"
)

// translator_tool_custom_test.go covers emitToolResultCustom (the additive artifact/display
// CUSTOM events that ride a tool-RESULT event inline) and the emitToolInvocation start/end
// lifecycle. The standalone artifact/display branches are unreachable for a tool result
// (the tool-lifecycle branch `continue`s), so a payload on a tool-END event MUST be emitted
// by emitToolResultCustom right after TOOL_CALL_RESULT — proven here.

// toolEndWithArtifact builds a tool-RESULT event that ALSO carries an ArtifactDelta (the
// shape send_file's Meta lift stamps onto the SAME tool-end event).
func toolEndWithArtifact(callID, name, preview string, artifact map[string]any) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		Actions: agent.Actions{
			ToolInvocation: &agent.ToolInvocation{
				Event: agent.ToolInvocationEnd, ToolCallID: callID, ToolName: name, ResultPreview: preview,
			},
			ArtifactDelta: artifact,
		}}
}

// toolEndWithDisplay builds a tool-RESULT event that ALSO carries a typed Display payload
// (the shape the Phase-26 display normalizer attaches to a recognized tool result).
func toolEndWithDisplay(callID, name, preview string, p *display.Payload) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		Actions: agent.Actions{
			ToolInvocation: &agent.ToolInvocation{
				Event: agent.ToolInvocationEnd, ToolCallID: callID, ToolName: name, ResultPreview: preview,
			},
			Display: p,
		}}
}

// TestTranslatorToolResultEmitsInlineArtifact: a tool-END event carrying an ArtifactDelta
// yields the TOOL_CALL_END + TOOL_CALL_RESULT AND the additive aura.artifact CUSTOM event
// inline (emitToolResultCustom), not dropped.
func TestTranslatorToolResultEmitsInlineArtifact(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		toolStart("call-1", "send_file", `{"path":"/abs/a.pdf"}`),
		toolEndWithArtifact("call-1", "send_file", "delivered", map[string]any{"path": "/abs/a.pdf", "filename": "a.pdf"}),
	})
	if got := countType(evs, "TOOL_CALL_RESULT"); got != 1 {
		t.Fatalf("TOOL_CALL_RESULT = %d, want 1 (%v)", got, typesOf(evs))
	}
	if got := countType(evs, "CUSTOM"); got != 1 {
		t.Fatalf("CUSTOM = %d, want exactly 1 inline artifact event (%v)", got, typesOf(evs))
	}
	// The CUSTOM must follow the TOOL_CALL_RESULT (correlated by tool_call_id), never precede it.
	order := typesOf(evs)
	result, custom := -1, -1
	for i, ty := range order {
		if ty == "TOOL_CALL_RESULT" {
			result = i
		}
		if ty == "CUSTOM" {
			custom = i
		}
	}
	if result == -1 || custom == -1 || custom < result {
		t.Fatalf("CUSTOM (idx %d) must follow TOOL_CALL_RESULT (idx %d): %v", custom, result, order)
	}
	for _, ev := range evs {
		if err := ev.Validate(); err != nil {
			t.Fatalf("emitted %s failed Validate: %v", ev.Type(), err)
		}
	}
}

// TestTranslatorToolResultEmitsInlineDisplay: a tool-END event carrying a Display payload
// yields the aura.display CUSTOM event inline alongside the TOOL_CALL_RESULT.
func TestTranslatorToolResultEmitsInlineDisplay(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		toolStart("call-2", "web_search", ""),
		toolEndWithDisplay("call-2", "web_search", "3 results", sampleDisplay()),
	})
	if got := countType(evs, "TOOL_CALL_RESULT"); got != 1 {
		t.Fatalf("TOOL_CALL_RESULT = %d, want 1 (%v)", got, typesOf(evs))
	}
	if got := countType(evs, "CUSTOM"); got != 1 {
		t.Fatalf("CUSTOM = %d, want exactly 1 inline display event (%v)", got, typesOf(evs))
	}
	for _, ev := range evs {
		if err := ev.Validate(); err != nil {
			t.Fatalf("emitted %s failed Validate: %v", ev.Type(), err)
		}
	}
}

// TestTranslatorToolResultBothArtifactAndDisplay: a tool-END event carrying BOTH an
// ArtifactDelta and a Display yields TWO inline CUSTOM events (both branches of
// emitToolResultCustom fire).
func TestTranslatorToolResultBothArtifactAndDisplay(t *testing.T) {
	ev := toolEndWithArtifact("call-3", "send_file", "delivered", map[string]any{"path": "/abs/a.pdf"})
	ev.Actions.Display = sampleDisplay()
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		toolStart("call-3", "send_file", ""),
		ev,
	})
	if got := countType(evs, "CUSTOM"); got != 2 {
		t.Fatalf("CUSTOM = %d, want 2 (artifact + display inline) (%v)", got, typesOf(evs))
	}
}

// TestTranslatorToolStartNoArgs: a tool-START with EMPTY arguments emits TOOL_CALL_START
// but NO TOOL_CALL_ARGS (the args event is conditional on Arguments != "").
func TestTranslatorToolStartNoArgs(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		toolStart("call-4", "noop_tool", ""),
		toolEnd("call-4", "noop_tool", "ok"),
	})
	if got := countType(evs, "TOOL_CALL_START"); got != 1 {
		t.Fatalf("TOOL_CALL_START = %d, want 1 (%v)", got, typesOf(evs))
	}
	if got := countType(evs, "TOOL_CALL_ARGS"); got != 0 {
		t.Fatalf("TOOL_CALL_ARGS = %d, want 0 for empty arguments (%v)", got, typesOf(evs))
	}
	if got := countType(evs, "TOOL_CALL_END"); got != 1 {
		t.Fatalf("TOOL_CALL_END = %d, want 1 (%v)", got, typesOf(evs))
	}
}
