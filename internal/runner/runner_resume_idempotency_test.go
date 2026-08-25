package runner

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
)

func TestSubmitAnswerPreservesOriginalOperationThroughAtomicResume(t *testing.T) {
	t.Parallel()

	r, _, pauses := newTestRunner(t, agenttest.NewFakeClient())
	convID := newConvID(t)
	mustCreate(t, r, convID)
	const token = "resume-token"
	resumeContext, err := resumeContextWithDecisionPolicy(json.RawMessage(`{"type":"test"}`), allResumeDecisions())
	if err != nil {
		t.Fatal(err)
	}
	if err := pauses.Insert(context.Background(), askuser.InsertParams{
		Token:          token,
		ConversationID: convID,
		Kind:           "approval",
		Question:       "approve?",
		ToolCallID:     "call-1",
		ResumeContext:  resumeContext,
	}); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := idempotency.FingerprintTyped(struct {
		Token  string `json:"token"`
		Action string `json:"action"`
	}{Token: token, Action: askuser.ActionAccept})
	if err != nil {
		t.Fatal(err)
	}
	op := idempotency.Operation{
		Key:         idempotency.OperationKey{IdentityID: identityctx.LocalOperatorIdentity, Scope: idempotency.ScopeApproval, Key: "resume-key"},
		Fingerprint: fingerprint,
	}
	ctx, err := idempotency.WithOperation(identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity), op)
	if err != nil {
		t.Fatal(err)
	}

	var hookOperation idempotency.Operation
	r.resumeHook = func(ctx context.Context, _ askuser.Pending, _ ResponseInput) error {
		var ok bool
		hookOperation, ok = idempotency.OperationFromContext(ctx)
		if !ok {
			t.Fatal("resume hook context lacks the original operation")
		}
		return nil
	}
	if _, err := r.SubmitAnswer(ctx, token, ResponseInput{Action: askuser.ActionAccept, Content: "yes"}); err != nil {
		t.Fatalf("SubmitAnswer: %v", err)
	}
	if hookOperation != op {
		t.Fatalf("resume operation = %+v, want %+v", hookOperation, op)
	}
}
