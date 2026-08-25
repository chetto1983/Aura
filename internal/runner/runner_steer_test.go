package runner

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// runner_steer_test.go pins amendment #132 item 5/D-07: a drained steer
// persists as its own RAW-text RoleUser turn at the moment the SteerDelta
// Event arrives, never the marked/nonce form, and only for the two mid-run
// drain delivery forms.

// steerDeltaEvent builds an *agent.Event carrying the exact SteerDelta shape
// drainSteer (internal/agent/llm_agent_steer.go) produces.
func steerDeltaEvent(steers ...map[string]any) *agent.Event {
	return &agent.Event{
		RequestID: uuid.Must(uuid.NewV7()),
		Actions: agent.Actions{
			SteerDelta: map[string]any{
				"conversation_id": "conv-steer",
				"round":           uint32(1),
				"steers":          steers,
			},
		},
	}
}

// nonceMarkerRe matches the wrapUserSteer envelope (internal/agent's
// steerMarkerOpen/steerMarkerClose) so the test can assert the persisted
// content never carries it.
var nonceMarkerRe = regexp.MustCompile(`<user_steer nonce="[0-9a-f]+">`)

func TestPersistedSteerCarriesRawText(t *testing.T) {
	t.Run("tool_result_append form persists the raw text", func(t *testing.T) {
		r, conv, _ := newTestRunner(t, nil)
		convID := newConvID(t)
		mustCreate(t, r, convID)
		tr := &turnTracker{convID: convID}

		const raw = "switch to plan B"
		ev := steerDeltaEvent(map[string]any{
			"id": "s1", "source": "cockpit", "text": raw, "delivery": "tool_result_append",
		})
		if err := r.persistEvent(context.Background(), tr, ev); err != nil {
			t.Fatalf("persistEvent: %v", err)
		}
		hist, err := conv.LoadHistory(context.Background(), convID)
		if err != nil {
			t.Fatalf("LoadHistory: %v", err)
		}
		if len(hist) != 1 {
			t.Fatalf("history = %d turns, want 1 persisted RoleUser turn: %+v", len(hist), hist)
		}
		got := hist[0]
		if got.Role != llm.RoleUser {
			t.Fatalf("persisted role = %q, want RoleUser", got.Role)
		}
		if got.Content != raw {
			t.Fatalf("persisted content = %q, want the RAW text %q", got.Content, raw)
		}
		if nonceMarkerRe.MatchString(got.Content) {
			t.Fatalf("persisted content carries the marker/nonce envelope: %q", got.Content)
		}
		if strings.Contains(got.Content, "<user_steer") {
			t.Fatalf("persisted content carries the marker tag: %q", got.Content)
		}
	})

	t.Run("user_message_fallback form also persists the raw text", func(t *testing.T) {
		r, conv, _ := newTestRunner(t, nil)
		convID := newConvID(t)
		mustCreate(t, r, convID)
		tr := &turnTracker{convID: convID}

		const raw = "actually, stop and summarize"
		ev := steerDeltaEvent(map[string]any{
			"id": "s2", "source": "telegram", "text": raw, "delivery": "user_message_fallback",
		})
		if err := r.persistEvent(context.Background(), tr, ev); err != nil {
			t.Fatalf("persistEvent: %v", err)
		}
		hist, err := conv.LoadHistory(context.Background(), convID)
		if err != nil {
			t.Fatalf("LoadHistory: %v", err)
		}
		if len(hist) != 1 || hist[0].Content != raw {
			t.Fatalf("history = %+v, want one RoleUser turn with content %q", hist, raw)
		}
	})

	t.Run("multiple drained steers persist in FIFO order, each its own turn", func(t *testing.T) {
		r, conv, _ := newTestRunner(t, nil)
		convID := newConvID(t)
		mustCreate(t, r, convID)
		tr := &turnTracker{convID: convID}

		ev := steerDeltaEvent(
			map[string]any{"id": "s1", "source": "cockpit", "text": "first", "delivery": "tool_result_append"},
			map[string]any{"id": "s2", "source": "cockpit", "text": "second", "delivery": "tool_result_append"},
		)
		if err := r.persistEvent(context.Background(), tr, ev); err != nil {
			t.Fatalf("persistEvent: %v", err)
		}
		hist, err := conv.LoadHistory(context.Background(), convID)
		if err != nil {
			t.Fatalf("LoadHistory: %v", err)
		}
		if len(hist) != 2 || hist[0].Content != "first" || hist[1].Content != "second" {
			t.Fatalf("history = %+v, want [first, second] in order", hist)
		}
	})

	t.Run("an unrecognized delivery form is not persisted here", func(t *testing.T) {
		// Guards 52-05's leftover auto-delivery form: that form drives a whole
		// follow-on turn whose OWN AppendTurn persists the leftover text, so
		// persistSteerTurn must skip it or the leftover would land as two
		// byte-identical RoleUser rows (STEER-04).
		r, conv, _ := newTestRunner(t, nil)
		convID := newConvID(t)
		mustCreate(t, r, convID)
		tr := &turnTracker{convID: convID}

		ev := steerDeltaEvent(map[string]any{
			"id": "s3", "source": "cockpit", "text": "leftover", "delivery": "auto_delivery_next_turn",
		})
		if err := r.persistEvent(context.Background(), tr, ev); err != nil {
			t.Fatalf("persistEvent: %v", err)
		}
		hist, err := conv.LoadHistory(context.Background(), convID)
		if err != nil {
			t.Fatalf("LoadHistory: %v", err)
		}
		if len(hist) != 0 {
			t.Fatalf("history = %+v, want 0 turns for an unrecognized delivery form", hist)
		}
	})

	t.Run("nil SteerDelta is a no-op", func(t *testing.T) {
		r, conv, _ := newTestRunner(t, nil)
		convID := newConvID(t)
		mustCreate(t, r, convID)
		tr := &turnTracker{convID: convID}

		if err := r.persistEvent(context.Background(), tr, &agent.Event{}); err != nil {
			t.Fatalf("persistEvent: %v", err)
		}
		hist, err := conv.LoadHistory(context.Background(), convID)
		if err != nil {
			t.Fatalf("LoadHistory: %v", err)
		}
		if len(hist) != 0 {
			t.Fatalf("history = %+v, want 0 turns", hist)
		}
	})
}

func TestSteerInboxOrNil(t *testing.T) {
	if got := steerInboxOrNil(nil); got != nil {
		t.Fatalf("steerInboxOrNil(nil) = %#v, want a genuinely nil interface (the Go nil-interface trap this helper exists to avoid)", got)
	}
}
