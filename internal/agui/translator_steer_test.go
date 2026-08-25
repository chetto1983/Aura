package agui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/google/uuid"
)

// translator_steer_test.go pins amendment #132/STEER-03's aura.steer echo —
// split out of translator_test.go (refactor-on-touch, CLAUDE.md 600-LOC cap)
// mirroring translator_artifact_test.go / translator_display_test.go's own
// per-event-type test file convention.

// steerEvent builds an *agent.Event carrying an Actions.SteerDelta payload —
// the shape drainSteer (internal/agent/llm_agent_steer.go) produces.
func steerEvent(delta map[string]any) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		Actions: agent.Actions{SteerDelta: delta}}
}

// TestSteerFrameIsCustomEvent pins amendment #132/STEER-03's echo: an Event
// carrying a non-nil SteerDelta emits exactly ONE CUSTOM event named
// SteerEventName ("aura.steer"), carrying the delta verbatim as its value —
// additive, so every prior emission path stays unchanged (a nil SteerDelta
// falls through to whatever branch would otherwise fire).
func TestSteerFrameIsCustomEvent(t *testing.T) {
	delta := map[string]any{
		"conversation_id": "conv-1",
		"round":           uint32(2),
		"steers": []map[string]any{
			{"id": "s1", "source": "cockpit", "text": "switch to Y", "delivery": "tool_result_append"},
		},
	}
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{steerEvent(delta)})
	if got := countType(evs, "CUSTOM"); got != 1 {
		t.Fatalf("CUSTOM = %d, want exactly 1 steer echo event (%v)", got, typesOf(evs))
	}
	for _, ty := range []string{"TEXT_MESSAGE_CONTENT", "TOOL_CALL_RESULT", "STATE_DELTA"} {
		if got := countType(evs, ty); got != 0 {
			t.Fatalf("%s = %d, want 0 — the steer echo is a dedicated CUSTOM event", ty, got)
		}
	}

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
		Name  string         `json:"name"`
		Value map[string]any `json:"value"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal CUSTOM: %v", err)
	}
	if got.Name != SteerEventName {
		t.Fatalf("CUSTOM name = %q, want %q", got.Name, SteerEventName)
	}
	if got.Value["conversation_id"] != "conv-1" {
		t.Fatalf("CUSTOM value missing conversation_id: %#v", got.Value)
	}
	if err := custom.Validate(); err != nil {
		t.Fatalf("emitted %s failed Validate: %v", custom.Type(), err)
	}
}

// TestSteerFrameNilDeltaIsIgnored: a nil SteerDelta emits no steer CUSTOM
// event — the branch keys on a non-nil delta, mirroring the ArtifactDelta/
// Display branches' own additive-nil-falls-through convention.
func TestSteerFrameNilDeltaIsIgnored(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		chunk("no steer here"),
	})
	if got := countType(evs, "CUSTOM"); got != 0 {
		t.Fatalf("CUSTOM = %d, want 0 for an Event with no SteerDelta (%v)", got, typesOf(evs))
	}
}
