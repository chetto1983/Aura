package runner

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

type adaptiveCommitCall struct {
	turn   conversations.AppendTurnParams
	metric *sqlc.InsertCacheMetricParams
	event  adaptive.Event
}

type recordingAdaptiveCommitter struct {
	calls []adaptiveCommitCall
}

func (r *recordingAdaptiveCommitter) CommitTurn(
	_ context.Context,
	turn conversations.AppendTurnParams,
	metric *sqlc.InsertCacheMetricParams,
	event adaptive.Event,
) error {
	r.calls = append(r.calls, adaptiveCommitCall{turn: turn, metric: metric, event: event})
	return nil
}

func TestRunnerPersistenceUsesAtomicAdaptiveCommitterAtToolAndCompletionControlPoints(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	recorder := &recordingAdaptiveCommitter{}
	r.adaptiveCommitter = recorder
	owner := uuid.Must(uuid.NewV7())
	ctx := identityctx.WithIdentityID(context.Background(), owner.String())
	requestID := uuid.Must(uuid.NewV7())
	now := time.Now().UTC()
	tracker := &turnTracker{
		convID:        newConvID(t),
		openToolCalls: map[string]struct{}{"call-1": {}},
	}

	toolEvent := &agent.Event{
		RequestID: requestID,
		Timestamp: now,
		Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
			Event: agent.ToolInvocationEnd, ToolCallID: "call-1", ToolName: "document_search",
			Status: "ok", DurationMS: 17, ResultBytes: 120,
		}},
	}
	if err := r.persistEvent(ctx, tracker, toolEvent); err != nil {
		t.Fatalf("persist tool event: %v", err)
	}

	completion := &agent.Event{
		RequestID: requestID,
		Timestamp: now.Add(time.Second),
		LLMResponse: &agent.LLMResponse{
			Content: "done", FinishReason: "stop",
		},
		Actions: agent.Actions{StateDelta: map[string]any{
			"prompt_tokens": 12, "completion_tokens": 4,
		}},
	}
	if err := r.persistEvent(ctx, tracker, completion); err != nil {
		t.Fatalf("persist completion event: %v", err)
	}

	if len(recorder.calls) != 2 {
		t.Fatalf("adaptive commit calls = %d, want tool + completion", len(recorder.calls))
	}
	if recorder.calls[0].metric != nil || recorder.calls[0].turn.ToolCallID != "call-1" {
		t.Fatalf("tool commit = %+v, want tool turn without cache metric", recorder.calls[0])
	}
	if recorder.calls[1].metric == nil || recorder.calls[1].turn.Content != "done" {
		t.Fatalf("completion commit = %+v, want answer + cache metric", recorder.calls[1])
	}
	wantDecisions := []uuid.UUID{
		adaptive.DecisionIDForPoint(requestID, "tool:call-1"),
		adaptive.DecisionIDForPoint(requestID, "reasoning"),
	}
	for i, call := range recorder.calls {
		if call.event.OwnerID != owner || call.event.DecisionID != wantDecisions[i] ||
			call.event.AggregateID != requestID.String() || call.event.Kind != adaptive.EventOutcome {
			t.Fatalf("adaptive call[%d] scope/correlation = %+v", i, call.event)
		}
	}
}
