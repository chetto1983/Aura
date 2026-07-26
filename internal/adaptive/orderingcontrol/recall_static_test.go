package orderingcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/runner"
)

func TestDynamicRecallShadowRecordsStaticFactsWithoutCallingProvider(
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

	prepared, err := control.PrepareDynamicRecall(
		t.Context(),
		input,
		func(
			context.Context,
			string,
			string,
			int,
		) (runner.DynamicRecall, error) {
			t.Fatal("shadow called the recall provider")
			return runner.DynamicRecall{}, nil
		},
	)
	if err != nil {
		t.Fatalf("PrepareDynamicRecall: %v", err)
	}
	if prepared == nil || prepared.Action != runner.DynamicRecallStatic {
		t.Fatalf("prepared = %#v, want static", prepared)
	}
	if !slices.Equal(order, []string{"assignment"}) {
		t.Fatalf("pre-commit order = %v", order)
	}

	var assignment adaptive.Assignment
	for _, assignment = range recorder.assignments {
	}
	wantProbabilities := []adaptive.ActionProbability{
		{ActionID: adaptive.StaticActionID, Probability: 1},
		{ActionID: string(runner.DynamicRecallTop4), Probability: 0},
		{ActionID: string(runner.DynamicRecallTop8), Probability: 0},
	}
	if assignment.IntendedActionID != adaptive.StaticActionID ||
		assignment.RecommendedActionID != string(runner.DynamicRecallTop4) ||
		assignment.SelectionReason != adaptive.SelectionShadowStatic ||
		assignment.Override || assignment.ExperimentID != "" ||
		assignment.ArmID != "" || assignment.ArmProbability != nil ||
		!slices.Equal(assignment.ActionProbabilities, wantProbabilities) {
		t.Fatalf("shadow assignment = %#v", assignment)
	}

	if err := prepared.Commit(
		t.Context(),
		agent.DynamicTailOutcome{},
	); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := prepared.Commit(
		t.Context(),
		agent.DynamicTailOutcome{},
	); err != nil {
		t.Fatalf("duplicate Commit: %v", err)
	}
	if !slices.Equal(order, []string{"assignment", "delivery"}) {
		t.Fatalf("final order = %v", order)
	}
	delivery := recorder.deliveries[assignment.AssignmentID]
	revisionsJSON, err := json.Marshal(delivery.Revisions)
	if err != nil {
		t.Fatalf("marshal static revisions: %v", err)
	}
	if delivery.Status != adaptive.DeliverySuccess ||
		delivery.IntendedActionID != adaptive.StaticActionID ||
		delivery.ActualActionID != adaptive.StaticActionID ||
		!delivery.ExposureKnown || delivery.ExposureProbability == nil ||
		*delivery.ExposureProbability != 1 ||
		delivery.ResultCount != 0 || len(delivery.ResultIDs) != 0 ||
		string(revisionsJSON) != "{}" ||
		len(delivery.EffectiveLimits) != 1 ||
		delivery.EffectiveLimits["top_k"] != 0 {
		t.Fatalf("static delivery = %#v", delivery)
	}
}

func TestDynamicRecallExogenousStaticSkipsPolicyProviderAndFacts(
	t *testing.T,
) {
	t.Parallel()
	tests := []struct {
		name       string
		maxItems   int
		candidates []runner.DynamicRecallAction
	}{
		{
			name:       "disabled",
			maxItems:   8,
			candidates: runner.DynamicRecallCatalog(false, 8),
		},
		{
			name:       "below four",
			maxItems:   3,
			candidates: runner.DynamicRecallCatalog(true, 3),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := memoryRecallInput()
			input.MaxItems = test.maxItems
			input.CandidateActions = test.candidates
			recorder := &recordingToolEvents{}
			control := newDynamicRecall(
				memoryRecallDecisionSourceFunc(func(
					context.Context,
					runner.DynamicRecallInput,
				) (toolDecision, error) {
					t.Fatal("exogenous static read adaptive policy")
					return toolDecision{}, nil
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
					t.Fatal("exogenous static called the recall provider")
					return runner.DynamicRecall{}, nil
				},
			)
			if err != nil || prepared != nil ||
				len(recorder.assignments) != 0 ||
				len(recorder.deliveries) != 0 {
				t.Fatalf(
					"exogenous result = (%#v, %v), facts=%d/%d",
					prepared,
					err,
					len(recorder.assignments),
					len(recorder.deliveries),
				)
			}
		})
	}
}

func TestDynamicRecallUnavailablePolicySkipsProviderAndFacts(t *testing.T) {
	t.Parallel()
	policyReadErr := errors.New("policy unavailable")
	tests := []struct {
		name      string
		mode      adaptive.PolicyMode
		sourceErr error
	}{
		{name: "off", mode: adaptive.PolicyOff},
		{name: "rollback", mode: adaptive.PolicyRollback},
		{name: "read failure", sourceErr: policyReadErr},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := memoryRecallInput()
			decision := memoryRecallShadowDecision(t, input)
			decision.Policy.Mode = test.mode
			recorder := &recordingToolEvents{}
			control := newDynamicRecall(
				memoryRecallDecisionSourceFunc(func(
					context.Context,
					runner.DynamicRecallInput,
				) (toolDecision, error) {
					return decision, test.sourceErr
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
					t.Fatal("unavailable policy called the recall provider")
					return runner.DynamicRecall{}, nil
				},
			)
			if err != nil || prepared != nil ||
				len(recorder.assignments) != 0 ||
				len(recorder.deliveries) != 0 {
				t.Fatalf(
					"unavailable result = (%#v, %v), facts=%d/%d",
					prepared,
					err,
					len(recorder.assignments),
					len(recorder.deliveries),
				)
			}
		})
	}
}

func TestDynamicRecallUnauthorizedServingModesStayStatic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode   adaptive.PolicyMode
		reason adaptive.SelectionReason
	}{
		{
			mode:   adaptive.PolicyCanary,
			reason: adaptive.SelectionCanaryDiagnostic,
		},
		{
			mode:   adaptive.PolicyActive,
			reason: adaptive.SelectionUnsupported,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(string(test.mode), func(t *testing.T) {
			t.Parallel()
			input := memoryRecallInput()
			decision := memoryRecallShadowDecision(t, input)
			decision.Policy.Mode = test.mode
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
					t.Fatalf("%s called the recall provider", test.mode)
					return runner.DynamicRecall{}, nil
				},
			)
			if err != nil || prepared == nil ||
				prepared.Action != runner.DynamicRecallStatic {
				t.Fatalf("prepared = (%#v, %v)", prepared, err)
			}
			var assignment adaptive.Assignment
			for _, assignment = range recorder.assignments {
			}
			if assignment.IntendedActionID != adaptive.StaticActionID ||
				assignment.RecommendedActionID !=
					string(runner.DynamicRecallTop4) ||
				assignment.SelectionReason != test.reason ||
				assignment.Override ||
				assignment.ActionProbabilities[0].Probability != 1 {
				t.Fatalf("%s assignment = %#v", test.mode, assignment)
			}
			if err := prepared.Commit(
				t.Context(),
				agent.DynamicTailOutcome{},
			); err != nil {
				t.Fatalf("Commit: %v", err)
			}
			delivery := recorder.deliveries[assignment.AssignmentID]
			if delivery.Status != adaptive.DeliverySuccess ||
				delivery.ActualActionID != adaptive.StaticActionID ||
				delivery.ExposureProbability == nil ||
				*delivery.ExposureProbability != 1 {
				t.Fatalf("%s delivery = %#v", test.mode, delivery)
			}
		})
	}
}
