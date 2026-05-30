package runner

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/llm"
)

// TestTurn_AppendUserTurnError surfaces a persistence failure on the user turn.
func TestTurn_AppendUserTurnError(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient())
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	conv.appendEr = errFake
	if _, err := drain(r.Turn(ctx, convID, userPtr("hi"))); err == nil {
		t.Fatal("expected the append-user-turn error to surface")
	}
}

// TestTurn_CountTurnsError surfaces a nextSeq failure (CountTurns error).
func TestTurn_CountTurnsError(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient())
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	conv.countErr = errFake
	if _, err := drain(r.Turn(ctx, convID, userPtr("hi"))); err == nil {
		t.Fatal("expected the count-turns error to surface")
	}
}

// TestNewConversation_IdentityNotFound surfaces a missing local identity.
func TestNewConversation_IdentityNotFound(t *testing.T) {
	conv := newFakeConvStore()
	pause := newFakePauseStore()
	id := &fakeIdentityStore{byName: map[string]identity.Identity{}} // empty registry
	r := New(Deps{Conv: conv, Pause: pause, Identity: id, Client: agenttest.NewFakeClient(), LLM: llm.Config{Model: "m"}})
	if _, err := r.NewConversation(context.Background()); err == nil {
		t.Fatal("expected an error when the local identity is absent")
	}
}

// TestStop_WorkerTimeout exercises the bounded wg.Wait() timeout branch by holding a
// worker open past a tiny StopTimeout. The worker is released afterward so goleak
// (TestMain) still sees a clean drain at package end.
func TestStop_WorkerTimeout(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	r.stopTimeout = 10 * time.Millisecond
	release := make(chan struct{})
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		<-release
	}()
	err := r.Stop(context.Background(), newConvID(t))
	close(release) // let the worker finish so goleak is clean
	if err == nil {
		t.Fatal("expected a drain-timeout error from Stop")
	}
}

// TestTurn_RunInfraError surfaces a real agent run error (Stream failure) on the
// Turn error slot.
func TestTurn_RunInfraError(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient(agenttest.FakeTurn{Err: errFake}))
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	if _, err := drain(r.Turn(ctx, convID, userPtr("hi"))); err == nil {
		t.Fatal("expected the agent run error to surface")
	}
}

// TestSubmitAnswers_UnknownToken surfaces a GetByToken failure in the batch path.
func TestSubmitAnswers_UnknownToken(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	answers := map[string]ResponseInput{newConvID(t): {Action: "accept", Content: "x"}}
	if _, err := r.SubmitAnswers(context.Background(), answers); err == nil {
		t.Fatal("expected an error for an unknown token in the batch")
	}
}

// TestSubmitAnswers_Cancel routes the whole conversation through auto-resolve.
func TestSubmitAnswers_Cancel(t *testing.T) {
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
	answers := map[string]ResponseInput{
		pending[0].Token: {Action: "cancel"},
		pending[1].Token: {Action: "accept", Content: "b"},
	}
	if _, err := r.SubmitAnswers(ctx, answers); err != nil {
		t.Fatalf("submit answers (cancel): %v", err)
	}
	if pause.unresolvedCount(convID) != 0 {
		t.Fatalf("cancel must auto-resolve all, remaining=%d", pause.unresolvedCount(convID))
	}
}

// TestSubmitAnswer_InjectAnswerError surfaces an AppendTurn failure during injection.
func TestSubmitAnswer_InjectAnswerError(t *testing.T) {
	client := agenttest.NewFakeClient(agenttest.ToolCallTurn(askUserCall("call-1", "Q?", "clarification")))
	r, conv, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	if _, err := drain(r.Turn(ctx, convID, userPtr("go"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, _ := pause.ListPending(ctx, convID)
	conv.appendEr = errFake
	if _, err := r.SubmitAnswer(ctx, pending[0].Token, ResponseInput{Action: "accept", Content: "x"}); err == nil {
		t.Fatal("expected an inject-answer error to surface")
	}
}

// TestTurn_TerminalAnswerWithCost covers the cost-bearing assistant-answer persist
// branch (u.Cost != nil → CostUSD folded into the aggregate).
func TestTurn_TerminalAnswerWithCost(t *testing.T) {
	cost := 0.0042
	client := agenttest.NewFakeClient(
		agenttest.WithUsage(
			agenttest.ToolCallTurn(textResponseCall("call-1", "Done.")),
			llm.Usage{PromptTokens: 8, CompletionTokens: 3, Cost: &cost},
		),
	)
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	if _, err := drain(r.Turn(ctx, convID, userPtr("hi"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	c, err := conv.Get(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	if c.TotalCostUSD <= 0 {
		t.Fatalf("want a non-zero aggregated cost, got %v", c.TotalCostUSD)
	}
}

// TestPersistPause_AppendError surfaces an AppendTurn failure during the pause
// assistant-turn persist (before the paused_states row is written).
func TestPersistPause_AppendError(t *testing.T) {
	client := agenttest.NewFakeClient(agenttest.ToolCallTurn(askUserCall("call-1", "Q?", "approval")))
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	// Fail only the SECOND AppendTurn (the pause assistant turn) — the first is the
	// user turn that must persist so the pause-persist path is the one that errors.
	conv.failAppN = 2
	if _, err := drain(r.Turn(ctx, convID, userPtr("go"))); err == nil {
		t.Fatal("expected the pause persist error to surface")
	}
}

// TestUsageFromStateDelta covers the StateDelta usage reconstruction helper across
// the int/int64/float64 widenings and the cost pointer.
func TestUsageFromStateDelta(t *testing.T) {
	if u := usageFromStateDelta(nil); u != (llm.Usage{}) {
		t.Fatalf("nil delta must yield zero usage, got %+v", u)
	}
	cost := 0.0125
	u := usageFromStateDelta(map[string]any{
		"prompt_tokens":     int64(10),
		"completion_tokens": 5,
		"cache_hit_tokens":  float64(3),
		"cost_usd":          cost,
	})
	if u.PromptTokens != 10 || u.CompletionTokens != 5 || u.CachedTokens != 3 {
		t.Fatalf("token widening wrong: %+v", u)
	}
	if u.Cost == nil || *u.Cost != cost {
		t.Fatalf("want cost %v, got %v", cost, u.Cost)
	}
}
