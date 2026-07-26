package reasoningcontrol

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
)

func TestControlPersistsAssignmentBeforeBuildAndDeliveryBeforeExposure(t *testing.T) {
	input := testInput()
	snapshot := testSnapshot(t, input, "policy-v1")
	var order []string
	source := decisionSourceFunc(func(
		context.Context,
		agent.ReasoningControlInput,
	) (Decision, error) {
		order = append(order, "select")
		return shadowDecision(snapshot), nil
	})
	recorder := &eventRecorder{order: &order}
	control := newControl(source, recorder)

	prepared, err := control.DecideAndDeliverReasoning(
		t.Context(),
		input,
		func(
			context.Context,
			agent.ReasoningAction,
		) (agent.PreparedReasoningRequest, error) {
			order = append(order, "build")
			return agent.PreparedReasoningRequest{
				Request: llm.Request{MaxTokens: 256},
			}, nil
		},
		staticFailure(t),
	)
	order = append(order, "expose")

	if err != nil {
		t.Fatalf("DecideAndDeliverReasoning: %v", err)
	}
	if prepared.Request.MaxTokens != 256 {
		t.Fatalf("prepared max_tokens = %d, want 256", prepared.Request.MaxTokens)
	}
	want := []string{"select", "assignment", "build", "delivery", "expose"}
	if !slices.Equal(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	assignments, deliveries := recorder.snapshot()
	if len(assignments) != 1 || len(deliveries) != 1 {
		t.Fatalf("facts = %d assignments/%d deliveries", len(assignments), len(deliveries))
	}
	assignment := assignments[0]
	if assignment.ChampionActionID != adaptive.StaticActionID ||
		assignment.IntendedActionID != string(agent.ReasoningActionStatic) ||
		assignment.RecommendedActionID != string(agent.ReasoningActionHigh) {
		t.Fatalf("shadow actions = %+v", assignment)
	}
	if assignment.ActionProbabilities[3].Probability != 1 ||
		deliveries[0].ActualActionID != string(agent.ReasoningActionStatic) {
		t.Fatalf("shadow exposure = %+v / %+v", assignment, deliveries[0])
	}
	if len(assignment.Features) != 1 ||
		assignment.Features[adaptive.FeatureMessageCount] !=
			float64(input.MessageCount) {
		t.Fatalf("reasoning features = %+v", assignment.Features)
	}
}

func TestControlUsesDistinctRoundIDsAndStableExactRetryID(t *testing.T) {
	input := testInput()
	snapshot := testSnapshot(t, input, "policy-v1")
	recorder := &eventRecorder{}
	control := newControl(
		decisionSourceFunc(func(
			context.Context,
			agent.ReasoningControlInput,
		) (Decision, error) {
			return shadowDecision(snapshot), nil
		}),
		recorder,
	)
	build := func(
		context.Context,
		agent.ReasoningAction,
	) (agent.PreparedReasoningRequest, error) {
		return agent.PreparedReasoningRequest{
			Request: llm.Request{MaxTokens: 128},
		}, nil
	}

	for range 2 {
		if _, err := control.DecideAndDeliverReasoning(
			t.Context(), input, build, staticFailure(t),
		); err != nil {
			t.Fatalf("exact retry: %v", err)
		}
	}
	next := input
	next.PointOrdinal++
	if _, err := control.DecideAndDeliverReasoning(
		t.Context(), next, build, staticFailure(t),
	); err != nil {
		t.Fatalf("next round: %v", err)
	}

	assignments, deliveries := recorder.snapshot()
	if assignments[0].AssignmentID != assignments[1].AssignmentID ||
		deliveries[0].AssignmentID != deliveries[1].AssignmentID {
		t.Fatal("exact retry changed assignment or delivery identity")
	}
	if assignments[2].AssignmentID == assignments[0].AssignmentID {
		t.Fatal("next model round reused the prior assignment ID")
	}
}

func TestControlRecordsExplicitOverrideWithoutRandomizedArm(t *testing.T) {
	input := testInput()
	input.Override = llm.ReasoningEffortLow
	snapshot := testSnapshot(t, input, "policy-v1")
	recorder := &eventRecorder{}
	control := newControl(
		decisionSourceFunc(func(
			context.Context,
			agent.ReasoningControlInput,
		) (Decision, error) {
			return shadowDecision(snapshot), nil
		}),
		recorder,
	)
	var action agent.ReasoningAction

	if _, err := control.DecideAndDeliverReasoning(
		t.Context(),
		input,
		func(
			_ context.Context,
			selected agent.ReasoningAction,
		) (agent.PreparedReasoningRequest, error) {
			action = selected
			return agent.PreparedReasoningRequest{
				Request: llm.Request{MaxTokens: 64},
			}, nil
		},
		staticFailure(t),
	); err != nil {
		t.Fatalf("override: %v", err)
	}

	assignments, _ := recorder.snapshot()
	assignment := assignments[0]
	if action != agent.ReasoningActionLow ||
		!assignment.Override ||
		assignment.SelectionReason != adaptive.SelectionOperatorOverride ||
		assignment.ExperimentID != "" ||
		assignment.ArmID != "" ||
		assignment.ArmProbability != nil {
		t.Fatalf("override claimed adaptive arm: action=%q assignment=%+v", action, assignment)
	}
}

func TestControlKeepsCanaryAndActiveStatic(t *testing.T) {
	for _, mode := range []adaptive.PolicyMode{
		adaptive.PolicyCanary, adaptive.PolicyActive,
	} {
		t.Run(string(mode), func(t *testing.T) {
			input := testInput()
			snapshot := testSnapshot(t, input, "policy-v1")
			recorder := &eventRecorder{}
			control := newControl(
				decisionSourceFunc(func(
					context.Context,
					agent.ReasoningControlInput,
				) (Decision, error) {
					decision := shadowDecision(snapshot)
					decision.Policy.Mode = mode
					return decision, nil
				}),
				recorder,
			)
			var action agent.ReasoningAction
			if _, err := control.DecideAndDeliverReasoning(
				t.Context(),
				input,
				func(
					_ context.Context,
					selected agent.ReasoningAction,
				) (agent.PreparedReasoningRequest, error) {
					action = selected
					return agent.PreparedReasoningRequest{
						Request: llm.Request{MaxTokens: 64},
					}, nil
				},
				staticFailure(t),
			); err != nil {
				t.Fatalf("mode %s: %v", mode, err)
			}
			assignments, _ := recorder.snapshot()
			if action != agent.ReasoningActionStatic ||
				assignments[0].IntendedActionID != adaptive.StaticActionID {
				t.Fatalf("mode %s selected %q: %+v", mode, action, assignments[0])
			}
		})
	}
}

func TestControlFallsBackAtAllTypedBoundaries(t *testing.T) {
	boundaryErr := errors.New("boundary failed")
	tests := []struct {
		name          string
		sourceErr     error
		assignmentErr error
		buildErr      error
		deliveryErr   error
	}{
		{name: "selection", sourceErr: boundaryErr},
		{name: "assignment", assignmentErr: boundaryErr},
		{name: "build", buildErr: boundaryErr},
		{name: "delivery", deliveryErr: boundaryErr},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := testInput()
			snapshot := testSnapshot(t, input, "policy-v1")
			recorder := &eventRecorder{
				assignmentErr: test.assignmentErr, deliveryErr: test.deliveryErr,
			}
			control := newControl(
				decisionSourceFunc(func(
					context.Context,
					agent.ReasoningControlInput,
				) (Decision, error) {
					if test.sourceErr != nil {
						return Decision{}, test.sourceErr
					}
					return shadowDecision(snapshot), nil
				}),
				recorder,
			)
			staticCalls := 0
			value, err := control.DecideAndDeliverReasoning(
				t.Context(),
				input,
				func(
					context.Context,
					agent.ReasoningAction,
				) (agent.PreparedReasoningRequest, error) {
					return agent.PreparedReasoningRequest{}, test.buildErr
				},
				func(context.Context) (agent.PreparedReasoningRequest, error) {
					staticCalls++
					return agent.PreparedReasoningRequest{
						Request: llm.Request{Model: "static"},
					}, nil
				},
			)
			if err != nil || value.Request.Model != "static" || staticCalls != 1 {
				t.Fatalf(
					"fallback = (%+v, %v), calls=%d",
					value, err, staticCalls,
				)
			}
			_, deliveries := recorder.snapshot()
			if len(deliveries) != 0 {
				t.Fatalf("failed boundary persisted delivery %+v", deliveries)
			}
		})
	}
}

