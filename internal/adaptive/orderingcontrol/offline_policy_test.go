package orderingcontrol

import (
	"context"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent/tools"
)

func TestOfflineOrderingRejectsServingPolicyBeforeSnapshotOrFacts(
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
			input := toolInput()
			decision := toolShadowDecision(t, input)
			policy := decision.Policy
			policy.Mode = test.mode
			policy.RolloutBPS = test.rolloutBPS
			policy.Config = runtimePolicyConfigJSON(t, decision)
			loader := &countingSnapshotLoader{
				snapshot: decision.Snapshot,
			}
			recorder := &recordingToolEvents{}
			control := NewOfflineToolDiscovery(
				&countingPolicyReader{policy: policy},
				loader,
				recorder,
				decision.ProviderID,
				decision.ModelID,
			)
			got, err := control.OrderToolDiscovery(
				t.Context(),
				input,
				func(
					context.Context,
					string,
				) ([]string, error) {
					return []string{"static-result"}, nil
				},
			)
			if err != nil ||
				!slices.Equal(got, []string{"static-result"}) {
				t.Fatalf("static fallback = (%v, %v)", got, err)
			}
			if loader.reads != 0 ||
				len(recorder.assignments) != 0 ||
				len(recorder.deliveries) != 0 {
				t.Fatalf(
					"offline policy crossed boundary: snapshot=%d facts=%d/%d",
					loader.reads,
					len(recorder.assignments),
					len(recorder.deliveries),
				)
			}
		})
	}
}

func TestOfflineOrderingDisabledPolicyStaysStaticWithoutFacts(t *testing.T) {
	for _, mode := range []adaptive.PolicyMode{
		adaptive.PolicyOff,
		adaptive.PolicyRollback,
	} {
		t.Run(string(mode), func(t *testing.T) {
			input := toolInput()
			decision := toolShadowDecision(t, input)
			policy := decision.Policy
			policy.Mode = mode
			loader := &countingSnapshotLoader{
				snapshot: decision.Snapshot,
			}
			recorder := &recordingToolEvents{}
			control := NewOfflineToolDiscovery(
				&countingPolicyReader{policy: policy},
				loader,
				recorder,
				decision.ProviderID,
				decision.ModelID,
			)
			if _, err := control.OrderToolDiscovery(
				t.Context(),
				input,
				func(
					context.Context,
					string,
				) ([]string, error) {
					return []string{"static-result"}, nil
				},
			); err != nil {
				t.Fatal(err)
			}
			if loader.reads != 0 ||
				len(recorder.assignments) != 0 ||
				len(recorder.deliveries) != 0 {
				t.Fatalf(
					"disabled policy crossed boundary: snapshot=%d facts=%d/%d",
					loader.reads,
					len(recorder.assignments),
					len(recorder.deliveries),
				)
			}
		})
	}
}

func TestProductionOrderingStillAcceptsCanaryRollout(t *testing.T) {
	input := toolInput()
	decision := toolShadowDecision(t, input)
	policy := decision.Policy
	policy.Mode = adaptive.PolicyCanary
	policy.RolloutBPS = 500
	policy.Config = runtimePolicyConfigJSON(t, decision)
	loader := &countingSnapshotLoader{snapshot: decision.Snapshot}
	source := newRuntimeSource(
		&countingPolicyReader{policy: policy},
		loader,
		decision.ProviderID,
		decision.ModelID,
	)
	got, err := source.decideToolDiscovery(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if loader.reads != 1 ||
		got.Environment != adaptive.EvaluationProductionCanary ||
		got.Policy.Mode != adaptive.PolicyCanary ||
		got.Policy.RolloutBPS != 500 {
		t.Fatalf("production decision = (%+v, reads=%d)", got, loader.reads)
	}
}

var _ tools.ToolDiscoveryControl = NewOfflineToolDiscovery(
	&countingPolicyReader{},
	&countingSnapshotLoader{},
	&recordingToolEvents{},
	"provider",
	"model",
)
