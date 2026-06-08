package agui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/google/uuid"
)

// artifactEvent builds an *agent.Event carrying an Actions.ArtifactDelta
// descriptor (the shape send_file's Meta lift produces in Plan 13-02 Task 1).
func artifactEvent(descriptor map[string]any) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		Actions: agent.Actions{ArtifactDelta: descriptor}}
}

// TestTranslatorArtifactEmitsCustomEvent: an Event with a non-empty ArtifactDelta
// yields exactly ONE generic CUSTOM AG-UI event (channel-agnostic, D-06) and ZERO
// TEXT_MESSAGE_CONTENT / TOOL_CALL_RESULT / STATE_DELTA — the artifact path is a
// dedicated custom event, NOT an overloaded tool result or prose.
func TestTranslatorArtifactEmitsCustomEvent(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		artifactEvent(map[string]any{"path": "/abs/results.xlsx", "filename": "results.xlsx", "caption": "results"}),
	})
	if got := countType(evs, "CUSTOM"); got != 1 {
		t.Fatalf("CUSTOM = %d, want exactly 1 artifact event (%v)", got, typesOf(evs))
	}
	for _, ty := range []string{"TEXT_MESSAGE_CONTENT", "TOOL_CALL_RESULT", "STATE_DELTA"} {
		if got := countType(evs, ty); got != 0 {
			t.Fatalf("%s = %d, want 0 — the artifact path is a dedicated CUSTOM event, not %s", ty, got, ty)
		}
	}
	for _, ev := range evs {
		if err := ev.Validate(); err != nil {
			t.Fatalf("emitted %s failed Validate: %v", ev.Type(), err)
		}
	}
}

// TestTranslatorArtifactClosesOpenRun: an artifact Event arriving mid-text closes
// the open text run (TEXT_MESSAGE_END) BEFORE the CUSTOM event — mirroring the
// STATE_DELTA/tool-result close-first discipline so no run is left dangling.
func TestTranslatorArtifactClosesOpenRun(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		chunk("Here is the file"),
		artifactEvent(map[string]any{"path": "/abs/a.pdf", "filename": "a.pdf", "caption": "report"}),
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

// TestTranslatorArtifactCustomEventPayload: the CUSTOM event carries the artifact
// descriptor verbatim as its `value`, under a stable `name`, so a channel consumer
// (Telegram artifact.go in 13-06, CLI, AG-UI HTTP) can render path/filename/caption.
func TestTranslatorArtifactCustomEventPayload(t *testing.T) {
	descriptor := map[string]any{"path": "/abs/results.xlsx", "filename": "results.xlsx", "caption": "results"}
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{artifactEvent(descriptor)})

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
	if got.Name != artifactEventName {
		t.Fatalf("CUSTOM name = %q, want %q", got.Name, artifactEventName)
	}
	for _, k := range []string{"path", "filename", "caption"} {
		if got.Value[k] != descriptor[k] {
			t.Fatalf("CUSTOM value[%q] = %v, want %v", k, got.Value[k], descriptor[k])
		}
	}
}

// TestTranslatorArtifactGoldenShape pins the CUSTOM event against the golden
// fixture (additive — all prior fixtures stay byte-identical, asserted by the
// existing TestTranslatorGoldenShapes).
func TestTranslatorArtifactGoldenShape(t *testing.T) {
	golden := loadGolden(t)
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		artifactEvent(map[string]any{"path": "/abs/results.xlsx", "filename": "results.xlsx", "caption": "results"}),
	})
	var custom events.Event
	for _, ev := range evs {
		if string(ev.Type()) == "CUSTOM" {
			custom = ev
		}
	}
	if custom == nil {
		t.Fatalf("translator did not emit CUSTOM (got %v)", typesOf(evs))
	}
	assertGoldenShape(t, "CUSTOM", golden["CUSTOM"], custom)
}

// TestTranslatorEmptyArtifactDeltaIgnored: an Event with a present-but-EMPTY
// ArtifactDelta (len 0) does NOT emit a CUSTOM event — the branch keys on a
// non-empty descriptor, like the StateDelta branch.
func TestTranslatorEmptyArtifactDeltaIgnored(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		artifactEvent(map[string]any{}),
	})
	if got := countType(evs, "CUSTOM"); got != 0 {
		t.Fatalf("CUSTOM = %d, want 0 for an empty ArtifactDelta (%v)", got, typesOf(evs))
	}
}
