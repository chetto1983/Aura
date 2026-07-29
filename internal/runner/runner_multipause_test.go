package runner

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/llm"
)

// assertWireValid asserts the OpenAI tool-call contract over a rehydrated history:
// every assistant message carrying tool_calls is IMMEDIATELY followed by exactly one
// RoleTool message per tool_call id, in id order — no dangling tool_call, no
// orphan/interleaved tool answer (CR-01 / CR-02 invariant).
func assertWireValid(t *testing.T, msgs []llm.Message) {
	t.Helper()
	i := 0
	for i < len(msgs) {
		m := msgs[i]
		if m.Role == llm.RoleTool {
			t.Fatalf("wire-invalid: RoleTool at index %d has no preceding assistant tool_calls message (tool_call_id=%q)", i, m.ToolCallID)
		}
		if m.Role != llm.RoleAssistant || len(m.ToolCalls) == 0 {
			i++
			continue
		}
		// The next len(ToolCalls) messages must be the matching tool answers.
		want := make(map[string]bool, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			want[tc.ID] = true
		}
		for j := 0; j < len(m.ToolCalls); j++ {
			idx := i + 1 + j
			if idx >= len(msgs) {
				t.Fatalf("wire-invalid: assistant tool_calls at index %d has %d calls but only %d following messages", i, len(m.ToolCalls), len(msgs)-i-1)
			}
			tm := msgs[idx]
			if tm.Role != llm.RoleTool {
				t.Fatalf("wire-invalid: expected RoleTool at index %d responding to the assistant tool_calls at %d, got role %q", idx, i, tm.Role)
			}
			if !want[tm.ToolCallID] {
				t.Fatalf("wire-invalid: RoleTool at index %d (tool_call_id=%q) does not match any tool_call of the assistant message at %d", idx, tm.ToolCallID, i)
			}
			delete(want, tm.ToolCallID)
		}
		if len(want) != 0 {
			t.Fatalf("wire-invalid: assistant tool_calls at index %d left unanswered ids: %v", i, want)
		}
		i += 1 + len(m.ToolCalls)
	}
}

// countAssistantPauseTurns counts persisted assistant turns that carry ask_user
// tool_calls (the pause turns), and returns the max number of tool_calls on any one
// of them. Used to assert a 2-pause round persists exactly ONE such turn (CR-02).
func countAssistantPauseTurns(msgs []llm.Message) (turns, maxCalls int) {
	for _, m := range msgs {
		if m.Role != llm.RoleAssistant || len(m.ToolCalls) == 0 {
			continue
		}
		hasAskUser := false
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == "ask_user" {
				hasAskUser = true
			}
		}
		if !hasAskUser {
			continue
		}
		turns++
		if len(m.ToolCalls) > maxCalls {
			maxCalls = len(m.ToolCalls)
		}
	}
	return turns, maxCalls
}

// TestMultiPause_SingleAssistantTurn_CR02 is the regression for CR-02: a 2-pause
// round must persist exactly ONE assistant turn carrying BOTH ask_user tool_calls,
// and after answering both the rehydrated sequence must be wire-valid
// (assistant{[A,B]}, tool{A}, tool{B}).
func TestMultiPause_SingleAssistantTurn_CR02(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			askUserCall("call-1", "A?", "clarification"),
			askUserCall("call-2", "B?", "clarification"),
		),
		agenttest.ToolCallTurn(textResponseCall("call-3", "Both answered.")),
	)
	r, conv, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, new("go"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if pause.inserts != 2 {
		t.Fatalf("want 2 paused_states inserts (N rows), got %d", pause.inserts)
	}

	// Exactly ONE assistant pause turn carrying BOTH tool_calls.
	hist, err := conv.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	turns, maxCalls := countAssistantPauseTurns(hist)
	if turns != 1 {
		t.Fatalf("CR-02: want exactly 1 assistant pause turn, got %d (history=%+v)", turns, hist)
	}
	if maxCalls != 2 {
		t.Fatalf("CR-02: the single assistant pause turn must carry BOTH ask_user tool_calls, got %d", maxCalls)
	}

	// Answer both, then assert the rehydrated sequence is wire-valid.
	pending, _ := pause.ListPending(ctx, convID)
	if len(pending) != 2 {
		t.Fatalf("want 2 pending, got %d", len(pending))
	}
	answers := map[string]ResponseInput{
		pending[0].Token: {Action: askuser.ActionAccept, Content: "ans-a"},
		pending[1].Token: {Action: askuser.ActionAccept, Content: "ans-b"},
	}
	if _, err := r.SubmitAnswers(ctx, answers); err != nil {
		t.Fatalf("submit answers: %v", err)
	}

	hist, err = conv.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	assertWireValid(t, hist)

	// The resume request the agent sends must also be wire-valid.
	if _, err := drain(r.Turn(ctx, convID, nil)); err != nil {
		t.Fatalf("resume turn: %v", err)
	}
	assertWireValid(t, client.LastRequest().Messages)
}
