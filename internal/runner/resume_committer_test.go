package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

// seedPendingPause inserts one pending pause into the in-memory fake and returns its
// token. The committer unit tests exercise the pool-less splitResumeCommitter (the
// Pool impl's cross-store tx is db_integration-only — RESEARCH gotcha #10).
func seedPendingPause(t *testing.T, pause *fakePauseStore, convID, toolCallID string) string {
	t.Helper()
	token := newConvID(t)
	if err := pause.Insert(context.Background(), askuser.InsertParams{
		Token: token, ConversationID: convID, Kind: "clarification",
		Question: "?", ToolCallID: toolCallID,
	}); err != nil {
		t.Fatalf("seed pause: %v", err)
	}
	return token
}

func acceptClaim(token, convID, toolCallID, content string) ResumeClaim {
	return ResumeClaim{
		Token:  token,
		Answer: askuser.ResumeAnswer{Action: askuser.ActionAccept, Content: content},
		Turn: conversations.AppendTurnParams{
			ConversationID: convID, Role: llm.RoleTool, ToolCallID: toolCallID, Content: content,
		},
	}
}

// TestSplitResumeCommitter_CommitResume_ClaimsBeforeAppend proves the default
// (pool-less) committer claims the pause BEFORE appending the answer turn: an
// already-resumed token fails the claim and NO second answer turn is appended (the
// D-06 idempotency contract, non-atomically preserved by the fallback).
func TestSplitResumeCommitter_CommitResume_ClaimsBeforeAppend(t *testing.T) {
	conv := newFakeConvStore()
	pause := newFakePauseStore()
	convID := newConvID(t)
	token := seedPendingPause(t, pause, convID, "call-1")
	c := newSplitResumeCommitter(conv, pause)
	claim := acceptClaim(token, convID, "call-1", "yes")

	if err := c.CommitResume(context.Background(), claim); err != nil {
		t.Fatalf("first CommitResume: %v", err)
	}
	n1, _ := conv.CountTurns(context.Background(), convID)
	if n1 != 1 {
		t.Fatalf("want exactly 1 answer turn after resume, got %d", n1)
	}

	dupErr := c.CommitResume(context.Background(), claim)
	if !errors.Is(dupErr, askuser.ErrPauseNotFound) {
		t.Fatalf("duplicate CommitResume: want ErrPauseNotFound, got %v", dupErr)
	}
	n2, _ := conv.CountTurns(context.Background(), convID)
	if n2 != n1 {
		t.Fatalf("a duplicate resume must NOT append a second answer turn: %d -> %d", n1, n2)
	}
}

// TestSplitResumeCommitter_CommitResumeBatch_ClaimsAllBeforeAppendAll proves the batch
// claims EVERY pause before appending ANY answer: if one token is already resolved the
// whole batch claim fails and nothing is appended (all-or-nothing, replacing the
// pre-34-06 inject-first bug).
func TestSplitResumeCommitter_CommitResumeBatch_ClaimsAllBeforeAppendAll(t *testing.T) {
	conv := newFakeConvStore()
	pause := newFakePauseStore()
	convID := newConvID(t)
	tokA := seedPendingPause(t, pause, convID, "call-a")
	tokB := seedPendingPause(t, pause, convID, "call-b")
	c := newSplitResumeCommitter(conv, pause)

	// Resolve tokB out-of-band so the batch's claim-all step must fail before any append.
	if err := pause.MarkResumed(context.Background(), tokB, askuser.ResumeAnswer{Action: askuser.ActionAccept, Content: "pre"}); err != nil {
		t.Fatalf("pre-resolve tokB: %v", err)
	}
	claims := []ResumeClaim{
		acceptClaim(tokA, convID, "call-a", "a"),
		acceptClaim(tokB, convID, "call-b", "b"),
	}
	err := c.CommitResumeBatch(context.Background(), claims)
	if !errors.Is(err, askuser.ErrPauseNotFound) {
		t.Fatalf("batch with an already-resolved token: want ErrPauseNotFound, got %v", err)
	}
	if n, _ := conv.CountTurns(context.Background(), convID); n != 0 {
		t.Fatalf("a failed batch claim must append NO answer turns, got %d", n)
	}

	// A clean batch (both still pending): claim-all then append-all.
	conv2 := newFakeConvStore()
	pause2 := newFakePauseStore()
	convID2 := newConvID(t)
	tok1 := seedPendingPause(t, pause2, convID2, "c1")
	tok2 := seedPendingPause(t, pause2, convID2, "c2")
	c2 := newSplitResumeCommitter(conv2, pause2)
	if err := c2.CommitResumeBatch(context.Background(), []ResumeClaim{
		acceptClaim(tok1, convID2, "c1", "one"),
		acceptClaim(tok2, convID2, "c2", "two"),
	}); err != nil {
		t.Fatalf("clean batch: %v", err)
	}
	if n, _ := conv2.CountTurns(context.Background(), convID2); n != 2 {
		t.Fatalf("a clean 2-pause batch must append exactly 2 answer turns, got %d", n)
	}
	if pause2.unresolvedCount(convID2) != 0 {
		t.Fatalf("a clean batch must resolve both pauses, %d remain", pause2.unresolvedCount(convID2))
	}
}

// TestSplitResumeCommitter_CommitPause_InsertsPausesAndAppendsAssistant proves the
// pause-exposure commit writes N paused_states rows AND the single assistant tool_call
// turn (the pool-less non-atomic equivalent of D-05).
func TestSplitResumeCommitter_CommitPause_InsertsPausesAndAppendsAssistant(t *testing.T) {
	conv := newFakeConvStore()
	pause := newFakePauseStore()
	convID := newConvID(t)
	c := newSplitResumeCommitter(conv, pause)

	assistantTurn := conversations.AppendTurnParams{
		ConversationID: convID, Role: llm.RoleAssistant, ToolCalls: []byte(`[{"id":"call-1"}]`),
	}
	pauses := []askuser.InsertParams{
		{Token: newConvID(t), ConversationID: convID, Kind: "clarification", Question: "A?", ToolCallID: "call-1"},
		{Token: newConvID(t), ConversationID: convID, Kind: "clarification", Question: "B?", ToolCallID: "call-2"},
	}
	if err := c.CommitPause(context.Background(), assistantTurn, pauses); err != nil {
		t.Fatalf("CommitPause: %v", err)
	}
	if pause.inserts != 2 {
		t.Fatalf("CommitPause must insert N=2 paused_states rows, got %d", pause.inserts)
	}
	if n, _ := conv.CountTurns(context.Background(), convID); n != 1 {
		t.Fatalf("CommitPause must append exactly 1 assistant tool_call turn, got %d", n)
	}
}

// TestNew_DefaultsToSplitResumeCommitter proves runner.New falls back to the pool-less
// splitResumeCommitter when Deps.ResumeCommitter is nil, so existing pool-less call
// sites (cache_audit, unit tests) compile and run unchanged.
func TestNew_DefaultsToSplitResumeCommitter(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	if r.resumeCommitter == nil {
		t.Fatal("New must default resumeCommitter when Deps.ResumeCommitter is nil")
	}
	if _, ok := r.resumeCommitter.(*splitResumeCommitter); !ok {
		t.Fatalf("the nil-default committer must be *splitResumeCommitter, got %T", r.resumeCommitter)
	}
}
