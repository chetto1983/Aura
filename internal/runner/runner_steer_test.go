package runner

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/chetto1983/aura/internal/steer/steertest"
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
	if got := SteerInboxOrNil(nil); got != nil {
		t.Fatalf("SteerInboxOrNil(nil) = %#v, want a genuinely nil interface (the Go nil-interface trap this helper exists to avoid)", got)
	}
}

// steerInjectorTool simulates "the operator typed a steer while a tool was
// running": its Execute pushes into the SAME inbox + conv key a real
// concurrent cockpit POST would use, deterministically, from inside the
// synchronous dispatch path — no goroutine/timing race needed to prove the
// drain-point ordering.
type steerInjectorTool struct {
	inbox  *steertest.Fake
	convID string
	text   string
}

func (s *steerInjectorTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "steer_inject",
		Summary:     "Test-only: pushes a steer into the shared inbox mid-dispatch.",
		Description: "Test-only: pushes a steer into the shared inbox mid-dispatch.",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
}

func (s *steerInjectorTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	if err := s.inbox.Push(s.convID, "test", s.text); err != nil {
		return tools.ToolResult{}, err
	}
	return tools.NewResult(ctx, "steer injected")
}

// assertWireValidHistory is the core STEER-03/FA-2 invariant: every assistant
// turn carrying ToolCalls must be IMMEDIATELY followed by exactly
// len(ToolCalls) RoleTool turns answering it, with no other role interposed
// — in particular, no RoleUser turn (a persisted steer) ever sits between an
// assistant tool_calls turn and the tool-result turns answering it.
func assertWireValidHistory(t *testing.T, hist []llm.Message) {
	t.Helper()
	for i := range hist {
		msg := hist[i]
		if msg.Role != llm.RoleAssistant || len(msg.ToolCalls) == 0 {
			continue
		}
		n := len(msg.ToolCalls)
		for j := 1; j <= n; j++ {
			if i+j >= len(hist) {
				t.Fatalf("assistant tool_calls turn at index %d has no matching tool result at offset %d: %+v", i, j, hist)
			}
			if hist[i+j].Role != llm.RoleTool {
				t.Fatalf("turn at index %d (role=%q) sits between the assistant tool_calls turn at %d and its tool results -- wire-invalid rehydration: %+v", i+j, hist[i+j].Role, i, hist)
			}
		}
	}
}

// TestRehydratedSteeredHistoryIsWireValid closes FA-2 (52-02-PLAN.md's
// flagged assumption about the runner's persistence ordering): drives a
// multi-round turn with a steer drained between rounds through the REAL
// agent loop, reloads history through the REAL loader (conv.LoadHistory —
// never a hand-built []llm.Message fixture), and asserts no user-role turn
// sits between an assistant tool_calls turn and its answering tool results.
func TestRehydratedSteeredHistoryIsWireValid(t *testing.T) {
	inbox := steertest.New(steer.Config{Max: 8, MaxBytes: 16384})
	convID := newConvID(t)

	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call-1", "steer_inject", "{}")),
		agenttest.ToolCallTurn(textResponseCall("call-final", "done")),
	)
	r, conv, _ := newTestRunner(t, client)
	r.steer = inbox
	r.registry.Register(&steerInjectorTool{inbox: inbox, convID: convID, text: "switch to plan B"})
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(context.Background(), convID, new("hi"))); err != nil {
		t.Fatalf("turn: %v", err)
	}

	hist, err := conv.LoadHistory(context.Background(), convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	assertWireValidHistory(t, hist)

	found := false
	for _, m := range hist {
		if m.Role == llm.RoleUser && m.Content == "switch to plan B" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no persisted RoleUser turn carrying the raw steer text: %+v", hist)
	}
}
