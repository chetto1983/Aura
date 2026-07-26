package orderingcontrol

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type memoryRecallDecisionSourceFunc func(
	context.Context,
	runner.DynamicRecallInput,
) (toolDecision, error)

func (fn memoryRecallDecisionSourceFunc) decideMemoryRecall(
	ctx context.Context,
	input runner.DynamicRecallInput,
) (toolDecision, error) {
	return fn(ctx, input)
}

func TestDynamicRecallRuntimeConstructorAndSource(t *testing.T) {
	t.Parallel()
	if NewDynamicRecall(nil, "openrouter", "production-model") != nil {
		t.Fatal("nil pool produced a dynamic recall control")
	}
	if NewDynamicRecall(
		new(pgxpool.Pool),
		"openrouter",
		"production-model",
	) == nil {
		t.Fatal("live pool did not produce a dynamic recall control")
	}

	input := memoryRecallInput()
	decision := memoryRecallShadowDecision(t, input)
	policy := decision.Policy
	policy.Config = runtimePolicyConfigJSON(t, decision)
	source := runtimeSourceForPolicy(policy, decision.Snapshot)
	got, err := source.decideMemoryRecall(t.Context(), input)
	if err != nil {
		t.Fatalf("decideMemoryRecall: %v", err)
	}
	if got.RecommendedAction != adaptive.StaticActionID ||
		got.Snapshot != decision.Snapshot {
		t.Fatalf("memory decision = %#v", got)
	}
}

func TestDynamicRecallControlFailureBoundaries(t *testing.T) {
	t.Parallel()
	input := memoryRecallInput()
	decision := memoryRecallShadowDecision(t, input)
	source := memoryRecallDecisionSourceFunc(func(
		context.Context,
		runner.DynamicRecallInput,
	) (toolDecision, error) {
		return decision, nil
	})
	recorder := &recordingToolEvents{}
	if newDynamicRecall(nil, recorder) != nil ||
		newDynamicRecall(source, nil) != nil {
		t.Fatal("dynamic recall accepted missing production ports")
	}
	var nilControl *dynamicRecall
	if prepared, err := nilControl.PrepareDynamicRecall(
		t.Context(),
		input,
		nil,
	); prepared != nil || err != nil {
		t.Fatalf("nil control = (%#v, %v)", prepared, err)
	}

	boundaryErr := errors.New("boundary unavailable")
	t.Run("assignment persistence", func(t *testing.T) {
		control := newDynamicRecall(
			source,
			&recordingToolEvents{assignmentErr: boundaryErr},
		)
		prepared, err := control.PrepareDynamicRecall(
			t.Context(),
			input,
			func(
				context.Context,
				string,
				string,
				int,
			) (runner.DynamicRecall, error) {
				t.Fatal("provider ran before assignment persistence")
				return runner.DynamicRecall{}, nil
			},
		)
		if prepared != nil || !errors.Is(err, boundaryErr) {
			t.Fatalf("assignment failure = (%#v, %v)", prepared, err)
		}
	})

	t.Run("delivery persistence", func(t *testing.T) {
		control := newDynamicRecall(
			source,
			&recordingToolEvents{deliveryErr: boundaryErr},
		)
		prepared, err := control.PrepareDynamicRecall(
			t.Context(),
			input,
			func(
				context.Context,
				string,
				string,
				int,
			) (runner.DynamicRecall, error) {
				t.Fatal("static diagnostic called the provider")
				return runner.DynamicRecall{}, nil
			},
		)
		if err != nil {
			t.Fatalf("PrepareDynamicRecall: %v", err)
		}
		if err := prepared.Commit(
			t.Context(),
			agent.DynamicTailOutcome{},
		); !errors.Is(err, boundaryErr) {
			t.Fatalf("delivery persistence error = %v", err)
		}
	})

	t.Run("invalid recommendation", func(t *testing.T) {
		invalidDecision := decision
		invalidDecision.RecommendedAction = "unregistered"
		recorder := &recordingToolEvents{}
		control := newDynamicRecall(
			memoryRecallDecisionSourceFunc(func(
				context.Context,
				runner.DynamicRecallInput,
			) (toolDecision, error) {
				return invalidDecision, nil
			}),
			recorder,
		)
		prepared, err := control.PrepareDynamicRecall(
			t.Context(),
			input,
			func(
				context.Context,
				string,
				string,
				int,
			) (runner.DynamicRecall, error) {
				t.Fatal("invalid recommendation called the provider")
				return runner.DynamicRecall{}, nil
			},
		)
		if err != nil || prepared != nil ||
			len(recorder.assignments) != 0 ||
			len(recorder.deliveries) != 0 {
			t.Fatalf(
				"invalid recommendation = (%#v, %v), facts=%d/%d",
				prepared,
				err,
				len(recorder.assignments),
				len(recorder.deliveries),
			)
		}
	})
}

