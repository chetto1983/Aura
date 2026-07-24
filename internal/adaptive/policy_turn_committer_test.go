package adaptive

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
)

type recordingAdaptiveTurnWriter struct{ calls int }

func (w *recordingAdaptiveTurnWriter) CommitTurn(
	context.Context,
	conversations.AppendTurnParams,
	*sqlc.InsertCacheMetricParams,
	Event,
) error {
	w.calls++
	return nil
}

type recordingBaselineTurnWriter struct {
	turns      int
	assistants int
}

func (w *recordingBaselineTurnWriter) AppendTurn(context.Context, conversations.AppendTurnParams) error {
	w.turns++
	return nil
}

func (w *recordingBaselineTurnWriter) AppendAssistantTurnWithCacheMetric(
	context.Context,
	conversations.AppendTurnParams,
	sqlc.InsertCacheMetricParams,
) error {
	w.assistants++
	return nil
}

func TestPolicyAwareTurnCommitterFallsBackOnOffRollbackAndPartition(t *testing.T) {
	reader := &mutablePolicyReader{policy: Policy{Epoch: 1, Version: "shadow", Mode: PolicyShadow}}
	adaptiveWriter := &recordingAdaptiveTurnWriter{}
	baseline := &recordingBaselineTurnWriter{}
	committer := NewPolicyAwareTurnCommitter(NewPolicyController(reader), adaptiveWriter, baseline)
	event := Event{DecisionID: uuid.Must(uuid.NewV7())}
	turn := conversations.AppendTurnParams{ConversationID: "conversation", Role: "tool"}

	if err := committer.CommitTurn(context.Background(), turn, nil, event); err != nil {
		t.Fatal(err)
	}
	if adaptiveWriter.calls != 1 || baseline.turns != 0 {
		t.Fatalf("shadow calls adaptive=%d baseline=%d, want 1/0", adaptiveWriter.calls, baseline.turns)
	}

	reader.policy = Policy{Epoch: 2, Version: "rollback", Mode: PolicyRollback}
	if err := committer.CommitTurn(context.Background(), turn, nil, event); err != nil {
		t.Fatal(err)
	}
	reader.err = errors.New("postgres partition")
	metric := &sqlc.InsertCacheMetricParams{}
	if err := committer.CommitTurn(context.Background(), turn, metric, event); err != nil {
		t.Fatal(err)
	}
	if adaptiveWriter.calls != 1 || baseline.turns != 1 || baseline.assistants != 1 {
		t.Fatalf("fallback calls adaptive=%d turns=%d assistants=%d, want 1/1/1",
			adaptiveWriter.calls, baseline.turns, baseline.assistants)
	}
}
