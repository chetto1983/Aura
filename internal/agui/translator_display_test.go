package agui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/display"
	"github.com/google/uuid"
)

// displayEvent builds an *agent.Event carrying an Actions.Display payload (the
// shape the display normalizer produces for a recognized tool result, Phase 26).
func displayEvent(p *display.Payload) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		Actions: agent.Actions{Display: p}}
}

func sampleDisplay() *display.Payload {
	return &display.Payload{
		Type: display.KindWebResult, ToolCallID: "call-1", Title: "meteo",
		WebResults: []display.WebItem{{Title: "Forecast", URL: "https://weather.example.com/today", Snippet: "sunny", Domain: "weather.example.com", RefID: "src-1"}},
		Sources:    []display.Source{{RefID: "src-1", Index: 1, Type: display.KindWebResult, Title: "Forecast", URL: "https://weather.example.com/today", Snippet: "sunny", Cited: false}},
	}
}

// TestTranslateDisplayEmitsCustomEvent (DISP-01): an Event with Actions.Display
// yields exactly ONE aura.display CUSTOM event and ZERO text/tool/state events —
// the display path is a dedicated CUSTOM event beside the artifact branch.
func TestTranslateDisplayEmitsCustomEvent(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{displayEvent(sampleDisplay())})
	if got := countType(evs, "CUSTOM"); got != 1 {
		t.Fatalf("CUSTOM = %d, want exactly 1 display event (%v)", got, typesOf(evs))
	}
	for _, ty := range []string{"TEXT_MESSAGE_CONTENT", "TOOL_CALL_RESULT", "STATE_DELTA"} {
		if got := countType(evs, ty); got != 0 {
			t.Fatalf("%s = %d, want 0 — the display path is a dedicated CUSTOM event", ty, got)
		}
	}
	for _, ev := range evs {
		if err := ev.Validate(); err != nil {
			t.Fatalf("emitted %s failed Validate: %v", ev.Type(), err)
		}
	}
}

// TestTranslateNilDisplayNoEvent: an Event with a nil Display emits no aura.display
// event — additive, no regression to the existing stream.
func TestTranslateNilDisplayNoEvent(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{displayEvent(nil)})
	if got := countType(evs, "CUSTOM"); got != 0 {
		t.Fatalf("CUSTOM = %d, want 0 for a nil Display (%v)", got, typesOf(evs))
	}
}

// TestTranslateDisplayClosesOpenRun: a display Event arriving mid-text closes the
// open text run (TEXT_MESSAGE_END) BEFORE the CUSTOM event, mirroring the artifact
// close-first discipline so no run is left dangling.
func TestTranslateDisplayClosesOpenRun(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		chunk("Here are the results"),
		displayEvent(sampleDisplay()),
	})
	order := typesOf(evs)
	textEnd, custom := -1, -1
	for i, ty := range order {
		if ty == "TEXT_MESSAGE_END" {
			textEnd = i
		}
		if ty == "CUSTOM" {
			custom = i
		}
	}
	if textEnd == -1 || custom == -1 {
		t.Fatalf("missing TEXT_MESSAGE_END or CUSTOM (%v)", order)
	}
	if textEnd >= custom {
		t.Fatalf("TEXT_MESSAGE_END (idx %d) must precede CUSTOM (idx %d): %v", textEnd, custom, order)
	}
}

// TestTranslateDisplayCustomEventName: the CUSTOM event carries the DisplayEventName
// and the Payload as its value, so the cockpit sseAdapter can attach by tool_call_id.
func TestTranslateDisplayCustomEventName(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{displayEvent(sampleDisplay())})
	var custom events.Event
	for _, ev := range evs {
		if string(ev.Type()) == "CUSTOM" {
			custom = ev
		}
	}
	if custom == nil {
		t.Fatalf("no CUSTOM emitted (%v)", typesOf(evs))
	}
	raw, err := json.Marshal(custom)
	if err != nil {
		t.Fatalf("marshal CUSTOM: %v", err)
	}
	var got struct {
		Name  string `json:"name"`
		Value struct {
			Type       string `json:"type"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"value"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal CUSTOM: %v", err)
	}
	if got.Name != DisplayEventName {
		t.Fatalf("CUSTOM name = %q, want %q", got.Name, DisplayEventName)
	}
	if got.Value.Type != "web_result" || got.Value.ToolCallID != "call-1" {
		t.Fatalf("CUSTOM value = %+v, want web_result/call-1 pass-through", got.Value)
	}
}

// TestTranslateDisplayGoldenShape pins the aura.display CUSTOM event against the
// golden fixture (additive — all prior fixtures stay byte-identical).
func TestTranslateDisplayGoldenShape(t *testing.T) {
	golden := loadGolden(t)
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{displayEvent(sampleDisplay())})
	var custom events.Event
	for _, ev := range evs {
		if string(ev.Type()) == "CUSTOM" {
			custom = ev
		}
	}
	if custom == nil {
		t.Fatalf("translator did not emit CUSTOM (got %v)", typesOf(evs))
	}
	assertGoldenShape(t, "CUSTOM_DISPLAY", golden["CUSTOM_DISPLAY"], custom)
}
