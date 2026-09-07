package runner

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/askuser"
)

func TestWorkerResumeClaimCarriesFenceAndOwner(t *testing.T) {
	r := &Runner{}
	pending := askuser.Pending{ConversationID: "parent", ToolCallID: "child-call", PendingActionID: "fence", OwningWorkerID: "job"}
	claim := r.resumeClaim("token", pending, ResponseInput{Action: askuser.ActionAccept, Content: "9"})
	if claim.ExpectActionID != "fence" || claim.OwningWorkerID != "job" || claim.Answer.Content != "9" {
		t.Fatalf("lost worker resume identity: %+v", claim)
	}
	for _, action := range []string{askuser.ActionAccept, askuser.ActionDecline} {
		if got := classifyResolve(pending, action, 0); got.Outcome == OutcomeContinue {
			t.Fatalf("worker answer restarted parent: %+v", got)
		}
	}
}

func TestSplitWorkerResumeDoesNotAppendParentToolResult(t *testing.T) {
	for _, batch := range []bool{false, true} {
		conv, pause := newFakeConvStore(), newFakePauseStore()
		convID := newConvID(t)
		token := seedPendingPause(t, pause, convID, "child-call")
		claim := acceptClaim(token, convID, "child-call", "9")
		claim.OwningWorkerID = "worker-job"
		committer := newSplitResumeCommitter(conv, pause)
		var err error
		if batch {
			err = committer.CommitResumeBatch(context.Background(), []ResumeClaim{claim})
		} else {
			err = committer.CommitResume(context.Background(), claim)
		}
		if err != nil {
			t.Fatal(err)
		}
		if count, _ := conv.CountTurns(context.Background(), convID); count != 0 {
			t.Fatalf("worker answer appended %d parent turns", count)
		}
	}
}
