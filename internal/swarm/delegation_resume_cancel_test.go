package swarm

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

func TestCancelledWorkerPauseBecomesTerminalControlIntent(t *testing.T) {
	resume := &DelegationResumeState{PendingActionID: "cancel-fence", PendingToolCallID: "ask-choice"}
	store := &fakeResumeStore{jobs: []documents.AnsweredAwaitingInputJob{{
		JobID: "job-cancel", IdentityID: "identity-1", PendingActionID: "cancel-fence",
		Payload: answeredJobPayload(t, resume), ResumedAnswer: []byte(`{"action":"cancel","content":"<auto-terminated: conversation ended>"}`),
	}}}
	n, err := NewDelegationResumeObserver(store).ProcessOnce(context.Background(), "identity-1", 10)
	if err != nil || n != 1 || len(store.unparked) != 1 {
		t.Fatalf("cancel observer: n=%d err=%v", n, err)
	}
	payload, err := delegationPayloadFromMap(store.unparked[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	if !payload.OperatorCancelled {
		t.Fatal("explicit cancel would rebuild the model as a normal answer")
	}
}
