package orderingcontrol

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/runner"
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

func TestOfflineOrderingConstructorsAreReadOnlyAtConstruction(t *testing.T) {
	reader := &countingPolicyReader{}
	loader := &countingSnapshotLoader{}
	recorder := &recordingToolEvents{}

	controls := []any{
		NewOfflineSkillRouting(
			reader, loader, recorder, "openrouter", "production-model",
		),
		NewOfflineSkillRouting(
			reader, loader, recorder, "openrouter", "production-model",
		),
		NewOfflineDynamicRecall(
			reader, loader, recorder, "openrouter", "production-model",
		),
	}
	for index, control := range controls {
		if control == nil || reflect.ValueOf(control).IsNil() {
			t.Fatalf("offline control %d is nil", index)
		}
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

func TestOfflineOrderingConstructorsEmitOfflineStaticContracts(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T) (adaptive.Assignment, adaptive.Assignment)
	}{
		{name: "skill routing", run: runOfflineSkillRouting},
		{name: "memory recall", run: runOfflineMemoryRecall},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			offline, production := test.run(t)
			if offline.Environment != adaptive.EvaluationOffline {
				t.Fatalf("environment = %q", offline.Environment)
			}
			if production.Environment !=
				adaptive.EvaluationProductionCanary {
				t.Fatalf(
					"production environment = %q",
					production.Environment,
				)
			}
			assertStaticContractBytes(t, offline, production)
		})
	}
}

// Drift between the recorded policy and the live inputs must fall back to the
// static order WITHOUT persisting any adaptive exposure. The check belongs to the
// shared control machinery; skill routing carries it now that the tool-discovery
// arm is retired.
func TestOfflineOrderingDriftFallsBackWithoutAdaptiveExposure(
	t *testing.T,
) {
	tests := []struct {
		name   string
		mutate func(
			*testing.T,
			*tools.SkillRoutingInput,
			*skillDecision,
			*string,
			*string,
		)
		configure func(*testing.T, skillDecision) json.RawMessage
	}{
		{
			name: "owner",
			mutate: func(
				_ *testing.T,
				input *tools.SkillRoutingInput,
				_ *skillDecision,
				_ *string,
				_ *string,
			) {
				input.OwnerID = uuid.Must(uuid.NewV7()).String()
			},
		},
		{
			name: "provider",
			mutate: func(
				_ *testing.T,
				_ *tools.SkillRoutingInput,
				_ *skillDecision,
				provider *string,
				_ *string,
			) {
				*provider = "other-provider"
			},
		},
		{
			name: "model",
			mutate: func(
				_ *testing.T,
				_ *tools.SkillRoutingInput,
				_ *skillDecision,
				_ *string,
				model *string,
			) {
				*model = "other-model"
			},
		},
		{
			name: "catalog",
			mutate: func(
				t *testing.T,
				input *tools.SkillRoutingInput,
				decision *skillDecision,
				_ *string,
				_ *string,
			) {
				// An action set that no longer matches the domain's own
				// catalog is drift: drop one action from it.
				decision.Snapshot = skillDriftSnapshot(
					t,
					*input,
					decision.Snapshot.FeatureSchema(),
					[]string{tools.SkillRoutingStatic},
				)
			},
		},
		{
			name: "feature",
			mutate: func(
				t *testing.T,
				input *tools.SkillRoutingInput,
				decision *skillDecision,
				_ *string,
				_ *string,
			) {
				decision.Snapshot = skillDriftSnapshot(
					t,
					*input,
					[]adaptive.SnapshotFeatureDefinition{{
						Key:   adaptive.FeatureRecallLimit,
						Scale: 1,
					}},
					skillActionCatalog,
				)
			},
		},
		{
			name: "snapshot",
			mutate: func(
				_ *testing.T,
				_ *tools.SkillRoutingInput,
				_ *skillDecision,
				_ *string,
				_ *string,
			) {
			},
			configure: func(
				t *testing.T,
				decision skillDecision,
			) json.RawMessage {
				var config map[string]any
				if err := json.Unmarshal(
					runtimePolicyConfigJSON(t, decision),
					&config,
				); err != nil {
					t.Fatal(err)
				}
				config["snapshot_sha256"] =
					"abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"
				payload, err := json.Marshal(config)
				if err != nil {
					t.Fatal(err)
				}
				return payload
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := skillInput()
			decision := skillShadowDecision(t, input)
			provider, model := decision.ProviderID, decision.ModelID
			test.mutate(t, &input, &decision, &provider, &model)
			policy := decision.Policy
			policy.Config = runtimePolicyConfigJSON(t, decision)
			if test.configure != nil {
				policy.Config = test.configure(t, decision)
			}
			recorder := &recordingToolEvents{}
			control := NewOfflineSkillRouting(
				policyReaderFunc(func(
					context.Context,
				) (adaptive.Policy, error) {
					return policy, nil
				}),
				snapshotLoaderFunc(func(
					context.Context,
					uuid.UUID,
					uuid.UUID,
				) (*adaptive.PolicySnapshot, error) {
					return decision.Snapshot, nil
				}),
				recorder,
				provider,
				model,
			)
			ids, err := control.OrderSkillRouting(
				t.Context(),
				input,
				func(
					context.Context,
					string,
				) ([]string, error) {
					return []string{"static-result"}, nil
				},
			)
			if err != nil || !slices.Equal(
				ids,
				[]string{"static-result"},
			) {
				t.Fatalf("fallback = (%v, %v)", ids, err)
			}
			if len(recorder.assignments) != 0 ||
				len(recorder.deliveries) != 0 {
				t.Fatalf(
					"drift persisted adaptive exposure: %d/%d",
					len(recorder.assignments),
					len(recorder.deliveries),
				)
			}
		})
	}
}