func TestNewAndDisabledControlUseStaticRequest(t *testing.T) {
	source := decisionSourceFunc(func(
		context.Context,
		agent.ReasoningControlInput,
	) (Decision, error) {
		t.Fatal("disabled control consulted its source")
		return Decision{}, nil
	})
	recorder := &eventRecorder{}
	if newControl(nil, recorder) != nil {
		t.Fatal("newControl(nil, recorder) returned a control")
	}
	if newControl(source, nil) != nil {
		t.Fatal("newControl(source, nil) returned a control")
	}

	controls := []*Control{nil, {recorder: recorder}, {source: source}}
	for index, control := range controls {
		t.Run(fmt.Sprintf("disabled-%d", index), func(t *testing.T) {
			staticCalls := 0
			prepared, err := control.DecideAndDeliverReasoning(
				t.Context(),
				testInput(),
				func(
					context.Context,
					agent.ReasoningAction,
				) (agent.PreparedReasoningRequest, error) {
					t.Fatal("disabled control built a learned request")
					return agent.PreparedReasoningRequest{}, nil
				},
				func(context.Context) (agent.PreparedReasoningRequest, error) {
					staticCalls++
					return agent.PreparedReasoningRequest{
						Request: llm.Request{Model: "static"},
					}, nil
				},
			)
			if err != nil || prepared.Request.Model != "static" ||
				staticCalls != 1 {
				t.Fatalf(
					"static = (%+v, %v), calls=%d",
					prepared, err, staticCalls,
				)
			}
		})
	}
}

