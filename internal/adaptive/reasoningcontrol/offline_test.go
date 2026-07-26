package reasoningcontrol

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

type countingPolicyReader struct {
	policy adaptive.Policy
	reads  int
}

func (reader *countingPolicyReader) CurrentPolicy(
	context.Context,
) (adaptive.Policy, error) {
	reader.reads++
	return reader.policy, nil
}

type countingSnapshotLoader struct {
	snapshot *adaptive.PolicySnapshot
	reads    int
}

func (loader *countingSnapshotLoader) Load(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (*adaptive.PolicySnapshot, error) {
	loader.reads++
	return loader.snapshot, nil
}

func TestOfflineReasoningConstructorIsReadOnlyAndComplete(t *testing.T) {
	reader := &countingPolicyReader{}
	loader := &countingSnapshotLoader{}
	recorder := &eventRecorder{}
	if NewOffline(nil, loader, recorder, "openrouter", "model") != nil ||
		NewOffline(reader, nil, recorder, "openrouter", "model") != nil ||
		NewOffline(reader, loader, nil, "openrouter", "model") != nil ||
		NewOffline(reader, loader, recorder, "", "model") != nil ||
		NewOffline(reader, loader, recorder, "openrouter", "") != nil {
		t.Fatal("incomplete offline binding produced a control")
	}
	if NewOffline(
		reader,
		loader,
		recorder,
		"openrouter",
		"model",
	) == nil {
		t.Fatal("complete offline binding did not produce a control")
	}
	if reader.reads != 0 || loader.reads != 0 ||
		len(recorder.assignments) != 0 || len(recorder.deliveries) != 0 {
		t.Fatalf(
			"construction performed I/O: policy=%d snapshot=%d facts=%d/%d",
			reader.reads,
			loader.reads,
			len(recorder.assignments),
			len(recorder.deliveries),
		)
	}
}

func TestOfflineReasoningEmitsOfflineProductionEquivalentStaticContract(
	t *testing.T,
) {
	input := testInput()
	input.BaseURL = "https://openrouter.ai/api/v1"
	snapshot := testSnapshot(t, input, "policy-v1")
	policy := runtimePolicy(t, snapshot, adaptive.PolicyShadow)
	reader := &countingPolicyReader{policy: policy}
	loader := &countingSnapshotLoader{snapshot: snapshot}
	recorder := &eventRecorder{}
	control := NewOffline(
		reader,
		loader,
		recorder,
		input.ProviderID,
		input.ModelID,
	)
	if control == nil {
		t.Fatal("offline reasoning control is nil")
	}
	if _, err := control.DecideAndDeliverReasoning(
		t.Context(),
		input,
		func(
			context.Context,
			agent.ReasoningAction,
		) (agent.PreparedReasoningRequest, error) {
			return agent.PreparedReasoningRequest{
				Request: llm.Request{MaxTokens: 128},
			}, nil
		},
		staticFailure(t),
	); err != nil {
		t.Fatal(err)
	}
	assignments, deliveries := recorder.snapshot()
	if len(assignments) != 1 || len(deliveries) != 1 {
		t.Fatalf(
			"facts = %d assignments/%d deliveries",
			len(assignments),
			len(deliveries),
		)
	}
	offline := assignments[0]
	if offline.Environment != adaptive.EvaluationOffline {
		t.Fatalf("environment = %q", offline.Environment)
	}
	productionDecision, err := newRuntimeSource(
		reader,
		loader,
		input.ProviderID,
		input.ModelID,
	).DecideReasoning(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if productionDecision.Environment !=
		adaptive.EvaluationProductionCanary {
		t.Fatalf(
			"production environment = %q",
			productionDecision.Environment,
		)
	}
	production, err := newAssignment(input, productionDecision)
	if err != nil {
		t.Fatal(err)
	}
	assertReasoningStaticContractBytes(t, offline, production)
}

func TestOfflineReasoningInvalidEnvironmentFailsClosed(t *testing.T) {
	input := testInput()
	input.BaseURL = "https://openrouter.ai/api/v1"
	snapshot := testSnapshot(t, input, "policy-v1")
	policy := runtimePolicy(t, snapshot, adaptive.PolicyShadow)
	source := newRuntimeSourceForEnvironment(
		&countingPolicyReader{policy: policy},
		&countingSnapshotLoader{snapshot: snapshot},
		input.ProviderID,
		input.ModelID,
		adaptive.EvaluationEnvironment("invalid"),
	)
	if _, err := source.DecideReasoning(
		t.Context(),
		input,
	); err == nil {
		t.Fatal("invalid evaluation environment was accepted")
	}
}

func assertReasoningStaticContractBytes(
	t *testing.T,
	offline adaptive.Assignment,
	production adaptive.Assignment,
) {
	t.Helper()
	type staticContract struct {
		Champion      string
		Recommended   string
		Intended      string
		Eligible      []string
		Eligibility   string
		Catalog       string
		Probabilities []adaptive.ActionProbability
	}
	project := func(assignment adaptive.Assignment) staticContract {
		return staticContract{
			Champion:      assignment.ChampionActionID,
			Recommended:   assignment.RecommendedActionID,
			Intended:      assignment.IntendedActionID,
			Eligible:      assignment.EligibleActions,
			Eligibility:   assignment.EligibilitySHA256,
			Catalog:       assignment.CatalogSHA256,
			Probabilities: assignment.ActionProbabilities,
		}
	}
	offlineJSON, err := json.Marshal(project(offline))
	if err != nil {
		t.Fatal(err)
	}
	productionJSON, err := json.Marshal(project(production))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(offlineJSON, productionJSON) {
		t.Fatalf(
			"static contract drift:\noffline=%s\nproduction=%s",
			offlineJSON,
			productionJSON,
		)
	}
}

var _ SnapshotLoader = (*countingSnapshotLoader)(nil)
