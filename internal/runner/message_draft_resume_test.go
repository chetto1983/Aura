package runner

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

func TestMessageDraftReviewRejectsUnavailableContext(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	if _, err := r.ListMessageDrafts(context.Background(), "conversation"); !errors.Is(err, messagedrafts.ErrUnavailable) {
		t.Fatalf("list without owner and store: %v", err)
	}
	if _, err := r.ResolveMessageDraft(context.Background(), "draft", "send", nil); !errors.Is(err, messagedrafts.ErrUnavailable) {
		t.Fatalf("resolve without store: %v", err)
	}
	r.messageDrafts = messagedrafts.NewStore(nil)
	ctx := identityctx.WithIdentityID(context.Background(), identityctx.LocalOperatorIdentity)
	if _, err := r.ResolveMessageDraft(ctx, "draft", "invalid", json.RawMessage(`{}`)); !errors.Is(err, messagedrafts.ErrUnavailable) {
		t.Fatalf("invalid decision reached the store: %v", err)
	}
}

func TestReviewedMessageDispositionAndAnswer(t *testing.T) {
	if status, code := reviewedMessageOutcome(nil); status != messagedrafts.StatusSent || code != "ok" {
		t.Fatalf("successful transport: %s %q", status, code)
	}
	if status, code := reviewedMessageOutcome(errors.New("connection lost")); status != messagedrafts.StatusUncertain || code != "unknown_effect" {
		t.Fatalf("unknown transport effect: %s %q", status, code)
	}
	for _, tc := range []struct {
		status messagedrafts.Status
		want   string
	}{
		{messagedrafts.StatusSent, "message sent"},
		{messagedrafts.StatusFailed, "message was not sent"},
		{messagedrafts.StatusUncertain, "message delivery is uncertain; do not retry this call automatically"},
	} {
		if got := reviewedMessageAnswer(tc.status); got != tc.want {
			t.Fatalf("%s answer = %q, want %q", tc.status, got, tc.want)
		}
	}
}