func TestDynamicRecallRejectsInvalidAssignmentContracts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*runner.DynamicRecallInput, *toolDecision)
	}{
		{
			name: "missing owner",
			mutate: func(
				input *runner.DynamicRecallInput,
				_ *toolDecision,
			) {
				input.OwnerID = uuid.Nil
			},
		},
		{
			name: "missing request",
			mutate: func(
				input *runner.DynamicRecallInput,
				_ *toolDecision,
			) {
				input.RequestID = uuid.Nil
			},
		},
		{
			name: "below eligible threshold",
			mutate: func(
				input *runner.DynamicRecallInput,
				_ *toolDecision,
			) {
				input.MaxItems = 3
			},
		},
		{
			name: "catalog does not match maximum",
			mutate: func(
				input *runner.DynamicRecallInput,
				_ *toolDecision,
			) {
				input.CandidateActions = runner.DynamicRecallCatalog(true, 4)
			},
		},
		{
			name: "snapshot scope mismatch",
			mutate: func(
				_ *runner.DynamicRecallInput,
				decision *toolDecision,
			) {
				decision.ProviderID = "other-provider"
			},
		},
		{
			name: "missing policy epoch",
			mutate: func(
				_ *runner.DynamicRecallInput,
				decision *toolDecision,
			) {
				decision.Policy.Epoch = 0
			},
		},
		{
			name: "non-serving mode",
			mutate: func(
				_ *runner.DynamicRecallInput,
				decision *toolDecision,
			) {
				decision.Policy.Mode = adaptive.PolicyOff
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := memoryRecallInput()
			decision := memoryRecallShadowDecision(t, input)
			test.mutate(&input, &decision)
			if _, err := newMemoryRecallAssignment(
				input,
				decision,
			); err == nil {
				t.Fatal("invalid memory assignment contract was accepted")
			}
		})
	}
}

func TestDynamicRecallFourItemCatalogStaysStatic(t *testing.T) {
	t.Parallel()
	input := memoryRecallInput()
	input.MaxItems = 4
	input.CandidateActions = runner.DynamicRecallCatalog(true, 4)
	decision := memoryRecallShadowDecision(t, input)
	assignment, err := newMemoryRecallAssignment(input, decision)
	if err != nil {
		t.Fatalf("newMemoryRecallAssignment: %v", err)
	}
	if assignment.IntendedActionID != adaptive.StaticActionID ||
		assignment.Features[adaptive.FeatureRecallLimit] != 4 ||
		len(assignment.ActionProbabilities) != 2 ||
		assignment.ActionProbabilities[0].Probability != 1 ||
		assignment.ActionProbabilities[1].Probability != 0 {
		t.Fatalf("four-item assignment = %#v", assignment)
	}
}

func TestDynamicRecallStaticDeliveryRejectsInvalidAssignment(t *testing.T) {
	t.Parallel()
	if _, err := newMemoryRecallStaticDelivery(
		adaptive.Assignment{},
	); err == nil {
		t.Fatal("static delivery accepted an invalid assignment")
	}
}

func memoryRecallInput() runner.DynamicRecallInput {
	return runner.DynamicRecallInput{
		OwnerID:   uuid.Must(uuid.NewV7()),
		RequestID: uuid.Must(uuid.NewV7()),
		Query:     "remember my preferences",
		CandidateActions: []runner.DynamicRecallAction{
			runner.DynamicRecallStatic,
			runner.DynamicRecallTop4,
			runner.DynamicRecallTop8,
		},
		MaxItems:   8,
		ProviderID: "openrouter",
		ModelID:    "production-model",
	}
}

func memoryRecallShadowDecision(
	t *testing.T,
	input runner.DynamicRecallInput,
) toolDecision {
	t.Helper()
	snapshot, err := adaptive.NewPolicySnapshot(adaptive.SnapshotSpec{
		Scope: adaptive.SnapshotScope{
			OwnerID:        input.OwnerID,
			Domain:         adaptive.DomainMemoryRecall,
			Point:          adaptive.PointMemoryRecall,
			ProviderID:     input.ProviderID,
			ModelID:        input.ModelID,
			PolicyVersion:  "policy-v1",
			TrainingCutoff: time.Now().UTC().Add(-time.Hour),
		},
		ChampionActionID: adaptive.StaticActionID,
		FeatureSchema: []adaptive.SnapshotFeatureDefinition{
			{Key: adaptive.FeatureQueryLength, Center: 0, Scale: 1},
			{Key: adaptive.FeatureRecallLimit, Center: 0, Scale: 1},
		},
		Actions: memoryRecallSnapshotActions(input.CandidateActions),
	})
	if err != nil {
		t.Fatalf("NewPolicySnapshot: %v", err)
	}
	return toolDecision{
		Policy: adaptive.Policy{
			Epoch:   1,
			Version: "policy-v1",
			Mode:    adaptive.PolicyShadow,
		},
		Snapshot:          snapshot,
		Environment:       adaptive.EvaluationProductionCanary,
		RecommendedAction: string(runner.DynamicRecallTop4),
		ProviderID:        input.ProviderID,
		ModelID:           input.ModelID,
	}
}

func memoryRecallSnapshotActions(
	actions []runner.DynamicRecallAction,
) []adaptive.SnapshotAction {
	snapshotActions := make([]adaptive.SnapshotAction, len(actions))
	for index, action := range actions {
		snapshotActions[index] = adaptive.SnapshotAction{
			ActionID: string(action),
		}
	}
	return snapshotActions
}
