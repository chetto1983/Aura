package reasoningcontrol

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/google/uuid"
)

type decisionSourceFunc func(context.Context, agent.ReasoningControlInput) (Decision, error)

func (fn decisionSourceFunc) DecideReasoning(
	ctx context.Context,
	input agent.ReasoningControlInput,
) (Decision, error) {
	return fn(ctx, input)
}

type eventRecorder struct {
	mu            sync.Mutex
	order         *[]string
	assignments   []adaptive.Assignment
	deliveries    []adaptive.Delivery
	assignmentErr error
	deliveryErr   error
}

func (recorder *eventRecorder) RecordAssignment(
	_ context.Context,
	assignment adaptive.Assignment,
) (int64, error) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.order != nil {
		*recorder.order = append(*recorder.order, "assignment")
	}
	if recorder.assignmentErr != nil {
		return 0, recorder.assignmentErr
	}
	recorder.assignments = append(recorder.assignments, assignment)
	return int64(len(recorder.assignments)), nil
}

func (recorder *eventRecorder) RecordDelivery(
	_ context.Context,
	_ uuid.UUID,
	_ uuid.UUID,
	delivery adaptive.Delivery,
) (int64, error) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.order != nil {
		*recorder.order = append(*recorder.order, "delivery")
	}
	if recorder.deliveryErr != nil {
		return 0, recorder.deliveryErr
	}
	recorder.deliveries = append(recorder.deliveries, delivery)
	return int64(len(recorder.deliveries)), nil
}

func (recorder *eventRecorder) snapshot() ([]adaptive.Assignment, []adaptive.Delivery) {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return slices.Clone(recorder.assignments), slices.Clone(recorder.deliveries)
}

func testInput() agent.ReasoningControlInput {
	return agent.ReasoningControlInput{
		OwnerID: uuid.Must(uuid.NewV7()), RequestID: uuid.Must(uuid.NewV7()),
		PointOrdinal: 1, ProviderID: "openrouter", ModelID: "test-model",
		StaticTier: prompt.ReasoningTierLow, StaticTierSet: true,
	}
}

func testSnapshot(
	t *testing.T,
	input agent.ReasoningControlInput,
	policyVersion string,
) *adaptive.PolicySnapshot {
	t.Helper()
	return testSnapshotWithActions(t, input, policyVersion, actionCatalog)
}

func testSnapshotWithActions(
	t *testing.T,
	input agent.ReasoningControlInput,
	policyVersion string,
	actionIDs []string,
) *adaptive.PolicySnapshot {
	t.Helper()
	actions := make([]adaptive.SnapshotAction, len(actionIDs))
	for index, actionID := range actionIDs {
		actions[index] = adaptive.SnapshotAction{ActionID: actionID}
	}
	snapshot, err := adaptive.NewPolicySnapshot(adaptive.SnapshotSpec{
		Scope: adaptive.SnapshotScope{
			OwnerID: input.OwnerID, Domain: adaptive.DomainReasoning,
			Point: adaptive.PointReasoning, ProviderID: input.ProviderID,
			ModelID: input.ModelID, PolicyVersion: policyVersion,
			TrainingCutoff: time.Unix(1, 0).UTC(),
		},
		ChampionActionID: adaptive.StaticActionID,
		FeatureSchema: []adaptive.SnapshotFeatureDefinition{{
			Key: adaptive.FeatureMessageCount, Center: 0, Scale: 1,
		}},
		Actions: actions,
	})
	if err != nil {
		t.Fatalf("NewPolicySnapshot: %v", err)
	}
	return snapshot
}

func shadowDecision(snapshot *adaptive.PolicySnapshot) Decision {
	return Decision{
		Policy: adaptive.Policy{
			Epoch: 1, Version: "policy-v1", Mode: adaptive.PolicyShadow,
		},
		Snapshot: snapshot, Environment: adaptive.EvaluationProductionCanary,
		RecommendedTier: prompt.ReasoningTierHigh,
	}
}

func staticFailure(t *testing.T) agent.StaticReasoningRequestBuilder {
	t.Helper()
	return func(context.Context) (agent.PreparedReasoningRequest, error) {
		t.Fatal("static fallback ran unexpectedly")
		return agent.PreparedReasoningRequest{}, nil
	}
}

var _ DecisionSource = decisionSourceFunc(nil)
var _ EventRecorder = (*eventRecorder)(nil)
