package agent

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/display"
)

// TestActionsDisplayRoundTrip pins the additive Actions.Display slot (DISP-01):
// a set Display appears on the wire, survives decode, and re-marshals byte-
// identical (decode(encode)==identity), mirroring the AwaitingInput contract.
func TestActionsDisplayRoundTrip(t *testing.T) {
	ev := Event{
		RequestID: mustV7(t),
		Author:    "aura",
		Actions: Actions{
			Display: &display.Payload{
				Type:       display.KindWebResult,
				ToolCallID: "call_disp_1",
				Title:      "meteo",
				WebResults: []display.WebItem{{
					Title: "Forecast", URL: "https://example.com/a", Snippet: "sunny",
					Domain: "example.com", RefID: "src-1",
				}},
				Sources: []display.Source{{RefID: "src-1", Index: 1, Type: display.KindWebResult, Cited: true}},
			},
		},
		Timestamp: time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC),
	}

	first, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(first, []byte("\"display\"")) {
		t.Fatalf("set Display must appear on the wire, got: %s", first)
	}

	var decoded Event
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Actions.Display == nil {
		t.Fatalf("Display lost in round-trip")
	}
	if decoded.Actions.Display.Type != display.KindWebResult ||
		decoded.Actions.Display.ToolCallID != "call_disp_1" ||
		len(decoded.Actions.Display.WebResults) != 1 ||
		len(decoded.Actions.Display.Sources) != 1 {
		t.Fatalf("Display payload mangled: %+v", decoded.Actions.Display)
	}

	second, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("round-trip not byte-identical:\n first=%s\nsecond=%s", first, second)
	}
}

// TestActionsNilDisplayOmitsKey: a nil Display byte-omits the `display` key so the
// existing event wire is unchanged (additive, no regression).
func TestActionsNilDisplayOmitsKey(t *testing.T) {
	ev := Event{RequestID: mustV7(t), Author: "aura", Timestamp: time.Now().UTC()}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(b, []byte("\"display\"")) {
		t.Fatalf("nil Display must be omitted from the wire, got: %s", b)
	}
}