func TestControlSkipsAssignmentForOffAndRollback(t *testing.T) {
	for _, mode := range []adaptive.PolicyMode{
		adaptive.PolicyOff, adaptive.PolicyRollback,
	} {
		t.Run(string(mode), func(t *testing.T) {
			input := testInput()
			recorder := &eventRecorder{}
			control := newControl(
				decisionSourceFunc(func(
					context.Context,
					agent.ReasoningControlInput,
				) (Decision, error) {
					decision := shadowDecision(testSnapshot(t, input, "policy-v1"))
					decision.Policy.Mode = mode
					return decision, nil
				}),
				recorder,
			)
			if _, err := control.DecideAndDeliverReasoning(
				t.Context(),
				input,
				func(
					context.Context,
					agent.ReasoningAction,
				) (agent.PreparedReasoningRequest, error) {
					t.Fatal("disabled mode built a learned request")
					return agent.PreparedReasoningRequest{}, nil
				},
				func(context.Context) (agent.PreparedReasoningRequest, error) {
					return agent.PreparedReasoningRequest{}, nil
				},
			); err != nil {
				t.Fatalf("mode %s: %v", mode, err)
			}
			assignments, deliveries := recorder.snapshot()
			if len(assignments) != 0 || len(deliveries) != 0 {
				t.Fatalf(
					"mode %s emitted %d assignments/%d deliveries",
					mode, len(assignments), len(deliveries),
				)
			}
		})
	}
}

