package runner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
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

	if _, err := drain(r.Turn(ctx, convID, new("go"))); err != nil {
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
	if err := r.Stop(ctx, convID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	// Auto-title may issue a later LLM request, so the resume request is not
	// guaranteed to be LastRequest. Search all recorded requests for the injected
	// RoleTool answers.
	sawA, sawB := false, false
	for _, req := range client.Requests {
		for _, m := range req.Messages {
			if m.Role != llm.RoleTool {
				continue
			}
			if strings.Contains(m.Content, "ans-a") {
				sawA = true
			}
			if strings.Contains(m.Content, "ans-b") {
				sawB = true
			}
		}
	}
	if !sawA || !sawB {
		t.Fatalf("want both injected answers in a resume request, got ans-a=%v ans-b=%v; requests=%+v", sawA, sawB, client.Requests)
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

	if _, err := drain(r.Turn(ctx, convID, new("hi"))); err != nil {
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

func TestSubmitAnswer_RunsResumeHookForContext(t *testing.T) {
	r, _, pause := newTestRunner(t, agenttest.NewFakeClient())
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	token := "pause-token"
	resumeContext, err := resumeContextWithDecisionPolicy(
		json.RawMessage(`{"type":"skill_approval","skill_name":"calc"}`),
		allResumeDecisions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := pause.Insert(ctx, askuser.InsertParams{
		Token:          token,
		ConversationID: convID,
		Kind:           "approval",
		Question:       "approve skill?",
		ToolCallID:     "tc1",
		ResumeContext:  resumeContext,
	}); err != nil {
		t.Fatalf("seed pending: %v", err)
	}

	called := false
	r.resumeHook = func(_ context.Context, p askuser.Pending, resp ResponseInput) error {
		called = true
		if p.Token != token || p.ConversationID != convID || string(p.ResumeContext) != string(resumeContext) {
			t.Fatalf("hook pending = %+v, want token/context %s/%s", p, token, resumeContext)
		}
		if resp.Action != askuser.ActionAccept || resp.Content != "yes" {
			t.Fatalf("hook response = %+v, want accept/yes", resp)
		}
		pause.mu.Lock()
		_, resumed := pause.answers[token]
		pause.mu.Unlock()
		if !resumed {
			t.Fatal("resume hook must run after the paused row is marked resumed")
		}
		return nil
	}

	directive, err := r.SubmitAnswer(ctx, token, ResponseInput{Action: askuser.ActionAccept, Content: "yes"})
	if err != nil {
		t.Fatalf("SubmitAnswer: %v", err)
	}
	if directive.Remaining != 0 {
		t.Fatalf("remaining = %d, want 0", directive.Remaining)
	}
	if !called {
		t.Fatal("resume hook was not called")
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

	if _, err := drain(r.Turn(ctx, convID, new("go"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, _ := pause.ListPending(ctx, convID)
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

	if _, err := drain(r.Turn(ctx, convID, new("choose"))); err != nil {
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

// TestNewConversationOwnedByPrincipal proves MUSR-02: a new conversation is owned by the
// AUTHENTICATED PRINCIPAL carried on ctx (identityctx.WithIdentityID, threaded from
// agui.withPrincipal), and falls back to `local` only when no principal is present. This
// REPLACES the pre-Phase-36 TestNewConversationPrefersUserIdentityOverLegacyLocal, which
// asserted a "prefer the first user identity" scan — the cross-identity ownership bug this
// closes (with two users A and B, B's new conversation would be mis-owned by A).
func TestNewConversationOwnedByPrincipal(t *testing.T) {
	const operatorID = "11111111-1111-1111-1111-111111111111"
	const localID = "00000000-0000-0000-0000-000000000001"

	conv := newFakeConvStore()
	ids := newFakeIdentityStore()
	ids.byName["operator"] = identity.Identity{ID: operatorID, Name: "operator", Kind: "user"}
	r := New(Deps{Conv: conv, Pause: newFakePauseStore(), Identity: ids, Client: agenttest.NewFakeClient(), LLM: llm.Config{Model: "m"}})

	// With a principal on ctx: that principal owns the conversation (B owns B's thread).
	const principalConv = "22222222-2222-2222-2222-222222222222"
	principalCtx := identityctx.WithIdentityID(context.Background(), operatorID)
	if _, err := r.NewConversationWithID(principalCtx, principalConv); err != nil {
		t.Fatalf("new conversation (principal): %v", err)
	}
	got, err := conv.Get(context.Background(), principalConv)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if got.IdentityID != operatorID {
		t.Fatalf("principal conversation owner = %q, want the principal %q", got.IdentityID, operatorID)
	}

	// With NO principal: `local` owns the conversation (CLI / no-auth fallback, D-25).
	const localConv = "44444444-4444-4444-4444-444444444444"
	if _, err := r.NewConversationWithID(context.Background(), localConv); err != nil {
		t.Fatalf("new conversation (no principal): %v", err)
	}
	gotLocal, err := conv.Get(context.Background(), localConv)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if gotLocal.IdentityID != localID {
		t.Fatalf("no-principal conversation owner = %q, want local %q", gotLocal.IdentityID, localID)
	}
}

func TestNewConversationFallsBackToLegacyLocalWhenNoUserIdentityExists(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient())
	const convID = "33333333-3333-3333-3333-333333333333"

	if _, err := r.NewConversationWithID(context.Background(), convID); err != nil {
		t.Fatalf("new conversation: %v", err)
	}
	got, err := conv.Get(context.Background(), convID)
	if err != nil {
		t.Fatalf("get conversation: %v", err)
	}
	if got.IdentityID != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("conversation owner = %q, want legacy local fallback", got.IdentityID)
	}
}

// TestTurn_LoadHistoryError surfaces a managed-history load failure on the error slot.
func TestTurn_LoadHistoryError(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient())
	conv.manErr = errFake
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	if _, err := drain(r.Turn(ctx, convID, new("hi"))); err == nil {
		t.Fatal("expected the managed-history error to surface")
	}
}

// TestPendingFor_ReturnsFIFO covers the PendingFor accessor the REPL uses.
func TestPendingFor_ReturnsFIFO(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			askUserCall("call-1", "A?", "clarification"),
			askUserCall("call-2", "B?", "clarification"),
		),
	)
	r, _, _ := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	if _, err := drain(r.Turn(ctx, convID, new("go"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, err := r.PendingFor(ctx, convID)
	if err != nil {
		t.Fatalf("pending for: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("want 2 pending, got %d", len(pending))
	}
}

// TestSubmitAnswers_InjectError surfaces an AppendTurn failure during the batch append.
// The default committer here is the pool-less NON-ATOMIC split fallback, so this asserts
// only that the failure is not swallowed. ATOMIC retryability (an append failure AFTER the
// claim rolls the whole cross-store tx back → resumed_at IS NULL → retry works) is a
// property of the pool-owning committer and is proven against live Postgres in
// runner_resume_single_atomic_integration_test.go (LOOP-03/F-029).
func TestSubmitAnswers_InjectError(t *testing.T) {
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
	answers := map[string]ResponseInput{pending[0].Token: {Action: "accept", Content: "x"}}
	if _, err := r.SubmitAnswers(ctx, answers); err == nil {
		t.Fatal("expected an append error from the batch path")
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
