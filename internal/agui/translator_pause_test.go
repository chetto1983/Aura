package agui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"

	"github.com/chetto1983/aura/internal/agent"
)

// A round raises its pauses back to back. Measured 2026-11-27: two parallel send_email calls
// raised two, the translator stopped at the first, and the runner never read the second. Its
// draft had no pause, so the operator never saw it, and the history the model resumed on held
// only the first call.
func TestTranslatorEndsARoundOnEveryPauseItRaised(t *testing.T) {
	stream := []*agent.Event{
		chunk("drafting"),
		pauseEvent("m1", "Review this message before sending", "approval"),
		pauseEvent("m2", "Review this message before sending", "approval"),
		chunk("never reached"),
	}
	pulled := 0
	seq := func(yield func(*agent.Event, error) bool) {
		for _, ev := range stream {
			pulled++
			if !yield(ev, nil) {
				return
			}
		}
	}
	var out []events.Event
	for ev, err := range Translate("thread-1", "run-1", &fixedIDGen{}, seq, false) {
		if err != nil {
			t.Fatalf("Translate: %v", err)
		}
		out = append(out, ev)
	}

	if pulled < 3 {
		t.Fatalf("read %d events, want both pauses read so the runner persists each", pulled)
	}
	if got := countType(out, "RUN_FINISHED"); got != 1 {
		t.Fatalf("RUN_FINISHED = %d, want 1", got)
	}
	raw, err := json.Marshal(out[len(out)-1])
	if err != nil {
		t.Fatalf("marshal RUN_FINISHED: %v", err)
	}
	var fin struct {
		Outcome struct {
			Type       string `json:"type"`
			Interrupts []struct {
				ToolCallID string `json:"toolCallId"`
			} `json:"interrupts"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(raw, &fin); err != nil {
		t.Fatalf("unmarshal RUN_FINISHED: %v", err)
	}
	if fin.Outcome.Type != "interrupt" || len(fin.Outcome.Interrupts) != 2 ||
		fin.Outcome.Interrupts[0].ToolCallID != "m1" || fin.Outcome.Interrupts[1].ToolCallID != "m2" {
		t.Fatalf("outcome = %+v, want an interrupt naming m1 then m2", fin.Outcome)
	}
	for _, ev := range out {
		if raw, _ := json.Marshal(ev); strings.Contains(string(raw), "never reached") {
			t.Fatalf("an event after the pauses reached the wire: %s", raw)
		}
	}
}
