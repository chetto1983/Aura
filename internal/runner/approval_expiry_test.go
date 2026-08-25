package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

type scriptedApprovalExpiryStore struct {
	due []askuser.Pending
	err error
}

func (s *scriptedApprovalExpiryStore) ListExpiredPendingApprovals(context.Context, time.Time, int) ([]askuser.Pending, error) {
	return s.due, s.err
}

type scriptedExpiryCommitter struct {
	errs   map[string]error
	claims []ResumeClaim
}

func (s *scriptedExpiryCommitter) CommitResume(_ context.Context, claim ResumeClaim) error {
	s.claims = append(s.claims, claim)
	return s.errs[claim.Token]
}

func (*scriptedExpiryCommitter) CommitResumeBatch(context.Context, []ResumeClaim) error {
	return nil
}

func (*scriptedExpiryCommitter) CommitPause(context.Context, conversations.AppendTurnParams, []askuser.InsertParams) error {
	return nil
}

func TestExpirePendingApprovalsPersistsDistinctRefusalAndToolAnswer(t *testing.T) {
	r, conv, pause := newTestRunner(t, agenttest.NewFakeClient())
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)

	token := newConvID(t)
	if err := pause.Insert(ctx, askuser.InsertParams{
		Token: token, ConversationID: convID, Kind: "approval", Question: "approve?", ToolCallID: "call-expiry",
	}); err != nil {
		t.Fatalf("insert pause: %v", err)
	}

	expired, err := r.ExpirePendingApprovals(ctx, time.Now(), 10)
	if err != nil {
		t.Fatalf("ExpirePendingApprovals: %v", err)
	}
	if expired != 1 {
		t.Fatalf("expired = %d, want 1", expired)
	}
	answer := pause.answers[token]
	if answer.Action != askuser.ActionExpired || answer.Content != expiredApprovalContent {
		t.Fatalf("answer = %#v, want distinct expired refusal", answer)
	}
	turns := conv.turns[convID]
	if len(turns) == 0 {
		t.Fatal("expiry appended no RoleTool answer")
	}
	last := turns[len(turns)-1]
	if last.Role != llm.RoleTool || last.ToolCallID != "call-expiry" || last.Content != expiredApprovalContent {
		t.Fatalf("expiry turn = %#v, want matching RoleTool refusal", last)
	}
}

func TestExpirePendingApprovalsErrorAndConflictPaths(t *testing.T) {
	testErr := errors.New("expiry test error")
	first := askuser.Pending{Token: "first", ConversationID: "conv", ToolCallID: "call-first"}
	second := askuser.Pending{Token: "second", ConversationID: "conv", ToolCallID: "call-second"}

	t.Run("unconfigured store", func(t *testing.T) {
		expired, err := (&Runner{}).ExpirePendingApprovals(context.Background(), time.Now(), 10)
		if expired != 0 || err == nil {
			t.Fatalf("ExpirePendingApprovals = %d, %v; want 0 and error", expired, err)
		}
	})

	t.Run("list error", func(t *testing.T) {
		r := &Runner{
			approvalExpiry:  &scriptedApprovalExpiryStore{err: testErr},
			resumeCommitter: &scriptedExpiryCommitter{},
		}
		expired, err := r.ExpirePendingApprovals(context.Background(), time.Now(), 10)
		if expired != 0 || !errors.Is(err, testErr) {
			t.Fatalf("ExpirePendingApprovals = %d, %v; want 0 and wrapped list error", expired, err)
		}
	})

	t.Run("lost claim continues to next candidate", func(t *testing.T) {
		committer := &scriptedExpiryCommitter{errs: map[string]error{"first": askuser.ErrPauseNotFound}}
		r := &Runner{
			approvalExpiry:  &scriptedApprovalExpiryStore{due: []askuser.Pending{first, second}},
			resumeCommitter: committer,
		}
		expired, err := r.ExpirePendingApprovals(context.Background(), time.Now(), 10)
		if err != nil || expired != 1 {
			t.Fatalf("ExpirePendingApprovals = %d, %v; want 1, nil", expired, err)
		}
		if len(committer.claims) != 2 || committer.claims[1].Token != second.Token {
			t.Fatalf("commit claims = %#v; want both candidates in order", committer.claims)
		}
	})

	t.Run("commit error returns completed count", func(t *testing.T) {
		committer := &scriptedExpiryCommitter{errs: map[string]error{"second": testErr}}
		r := &Runner{
			approvalExpiry:  &scriptedApprovalExpiryStore{due: []askuser.Pending{first, second}},
			resumeCommitter: committer,
		}
		expired, err := r.ExpirePendingApprovals(context.Background(), time.Now(), 10)
		if expired != 1 || !errors.Is(err, testErr) {
			t.Fatalf("ExpirePendingApprovals = %d, %v; want 1 and wrapped commit error", expired, err)
		}
	})

	t.Run("hook error returns committed count", func(t *testing.T) {
		withContext := first
		withContext.ResumeContext = []byte(`{}`)
		r := &Runner{
			approvalExpiry:  &scriptedApprovalExpiryStore{due: []askuser.Pending{withContext}},
			resumeCommitter: &scriptedExpiryCommitter{},
			resumeHook: func(context.Context, askuser.Pending, ResponseInput) error {
				return testErr
			},
		}
		expired, err := r.ExpirePendingApprovals(context.Background(), time.Now(), 10)
		if expired != 1 || !errors.Is(err, testErr) {
			t.Fatalf("ExpirePendingApprovals = %d, %v; want 1 and wrapped hook error", expired, err)
		}
	})
}
