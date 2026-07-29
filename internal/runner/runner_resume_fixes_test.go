package runner

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/llm"
)

// TestCancel_InjectError_CR01 surfaces an AppendTurn failure while injecting the
// terminating cancel answers (the cancelConversation inject-error branch).
func TestCancel_InjectError_CR01(t *testing.T) {
	client := agenttest.NewFakeClient(agenttest.ToolCallTurn(askUserCall("call-1", "Q?", "clarification")))
	r, conv, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	if _, err := drain(r.Turn(ctx, convID, new("go"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, _ := pause.ListPending(ctx, convID)
	conv.appendEr = errFake
	if _, err := r.SubmitAnswer(ctx, pending[0].Token, ResponseInput{Action: askuser.ActionCancel}); err == nil {
		t.Fatal("expected an inject error from the cancel path")
	}
}

// TestCancel_RehydratedHistoryWireValid_CR01 is the regression for CR-01: after a
// cancel the persisted history must be wire-valid (the assistant ask_user tool_call
// is answered by a matching RoleTool turn), and a subsequent Turn(userMsg) must build
// a valid request — no dangling tool_call → no provider 400.
func TestCancel_RehydratedHistoryWireValid_CR01(t *testing.T) {
	client := agenttest.NewFakeClient(
		// Turn 1: the model emits an ask_user pause.
		agenttest.ToolCallTurn(askUserCall("call-1", "What city?", "clarification")),
		// Turn 2 (a brand-new user message AFTER the cancel): a terminal text_response.
		agenttest.ToolCallTurn(textResponseCall("call-2", "Fresh start.")),
	)
	r, conv, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, new("Where am I?"))); err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	pending, err := pause.ListPending(ctx, convID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("want 1 pending, got %d (err=%v)", len(pending), err)
	}

	directive, err := r.SubmitAnswer(ctx, pending[0].Token, ResponseInput{Action: askuser.ActionCancel})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if directive.Remaining != 0 {
		t.Fatalf("cancel must auto-resolve all pendings, remaining=%d", directive.Remaining)
	}
	if pause.unresolvedCount(convID) != 0 {
		t.Fatalf("want 0 unresolved after cancel, got %d", pause.unresolvedCount(convID))
	}

	// The persisted history after cancel must be wire-valid: the assistant ask_user
	// tool_call has a matching RoleTool answer (no dangling tool_call → no 400).
	hist, err := conv.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	assertWireValid(t, hist)
	sawCancelAnswer := false
	for _, m := range hist {
		if m.Role == llm.RoleTool && m.ToolCallID == "call-1" && m.Content == cancelledContent {
			sawCancelAnswer = true
		}
	}
	if !sawCancelAnswer {
		t.Fatalf("CR-01: cancel must inject a terminating RoleTool answer keyed to call-1; history=%+v", hist)
	}

	// A subsequent Turn with a fresh user message must build a valid request — the
	// dangling tool_call would have produced a provider error otherwise.
	if _, err := drain(r.Turn(ctx, convID, new("Hello again"))); err != nil {
		t.Fatalf("turn after cancel must succeed over wire-valid history: %v", err)
	}
	req := client.LastRequest()
	assertWireValid(t, req.Messages)
}

// TestCancelBatch_RehydratedHistoryWireValid_CR01 is the CR-01 regression for the
// SubmitAnswers (batch) cancel branch: a 2-pause round cancelled via SubmitAnswers
// must answer BOTH dangling ask_user tool_calls so history stays wire-valid.
func TestCancelBatch_RehydratedHistoryWireValid_CR01(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			askUserCall("call-1", "A?", "clarification"),
			askUserCall("call-2", "B?", "clarification"),
		),
	)
	r, conv, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, new("go"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, _ := pause.ListPending(ctx, convID)
	if len(pending) != 2 {
		t.Fatalf("want 2 pending, got %d", len(pending))
	}
	// One cancel in the batch aborts the whole conversation turn.
	answers := map[string]ResponseInput{
		pending[0].Token: {Action: askuser.ActionCancel},
		pending[1].Token: {Action: askuser.ActionAccept, Content: "ignored"},
	}
	remaining, err := r.SubmitAnswers(ctx, answers)
	if err != nil {
		t.Fatalf("submit answers (cancel): %v", err)
	}
	if remaining != 0 {
		t.Fatalf("cancel must auto-resolve all pendings, remaining=%d", remaining)
	}
	if pause.unresolvedCount(convID) != 0 {
		t.Fatalf("want 0 unresolved after cancel, got %d", pause.unresolvedCount(convID))
	}

	hist, err := conv.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	assertWireValid(t, hist)
	answered := map[string]bool{}
	for _, m := range hist {
		if m.Role == llm.RoleTool && m.Content == cancelledContent {
			answered[m.ToolCallID] = true
		}
	}
	if !answered["call-1"] || !answered["call-2"] {
		t.Fatalf("CR-01 batch: both ask_user calls must be answered on cancel, got %v", answered)
	}
}