func TestControlFallsBackForInvalidTypedDecision(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*agent.ReasoningControlInput, *Decision)
	}{
		{
			name: "incomplete",
			mutate: func(_ *agent.ReasoningControlInput, decision *Decision) {
				*decision = Decision{}
			},
		},
		{
			name: "scope mismatch",
			mutate: func(input *agent.ReasoningControlInput, decision *Decision) {
				other := *input
				other.ModelID = "other-model"
				decision.Snapshot = testSnapshot(t, other, "policy-v1")
			},
		},
		{
			name: "catalog mismatch",
			mutate: func(input *agent.ReasoningControlInput, decision *Decision) {
				actions := append(
					slices.Clone(actionCatalog[:len(actionCatalog)-1]),
					"reasoning_x",
				)
				actions = append(actions, actionCatalog[len(actionCatalog)-1])
				decision.Snapshot = testSnapshotWithActions(
					t,
					*input,
					"policy-v1",
					actions,
				)
			},
		},
		{
			name: "unsupported recommendation",
			mutate: func(_ *agent.ReasoningControlInput, decision *Decision) {
				decision.RecommendedTier = prompt.ReasoningTier("turbo")
			},
		},
		{
			name: "unsupported provider",
			mutate: func(input *agent.ReasoningControlInput, decision *Decision) {
				input.ProviderID = "anthropic"
				decision.Snapshot = testSnapshot(t, *input, "policy-v1")
			},
		},
		{
			name: "unsupported override",
			mutate: func(input *agent.ReasoningControlInput, _ *Decision) {
				input.Override = llm.ReasoningEffortMedium
			},
		},
		{
			name: "invalid policy mode",
			mutate: func(_ *agent.ReasoningControlInput, decision *Decision) {
				decision.Policy.Mode = adaptive.PolicyMode("invalid")
			},
		},
		{
			name: "invalid environment",
			mutate: func(_ *agent.ReasoningControlInput, decision *Decision) {
				decision.Environment = adaptive.EvaluationEnvironment("invalid")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := testInput()
			decision := shadowDecision(testSnapshot(t, input, "policy-v1"))
			test.mutate(&input, &decision)
			recorder := &eventRecorder{}
			control := newControl(
				decisionSourceFunc(func(
					context.Context,
					agent.ReasoningControlInput,
				) (Decision, error) {
					return decision, nil
				}),
				recorder,
			)
			staticCalls := 0
			prepared, err := control.DecideAndDeliverReasoning(
				t.Context(),
				input,
				func(
					context.Context,
					agent.ReasoningAction,
				) (agent.PreparedReasoningRequest, error) {
					t.Fatal("invalid decision built a learned request")
					return agent.PreparedReasoningRequest{}, nil
				},
				func(context.Context) (agent.PreparedReasoningRequest, error) {
					staticCalls++
					return agent.PreparedReasoningRequest{
						Request: llm.Request{Model: "static"},
					}, nil
				},
			)
			if err != nil || prepared.Request.Model != "static" ||
				staticCalls != 1 {
				t.Fatalf(
					"fallback = (%+v, %v), calls=%d",
					prepared, err, staticCalls,
				)
			}
			assignments, deliveries := recorder.snapshot()
			if len(assignments) != 0 || len(deliveries) != 0 {
				t.Fatalf(
					"invalid decision emitted %d assignments/%d deliveries",
					len(assignments), len(deliveries),
				)
			}
		})
	}
}

func TestControlReturnsInterceptWithoutDelivery(t *testing.T) {
	input := testInput()
	recorder := &eventRecorder{}
	control := newControl(
		decisionSourceFunc(func(
			context.Context,
			agent.ReasoningControlInput,
		) (Decision, error) {
			return shadowDecision(testSnapshot(t, input, "policy-v1")), nil
		}),
		recorder,
	)
	hookResult := &agent.ModelHookResult{}
	prepared, err := control.DecideAndDeliverReasoning(
		t.Context(),
		input,
		func(
			context.Context,
			agent.ReasoningAction,
		) (agent.PreparedReasoningRequest, error) {
			return agent.PreparedReasoningRequest{HookResult: hookResult}, nil
		},
		staticFailure(t),
	)
	if err != nil || prepared.HookResult != hookResult {
		t.Fatalf("intercept = (%+v, %v)", prepared, err)
	}
	assignments, deliveries := recorder.snapshot()
	if len(assignments) != 1 || len(deliveries) != 0 {
		t.Fatalf(
			"intercept emitted %d assignments/%d deliveries",
			len(assignments), len(deliveries),
		)
	}
}

func TestActionForTierUsesFrozenCatalog(t *testing.T) {
	tests := []struct {
		tier prompt.ReasoningTier
		want agent.ReasoningAction
		ok   bool
	}{
		{tier: prompt.ReasoningTierHigh, want: agent.ReasoningActionHigh, ok: true},
		{tier: prompt.ReasoningTierLow, want: agent.ReasoningActionLow, ok: true},
		{tier: prompt.ReasoningTierNone, want: agent.ReasoningActionNone, ok: true},
		{tier: prompt.ReasoningTier("turbo")},
	}
	for _, test := range tests {
		got, ok := actionForTier(test.tier)
		if got != string(test.want) || ok != test.ok {
			t.Fatalf(
				"actionForTier(%q) = (%q, %t), want (%q, %t)",
				test.tier, got, ok, test.want, test.ok,
			)
		}
	}
}
