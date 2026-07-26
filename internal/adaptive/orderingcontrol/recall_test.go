package orderingcontrol

import (
	"context"
	"errors"
	"slices"
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

func TestDynamicRecallPersistsAssignmentBeforeProviderAndDeliveryOnCommit(
	t *testing.T,
) {
	t.Parallel()
	input := memoryRecallInput()
	decision := memoryRecallShadowDecision(t, input)
	order := []string{}
	recorder := &recordingToolEvents{order: &order}
	control := newDynamicRecall(
		memoryRecallDecisionSourceFunc(func(
			context.Context,
			runner.DynamicRecallInput,
		) (toolDecision, error) {
			return decision, nil
		}),
		recorder,
	)
	provider := func(
		context.Context,
		string,
		string,
		int,
	) (runner.DynamicRecall, error) {
		order = append(order, "provider")
		if len(recorder.assignments) != 1 {
			t.Fatal("provider ran before durable assignment")
		}
		return validControlDynamicRecall(), nil
	}

	prepared, err := control.PrepareDynamicRecall(
		t.Context(),
		input,
		provider,
	)
	if err != nil {
		t.Fatalf("PrepareDynamicRecall: %v", err)
	}
	if prepared == nil || prepared.Action != runner.DynamicRecallTop8 {
		t.Fatalf("prepared = %#v, want top-8 override", prepared)
	}
	if !slices.Equal(order, []string{"assignment", "provider"}) {
		t.Fatalf("pre-commit order = %v", order)
	}
	if err := prepared.Commit(
		t.Context(),
		agent.DynamicTailOutcome{Delivered: true},
	); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := prepared.Commit(
		t.Context(),
		agent.DynamicTailOutcome{Delivered: true},
	); err != nil {
		t.Fatalf("duplicate Commit: %v", err)
	}
	if !slices.Equal(order, []string{"assignment", "provider", "delivery"}) {
		t.Fatalf("final order = %v", order)
	}
	for _, delivery := range recorder.deliveries {
		if delivery.Status != adaptive.DeliverySuccess ||
			delivery.ActualActionID != string(runner.DynamicRecallTop8) ||
			delivery.ResultCount != 2 {
			t.Fatalf("delivery = %#v", delivery)
		}
	}
}

func TestDynamicRecallContextBudgetCommitsNone(t *testing.T) {
	t.Parallel()
	input := memoryRecallInput()
	decision := memoryRecallShadowDecision(t, input)
	recorder := &recordingToolEvents{}
	control := newDynamicRecall(
		memoryRecallDecisionSourceFunc(func(
			context.Context,
			runner.DynamicRecallInput,
		) (toolDecision, error) {
			return decision, nil
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
			return validControlDynamicRecall(), nil
		},
	)
	if err != nil {
		t.Fatalf("PrepareDynamicRecall: %v", err)
	}
	if err := prepared.Commit(t.Context(), agent.DynamicTailOutcome{
		FallbackReason: agent.DynamicTailFallbackContextBudget,
	}); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	for _, delivery := range recorder.deliveries {
		if delivery.ActualActionID != adaptive.ActionNoneID ||
			delivery.FallbackReason != adaptive.FallbackContextBudget ||
			delivery.ExposureKnown ||
			delivery.ExposureProbability != nil {
			t.Fatalf("context-budget delivery = %#v", delivery)
		}
	}
}

func TestDynamicRecallProviderFailureRecordsStaticFallback(t *testing.T) {
	t.Parallel()
	input := memoryRecallInput()
	decision := memoryRecallShadowDecision(t, input)
	recorder := &recordingToolEvents{}
	control := newDynamicRecall(
		memoryRecallDecisionSourceFunc(func(
			context.Context,
			runner.DynamicRecallInput,
		) (toolDecision, error) {
			return decision, nil
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
			return runner.DynamicRecall{}, errors.New("memory unavailable")
		},
	)
	if err != nil || prepared != nil {
		t.Fatalf("provider fallback = (%#v, %v), want nil without turn error", prepared, err)
	}
	if len(recorder.assignments) != 1 || len(recorder.deliveries) != 1 {
		t.Fatalf(
			"facts = %d assignments/%d deliveries, want 1/1",
			len(recorder.assignments),
			len(recorder.deliveries),
		)
	}
	for _, delivery := range recorder.deliveries {
		if delivery.ActualActionID != adaptive.StaticActionID ||
			delivery.FallbackReason != adaptive.FallbackStrategyFailed {
			t.Fatalf("provider fallback delivery = %#v", delivery)
		}
	}
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

func TestDynamicRecallFailsClosedBeforeProvider(t *testing.T) {
	t.Parallel()
	input := memoryRecallInput()
	decision := memoryRecallShadowDecision(t, input)
	tests := []struct {
		name   string
		mutate func(*runner.DynamicRecallInput, *toolDecision)
	}{
		{
			name: "policy off",
			mutate: func(
				_ *runner.DynamicRecallInput,
				decision *toolDecision,
			) {
				decision.Policy.Mode = adaptive.PolicyOff
			},
		},
		{
			name: "empty query",
			mutate: func(
				input *runner.DynamicRecallInput,
				_ *toolDecision,
			) {
				input.Query = ""
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testInput := input
			testDecision := decision
			test.mutate(&testInput, &testDecision)
			recorder := &recordingToolEvents{}
			control := newDynamicRecall(
				memoryRecallDecisionSourceFunc(func(
					context.Context,
					runner.DynamicRecallInput,
				) (toolDecision, error) {
					return testDecision, nil
				}),
				recorder,
			)
			providerCalled := false
			prepared, err := control.PrepareDynamicRecall(
				t.Context(),
				testInput,
				func(
					context.Context,
					string,
					string,
					int,
				) (runner.DynamicRecall, error) {
					providerCalled = true
					return validControlDynamicRecall(), nil
				},
			)
			if err != nil || prepared != nil || providerCalled ||
				len(recorder.assignments) != 0 {
				t.Fatalf(
					"fail-closed result = (%#v, %v), provider=%t, assignments=%d",
					prepared,
					err,
					providerCalled,
					len(recorder.assignments),
				)
			}
		})
	}
}

func TestDynamicRecallControlFailureBoundaries(t *testing.T) {
	t.Parallel()
	input := memoryRecallInput()
	decision := memoryRecallShadowDecision(t, input)
	recorder := &recordingToolEvents{}
	source := memoryRecallDecisionSourceFunc(func(
		context.Context,
		runner.DynamicRecallInput,
	) (toolDecision, error) {
		return decision, nil
	})
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
	if limit, ok := dynamicRecallLimit(runner.DynamicRecallTop4); !ok || limit != 4 {
		t.Fatalf("top-4 limit = (%d, %t)", limit, ok)
	}
	if _, ok := dynamicRecallLimit(runner.DynamicRecallStatic); ok {
		t.Fatal("static action produced a provider limit")
	}

	boundaryErr := errors.New("boundary unavailable")
	t.Run("decision source", func(t *testing.T) {
		control := newDynamicRecall(
			memoryRecallDecisionSourceFunc(func(
				context.Context,
				runner.DynamicRecallInput,
			) (toolDecision, error) {
				return toolDecision{}, boundaryErr
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
				t.Fatal("provider ran after decision failure")
				return runner.DynamicRecall{}, nil
			},
		)
		if err != nil || prepared != nil {
			t.Fatalf("decision failure = (%#v, %v)", prepared, err)
		}
	})

	t.Run("assignment persistence", func(t *testing.T) {
		control := newDynamicRecall(
			source,
			&recordingToolEvents{assignmentErr: boundaryErr},
		)
		if prepared, err := control.PrepareDynamicRecall(
			t.Context(),
			input,
			func(
				context.Context,
				string,
				string,
				int,
			) (runner.DynamicRecall, error) {
				t.Fatal("provider ran before assignment persisted")
				return runner.DynamicRecall{}, nil
			},
		); prepared != nil || !errors.Is(err, boundaryErr) {
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
				return validControlDynamicRecall(), nil
			},
		)
		if err != nil {
			t.Fatalf("PrepareDynamicRecall: %v", err)
		}
		if err := prepared.Commit(
			t.Context(),
			agent.DynamicTailOutcome{Delivered: true},
		); !errors.Is(err, boundaryErr) {
			t.Fatalf("delivery persistence error = %v", err)
		}
	})

	t.Run("provider fallback persistence", func(t *testing.T) {
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
				return runner.DynamicRecall{}, boundaryErr
			},
		)
		if prepared != nil || !errors.Is(err, boundaryErr) {
			t.Fatalf("provider fallback persistence = (%#v, %v)", prepared, err)
		}
	})

	assignment, _, err := newMemoryRecallAssignment(input, decision)
	if err != nil {
		t.Fatalf("newMemoryRecallAssignment: %v", err)
	}
	unsupported := decision
	unsupported.RecommendedAction = "unregistered"
	if _, _, err := newMemoryRecallAssignment(
		input,
		unsupported,
	); err == nil {
		t.Fatal("unregistered memory recommendation was accepted")
	}
	t.Run("invalid exposure", func(t *testing.T) {
		delivery, err := newMemoryRecallDelivery(
			assignment,
			validControlDynamicRecall(),
			agent.DynamicTailOutcome{
				FallbackReason: agent.DynamicTailFallbackInvalid,
			},
		)
		if err != nil {
			t.Fatalf("newMemoryRecallDelivery: %v", err)
		}
		if delivery.ActualActionID != adaptive.StaticActionID ||
			delivery.FallbackReason != adaptive.FallbackStateInvalid {
			t.Fatalf("invalid exposure delivery = %#v", delivery)
		}
	})

	t.Run("incoherent delivered metadata", func(t *testing.T) {
		recall := validControlDynamicRecall()
		recall.Coherent = false
		if _, err := newMemoryRecallDelivery(
			assignment,
			recall,
			agent.DynamicTailOutcome{Delivered: true},
		); err == nil {
			t.Fatal("incoherent delivered recall was accepted")
		}
	})

	t.Run("unregistered delivered revision", func(t *testing.T) {
		recall := validControlDynamicRecall()
		recall.Revisions.Index = ""
		if _, err := newMemoryRecallDelivery(
			assignment,
			recall,
			agent.DynamicTailOutcome{Delivered: true},
		); err == nil {
			t.Fatal("unregistered delivered revision was accepted")
		}
	})
}

func memoryRecallInput() runner.DynamicRecallInput {
	return runner.DynamicRecallInput{
		OwnerID: uuid.Must(uuid.NewV7()), RequestID: uuid.Must(uuid.NewV7()),
		Query: "remember my preferences",
		CandidateActions: []runner.DynamicRecallAction{
			runner.DynamicRecallStatic,
			runner.DynamicRecallTop4,
			runner.DynamicRecallTop8,
		},
		MaxItems: 8, ProviderID: "openrouter", ModelID: "production-model",
	}
}

func memoryRecallShadowDecision(
	t *testing.T,
	input runner.DynamicRecallInput,
) toolDecision {
	t.Helper()
	snapshot, err := adaptive.NewPolicySnapshot(adaptive.SnapshotSpec{
		Scope: adaptive.SnapshotScope{
			OwnerID: input.OwnerID, Domain: adaptive.DomainMemoryRecall,
			Point: adaptive.PointMemoryRecall, ProviderID: input.ProviderID,
			ModelID: input.ModelID, PolicyVersion: "policy-v1",
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
			Epoch: 1, Version: "policy-v1", Mode: adaptive.PolicyShadow,
		},
		Snapshot: snapshot, Environment: adaptive.EvaluationProductionCanary,
		RecommendedAction: string(runner.DynamicRecallTop4),
		ProviderID:        input.ProviderID, ModelID: input.ModelID,
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

func validControlDynamicRecall() runner.DynamicRecall {
	epoch := uint64(42)
	return runner.DynamicRecall{
		Text: runner.FenceDynamicRecall("exact recalled context"),
		Results: []runner.DynamicRecallResult{
			{
				Kind: "memory_preference",
				ID:   "11111111-1111-4111-8111-111111111111", Order: 0,
			},
			{
				Kind: "memory_entity",
				ID:   "22222222-2222-4222-8222-222222222222", Order: 1,
			},
		},
		Limits: map[string]runner.DynamicRecallLimit{
			"memory_preference": {RequestedK: 8, EffectiveK: 8, Count: 1},
			"memory_entity":     {RequestedK: 8, EffectiveK: 8, Count: 1},
		},
		Revisions: runner.DynamicRecallRevisions{
			Retriever: "neo4j-agent-memory-long-term-v1",
			Reranker:  "none-v1",
			Embedding: "openai/granite-embedding-v1@768",
			Index:     "entity_embedding_idx+preference_embedding_idx@768",
		},
		CorpusEpochBefore: &epoch, CorpusEpochAfter: &epoch, Coherent: true,
	}
}
