package reasoningcontrol

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
)

func TestOfflineReasoningRejectsServingPolicyBeforeSnapshotOrFacts(
	t *testing.T,
) {
	tests := []struct {
		name       string
		mode       adaptive.PolicyMode
		rolloutBPS int32
	}{
		{name: "canary", mode: adaptive.PolicyCanary},
		{name: "active", mode: adaptive.PolicyActive},
		{name: "nonzero rollout", mode: adaptive.PolicyShadow, rolloutBPS: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, snapshot := offlineReasoningInput(t)
			policy := runtimePolicy(t, snapshot, test.mode)
			policy.RolloutBPS = test.rolloutBPS
			loader := &countingSnapshotLoader{snapshot: snapshot}
			recorder := &eventRecorder{}
			control := NewOffline(
				&countingPolicyReader{policy: policy},
				loader,
				recorder,
				input.ProviderID,
				input.ModelID,
			)
			staticCalls := 0
			_, err := control.DecideAndDeliverReasoning(
				t.Context(),
				input,
				func(
					context.Context,
					agent.ReasoningAction,
				) (agent.PreparedReasoningRequest, error) {
					t.Fatal("rejected policy reached learned builder")
					return agent.PreparedReasoningRequest{}, nil
				},
				func(
					context.Context,
				) (agent.PreparedReasoningRequest, error) {
					staticCalls++
					return agent.PreparedReasoningRequest{}, nil
				},
			)
			assignments, deliveries := recorder.snapshot()
			if err != nil || staticCalls != 1 || loader.reads != 0 ||
				len(assignments) != 0 || len(deliveries) != 0 {
				t.Fatalf(
					"boundary = err=%v static=%d snapshot=%d facts=%d/%d",
					err,
					staticCalls,
					loader.reads,
					len(assignments),
					len(deliveries),
				)
			}
		})
	}
}

func TestOfflineReasoningRejectsOverrideBeforeLearnedBuilderOrFacts(
	t *testing.T,
) {
	input, snapshot := offlineReasoningInput(t)
	input.Override = llm.ReasoningEffortLow
	policy := runtimePolicy(t, snapshot, adaptive.PolicyShadow)
	loader := &countingSnapshotLoader{snapshot: snapshot}
	recorder := &eventRecorder{}
	control := NewOffline(
		&countingPolicyReader{policy: policy},
		loader,
		recorder,
		input.ProviderID,
		input.ModelID,
	)
	staticCalls := 0
	_, err := control.DecideAndDeliverReasoning(
		t.Context(),
		input,
		func(
			context.Context,
			agent.ReasoningAction,
		) (agent.PreparedReasoningRequest, error) {
			t.Fatal("offline override reached learned builder")
			return agent.PreparedReasoningRequest{}, nil
		},
		func(
			context.Context,
		) (agent.PreparedReasoningRequest, error) {
			staticCalls++
			return agent.PreparedReasoningRequest{}, nil
		},
	)
	assignments, deliveries := recorder.snapshot()
	if err != nil || staticCalls != 1 || loader.reads != 0 ||
		len(assignments) != 0 || len(deliveries) != 0 {
		t.Fatalf(
			"override boundary = err=%v static=%d snapshot=%d facts=%d/%d",
			err,
			staticCalls,
			loader.reads,
			len(assignments),
			len(deliveries),
		)
	}
}

func TestProductionReasoningKeepsCanaryAndOverrideBehavior(t *testing.T) {
	input, snapshot := offlineReasoningInput(t)
	input.Override = llm.ReasoningEffortLow
	policy := runtimePolicy(t, snapshot, adaptive.PolicyCanary)
	policy.RolloutBPS = 500
	loader := &countingSnapshotLoader{snapshot: snapshot}
	recorder := &eventRecorder{}
	control := newControl(
		newRuntimeSource(
			&countingPolicyReader{policy: policy},
			loader,
			input.ProviderID,
			input.ModelID,
		),
		recorder,
	)
	if _, err := control.DecideAndDeliverReasoning(
		t.Context(),
		input,
		func(
			_ context.Context,
			action agent.ReasoningAction,
		) (agent.PreparedReasoningRequest, error) {
			if action != agent.ReasoningActionLow {
				t.Fatalf("production override action = %q", action)
			}
			return agent.PreparedReasoningRequest{
				Request: llm.Request{MaxTokens: 128},
			}, nil
		},
		staticFailure(t),
	); err != nil {
		t.Fatal(err)
	}
	assignments, deliveries := recorder.snapshot()
	if loader.reads != 1 ||
		len(assignments) != 1 || len(deliveries) != 1 ||
		!assignments[0].Override ||
		assignments[0].Environment != adaptive.EvaluationProductionCanary {
		t.Fatalf(
			"production boundary = snapshot=%d assignments=%+v deliveries=%d",
			loader.reads,
			assignments,
			len(deliveries),
		)
	}
}

func offlineReasoningInput(
	t *testing.T,
) (agent.ReasoningControlInput, *adaptive.PolicySnapshot) {
	t.Helper()
	input := testInput()
	input.BaseURL = "https://openrouter.ai/api/v1"
	return input, testSnapshot(t, input, "policy-v1")
}
