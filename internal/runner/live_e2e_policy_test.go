//go:build live_e2e

package runner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/identityctx"
)

// TestLiveE2E_RestrictedDecisionPolicy drives the production Runner, stores and
// openai-compatible client against real PostgreSQL and a real model. A forbidden
// decision must leave the token pending; the permitted retry must resolve that same
// token, preserve wire-valid history, and trigger a genuine fresh agent round.
func TestLiveE2E_RestrictedDecisionPolicy(t *testing.T) {
	h := newLiveHarness(t)
	ownerCtx := identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity)
	ctx, cancel := context.WithTimeout(ownerCtx, 180*time.Second)
	defer cancel()
	convID := h.newLiveConversation(t, ctx)

	token, err := h.r.MintApprovalPause(
		ctx,
		convID,
		"This operation may only be declined. Decline it?",
		nil,
		[]string{askuser.ActionDecline},
	)
	if err != nil {
		t.Fatalf("mint restricted pause: %v", err)
	}

	if _, err := h.r.SubmitAnswer(ctx, token, ResponseInput{
		Action:  askuser.ActionAccept,
		Content: "yes",
	}); !errors.Is(err, ErrResumeDecisionNotAllowed) {
		t.Fatalf("forbidden accept error = %v, want ErrResumeDecisionNotAllowed", err)
	}
	if _, resolved := h.resumedAnswerOf(t, ctx, token); resolved {
		t.Fatal("forbidden accept resolved the pause")
	}
	pending, err := h.r.PendingFor(ctx, convID)
	if err != nil {
		t.Fatalf("pending after forbidden accept: %v", err)
	}
	if len(pending) != 1 || pending[0].Token != token {
		t.Fatalf("pending after forbidden accept = %#v, want original token %s", pending, token)
	}

	if _, err := h.r.SubmitAnswer(ctx, token, ResponseInput{Action: askuser.ActionDecline}); err != nil {
		t.Fatalf("permitted decline: %v", err)
	}
	gotAnswer, resolved := h.resumedAnswerOf(t, ctx, token)
	if !resolved || gotAnswer.Action != askuser.ActionDecline {
		t.Fatalf("resolved answer = {%q,resolved:%v}, want {decline,true}", gotAnswer.Action, resolved)
	}
	history, err := h.conv.LoadHistory(ctx, convID)
	if err != nil {
		t.Fatalf("load history after permitted decline: %v", err)
	}
	assertWireValid(t, history)

	turnsBefore, err := h.conv.CountTurns(ctx, convID)
	if err != nil {
		t.Fatalf("count turns before agent re-drive: %v", err)
	}
	reply, usage, redrove := h.resumePostResume(t, ctx, convID, 3)
	if !redrove {
		t.Fatal("permitted decision drove no genuine fresh agent round")
	}
	turnsAfter, err := h.conv.CountTurns(ctx, convID)
	if err != nil {
		t.Fatalf("count turns after agent re-drive: %v", err)
	}
	if turnsAfter <= turnsBefore {
		t.Fatalf("agent re-drive did not persist a fresh turn (%d -> %d)", turnsBefore, turnsAfter)
	}
	if strings.TrimSpace(reply) != "" && usage.PromptTokens == 0 {
		t.Fatal("real agent reply reported zero prompt tokens")
	}
	t.Logf("restricted decision policy held; real agent re-drove (%d -> %d turns, prompt_tokens=%d)",
		turnsBefore, turnsAfter, usage.PromptTokens)
}