func runOfflineSkillRouting(
	t *testing.T,
) (adaptive.Assignment, adaptive.Assignment) {
	t.Helper()
	input := skillInput()
	decision := skillShadowDecision(t, input)
	recorder := offlineRecorderForDecision(t, decision)
	control := NewOfflineSkillRouting(
		recorder.reader,
		recorder.loader,
		recorder.events,
		decision.ProviderID,
		decision.ModelID,
	)
	if _, err := control.OrderSkillRouting(
		t.Context(),
		input,
		func(context.Context, string) ([]string, error) {
			return []string{"alpha"}, nil
		},
	); err != nil {
		t.Fatal(err)
	}
	productionDecision, err := newRuntimeSource(
		recorder.reader,
		recorder.loader,
		decision.ProviderID,
		decision.ModelID,
	).decideSkillRouting(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	production, err := newSkillAssignment(input, productionDecision)
	if err != nil {
		t.Fatal(err)
	}
	return recorder.onlyAssignment(t), production
}

func runOfflineMemoryRecall(
	t *testing.T,
) (adaptive.Assignment, adaptive.Assignment) {
	t.Helper()
	input := memoryRecallInput()
	decision := memoryRecallShadowDecision(t, input)
	recorder := offlineRecorderForDecision(t, decision)
	control := NewOfflineDynamicRecall(
		recorder.reader,
		recorder.loader,
		recorder.events,
		decision.ProviderID,
		decision.ModelID,
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
			t.Fatal("static offline recall called the provider")
			return runner.DynamicRecall{}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if prepared == nil {
		t.Fatal("offline recall did not prepare a static delivery")
	}
	productionDecision, err := newRuntimeSource(
		recorder.reader,
		recorder.loader,
		decision.ProviderID,
		decision.ModelID,
	).decideMemoryRecall(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	production, err := newMemoryRecallAssignment(input, productionDecision)
	if err != nil {
		t.Fatal(err)
	}
	return recorder.onlyAssignment(t), production
}

type offlineOrderingRecorder struct {
	reader *countingPolicyReader
	loader *countingSnapshotLoader
	events *recordingToolEvents
}

func offlineRecorderForDecision(
	t *testing.T,
	decision toolDecision,
) offlineOrderingRecorder {
	t.Helper()
	policy := decision.Policy
	policy.Config = runtimePolicyConfigJSON(t, decision)
	return offlineOrderingRecorder{
		reader: &countingPolicyReader{policy: policy},
		loader: &countingSnapshotLoader{snapshot: decision.Snapshot},
		events: &recordingToolEvents{},
	}
}

func (recorder offlineOrderingRecorder) onlyAssignment(
	t *testing.T,
) adaptive.Assignment {
	t.Helper()
	if len(recorder.events.assignments) != 1 {
		t.Fatalf(
			"assignments = %d, want 1",
			len(recorder.events.assignments),
		)
	}
	for _, assignment := range recorder.events.assignments {
		return assignment
	}
	panic("unreachable")
}

func assertStaticContractBytes(
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

func skillDriftSnapshot(
	t *testing.T,
	input tools.SkillRoutingInput,
	features []adaptive.SnapshotFeatureDefinition,
	actionIDs []string,
) *adaptive.PolicySnapshot {
	t.Helper()
	actions := make([]adaptive.SnapshotAction, len(actionIDs))
	for index, actionID := range actionIDs {
		actions[index] = adaptive.SnapshotAction{ActionID: actionID}
	}
	snapshot, err := adaptive.NewPolicySnapshot(adaptive.SnapshotSpec{
		Scope: adaptive.SnapshotScope{
			OwnerID:       uuid.MustParse(input.OwnerID),
			Domain:        adaptive.DomainSkillRouting,
			Point:         adaptive.PointSkillRouting,
			ProviderID:    "openrouter",
			ModelID:       "production-model",
			PolicyVersion: "policy-v1",
			TrainingCutoff: skillShadowDecision(t, input).
				Snapshot.Scope().TrainingCutoff,
		},
		ChampionActionID: adaptive.StaticActionID,
		FeatureSchema:    features,
		Actions:          actions,
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

var _ SnapshotLoader = (*countingSnapshotLoader)(nil)
var _ EventRecorder = (*recordingToolEvents)(nil)