// TestStop_RehydratedHistoryWireValid_CR01 locks the Stop path: force-stop with an
// active pause must inject a synthetic cancel tool answer before auto-resolving,
// otherwise a later resume sees an assistant tool_call with no RoleTool response.
func TestStop_RehydratedHistoryWireValid_CR01(t *testing.T) {
	client := agenttest.NewFakeClient(agenttest.ToolCallTurn(askUserCall("call-1", "Continue?", "approval")))
	r, conv, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, new("pause then stop"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if pause.unresolvedCount(convID) != 1 {
		t.Fatalf("want 1 unresolved before Stop, got %d", pause.unresolvedCount(convID))
	}
	if err := r.Stop(ctx, convID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	hist, err := conv.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	assertWireValid(t, hist)
	var sawCancel bool
	for _, m := range hist {
		if m.Role == llm.RoleTool && m.ToolCallID == "call-1" && m.Content == cancelledContent {
			sawCancel = true
		}
	}
	if !sawCancel {
		t.Fatalf("Stop must inject a cancel RoleTool for call-1; history=%+v", hist)
	}
}

// TestPause_AssistantTurnPersistedWhenConsumerStopsOnPause is the live-found loop
// regression (Telegram): the AG-UI translator returns on the pause interrupt, so the
// consumer STOPS iterating on the pause Event. The combined assistant ask_user
// tool_call turn must still be persisted (the deferred flush) — otherwise the injected
// answer is orphaned on resume (a tool result with no matching assistant tool_call) and
// the model re-asks ask_user every resume (an infinite pause loop).
func TestPause_AssistantTurnPersistedWhenConsumerStopsOnPause(t *testing.T) {
	client := agenttest.NewFakeClient(agenttest.ToolCallTurn(askUserCall("call-1", "Quale nome?", "approval")))
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	// Drive the turn but STOP on the pause Event — mimics the AG-UI translator returning
	// on the interrupt outcome (yield returns false to runner.Turn → its early return).
	var sawPause bool
	for ev, err := range r.Turn(ctx, convID, new("dammi un nome")) {
		if err != nil {
			t.Fatalf("turn: %v", err)
		}
		if ev.Actions.AwaitingInput != nil {
			sawPause = true
			break
		}
	}
	if !sawPause {
		t.Fatal("expected a pause Event")
	}

	// Answer the pause (the user's button tap) and load the resume history: it must be
	// wire-valid — the injected tool answer attaches to the persisted assistant ask_user
	// tool_call. Without the deferred flush the assistant turn is missing, the answer is
	// orphaned, and the model re-asks ask_user forever.
	pending, err := r.PendingFor(ctx, convID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("want exactly 1 pending pause, got %d (err=%v)", len(pending), err)
	}
	if _, err := r.SubmitAnswer(ctx, pending[0].Token, ResponseInput{Action: askuser.ActionAccept, Content: "Kai"}); err != nil {
		t.Fatalf("submit answer: %v", err)
	}
	hist, err := conv.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	assertWireValid(t, hist) // [user][assistant ask_user call-1][tool "Kai" call-1] — orphaned without the fix
	var found bool
	for _, m := range hist {
		if m.Role != llm.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == "ask_user" && tc.ID == "call-1" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("the assistant ask_user tool_call turn was not persisted after a consumer stop "+
			"(resume orphans the answer → ask_user pause loop); history=%+v", hist)
	}
}
