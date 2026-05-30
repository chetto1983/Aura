package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/llm"
)

// choiceCall builds an ask_user(kind=choice) call carrying 2 options + a priority so
// the options/priority branches of assistantAskUserToolCalls + pauseOptionsJSON run.
func choiceCall(id string) llm.ToolCall {
	return agenttest.MakeToolCall(id, "ask_user",
		`{"question":"Pick one","kind":"choice","priority":50,"options":["red","blue"]}`)
}

// TestSubmitAnswers_BatchResolvesAndInjects asserts SubmitAnswers resolves all
// pendings atomically and injects each as a RoleTool answer, returning remaining 0.
func TestSubmitAnswers_BatchResolvesAndInjects(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			askUserCall("call-1", "A?", "clarification"),
			askUserCall("call-2", "B?", "clarification"),
		),
		agenttest.ToolCallTurn(textResponseCall("call-3", "All set.")),
	)
	r, _, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, userPtr("go"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, err := pause.ListPending(ctx, convID)
	if err != nil || len(pending) != 2 {
		t.Fatalf("want 2 pending, got %d (err=%v)", len(pending), err)
	}
	answers := map[string]ResponseInput{
		pending[0].Token: {Action: askuser.ActionAccept, Content: "ans-a"},
		pending[1].Token: {Action: askuser.ActionAccept, Content: "ans-b"},
	}
	remaining, err := r.SubmitAnswers(ctx, answers)
	if err != nil {
		t.Fatalf("submit answers: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("want 0 remaining, got %d", remaining)
	}
	if _, err := drain(r.Turn(ctx, convID, nil)); err != nil {
		t.Fatalf("resume turn: %v", err)
	}
	req := client.LastRequest()
	answersSeen := 0
	for _, m := range req.Messages {
		if m.Role == llm.RoleTool && (strings.Contains(m.Content, "ans-a") || strings.Contains(m.Content, "ans-b")) {
			answersSeen++
		}
	}
	if answersSeen != 2 {
		t.Fatalf("want both injected answers in the resume request, got %d", answersSeen)
	}
}

// TestSubmitAnswer_Decline injects the "user declined" RoleTool marker.
func TestSubmitAnswer_Decline(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(askUserCall("call-1", "Proceed?", "approval")),
		agenttest.ToolCallTurn(textResponseCall("call-2", "Okay, skipping.")),
	)
	r, conv, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, userPtr("hi"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, _ := pause.ListPending(ctx, convID)
	if _, err := r.SubmitAnswer(ctx, pending[0].Token, ResponseInput{Action: askuser.ActionDecline}); err != nil {
		t.Fatalf("decline: %v", err)
	}
	hist, err := conv.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	sawDeclined := false
	for _, m := range hist {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, "declined") {
			sawDeclined = true
		}
	}
	if !sawDeclined {
		t.Fatal("expected a 'user declined' RoleTool turn")
	}
}

// TestSubmitAnswer_Cancel routes through the auto-resolve path (turn aborted).
func TestSubmitAnswer_Cancel(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			askUserCall("call-1", "A?", "clarification"),
			askUserCall("call-2", "B?", "clarification"),
		),
	)
	r, _, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, userPtr("go"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, _ := pause.ListPending(ctx, convID)
	remaining, err := r.SubmitAnswer(ctx, pending[0].Token, ResponseInput{Action: askuser.ActionCancel})
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("cancel must auto-resolve all pendings, remaining=%d", remaining)
	}
	if pause.unresolvedCount(convID) != 0 {
		t.Fatalf("want 0 unresolved after cancel, got %d", pause.unresolvedCount(convID))
	}
}

// TestSubmitAnswer_UnknownToken surfaces ErrPauseNotFound.
func TestSubmitAnswer_UnknownToken(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	if _, err := r.SubmitAnswer(context.Background(), newConvID(t), ResponseInput{Action: askuser.ActionAccept}); err == nil {
		t.Fatal("expected an error for an unknown token")
	}
}

// TestChoicePause_OptionsPersisted exercises the options + priority branches of the
// pause-persistence path and the final terminal answer with usage.
func TestChoicePause_OptionsPersisted(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(choiceCall("call-1")),
		agenttest.WithUsage(
			agenttest.ToolCallTurn(textResponseCall("call-2", "You picked red.")),
			llm.Usage{PromptTokens: 12, CompletionTokens: 4},
		),
	)
	r, _, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(ctx, convID, userPtr("choose"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, _ := pause.ListPending(ctx, convID)
	if len(pending) != 1 {
		t.Fatalf("want 1 pending, got %d", len(pending))
	}
	if pending[0].Priority != 50 {
		t.Fatalf("want priority 50, got %d", pending[0].Priority)
	}
	if len(pending[0].Options) == 0 {
		t.Fatal("want options persisted on the pending row")
	}
	if _, err := r.SubmitAnswer(ctx, pending[0].Token, ResponseInput{Action: askuser.ActionAccept, Content: "red"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := drain(r.Turn(ctx, convID, nil)); err != nil {
		t.Fatalf("resume: %v", err)
	}
}

// TestNewConversation_MintsID covers the id-minting path + the lifecycle helpers.
func TestNewConversation_MintsID(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient())
	ctx := context.Background()
	id, err := r.NewConversation(ctx)
	if err != nil {
		t.Fatalf("new conversation: %v", err)
	}
	if id == "" {
		t.Fatal("want a non-empty conversation id")
	}
	if err := conv.Rename(ctx, id, "My Chat"); err != nil {
		t.Fatal(err)
	}
	list, err := conv.List(ctx, false)
	if err != nil || len(list) != 1 {
		t.Fatalf("want 1 conversation listed, got %d (err=%v)", len(list), err)
	}
	if err := conv.UpdateStatus(ctx, id, "archived"); err != nil {
		t.Fatal(err)
	}
	if list, _ := conv.List(ctx, false); len(list) != 0 {
		t.Fatalf("archived conversation should be hidden from active list, got %d", len(list))
	}
	if err := conv.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
}

// TestTurn_LoadHistoryError surfaces a managed-history load failure on the error slot.
func TestTurn_LoadHistoryError(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient())
	conv.manErr = errFake
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	if _, err := drain(r.Turn(ctx, convID, userPtr("hi"))); err == nil {
		t.Fatal("expected the managed-history error to surface")
	}
}

// TestTurn_ContinueAfterResumeWithoutUserMsg covers the userMsg=nil continue path
// directly (a resume that drives a fresh round over existing history).
func TestTurn_ContinueAfterResumeWithoutUserMsg(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(textResponseCall("call-1", "Continued.")),
	)
	r, _, _ := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	seedSystemTurn(t, r, convID)
	if _, err := drain(r.Turn(ctx, convID, nil)); err != nil {
		t.Fatalf("continue turn: %v", err)
	}
}
