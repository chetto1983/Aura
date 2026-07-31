package orderingcontrol

import (
	"context"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/google/uuid"
)

func TestOfflineOrderingRejectsFeatureSchemaDriftForEveryDomain(
	t *testing.T,
) {
	tests := []struct {
		name     string
		expected []adaptive.FeatureKey
		run      func(*testing.T, []adaptive.FeatureKey) error
	}{
		{
			name: "skill routing",
			expected: []adaptive.FeatureKey{
				adaptive.FeatureQueryLength,
				adaptive.FeatureSkillCandidateCount,
			},
			run: rejectSkillFeatureSchema,
		},
		{
			name: "memory recall",
			expected: []adaptive.FeatureKey{
				adaptive.FeatureQueryLength,
				adaptive.FeatureRecallLimit,
			},
			run: rejectRecallFeatureSchema,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			drifts := []struct {
				name string
				keys []adaptive.FeatureKey
			}{
				{
					name: "subset",
					keys: slices.Clone(
						test.expected[:len(test.expected)-1],
					),
				},
				{
					name: "superset",
					keys: sortedFeatureKeys(
						test.expected,
						adaptive.FeatureAvailableToolCount,
					),
				},
				{
					name: "foreign",
					keys: sortedFeatureKeys(
						test.expected[1:],
						adaptive.FeatureAvailableToolCount,
					),
				},
			}
			for _, drift := range drifts {
				t.Run(drift.name, func(t *testing.T) {
					if err := test.run(t, drift.keys); err == nil {
						t.Fatal("feature schema drift was accepted")
					}
				})
			}
		})
	}
}

func rejectSkillFeatureSchema(
	t *testing.T,
	keys []adaptive.FeatureKey,
) error {
	t.Helper()
	input := skillInput()
	decision := skillShadowDecision(t, input)
	decision.Snapshot = snapshotWithFeatureKeys(
		t,
		decision.Snapshot,
		keys,
	)
	source := offlineFeatureSource(t, decision)
	_, err := source.decideSkillRouting(t.Context(), input)
	return err
}

func rejectRecallFeatureSchema(
	t *testing.T,
	keys []adaptive.FeatureKey,
) error {
	t.Helper()
	input := memoryRecallInput()
	decision := memoryRecallShadowDecision(t, input)
	decision.Snapshot = snapshotWithFeatureKeys(
		t,
		decision.Snapshot,
		keys,
	)
	source := offlineFeatureSource(t, decision)
	_, err := source.decideMemoryRecall(t.Context(), input)
	return err
}

func offlineFeatureSource(
	t *testing.T,
	decision skillDecision,
) *runtimeSource {
	t.Helper()
	policy := decision.Policy
	policy.Config = runtimePolicyConfigJSON(t, decision)
	return newOfflineRuntimeSource(
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
		decision.ProviderID,
		decision.ModelID,
	)
}

func snapshotWithFeatureKeys(
	t *testing.T,
	snapshot *adaptive.PolicySnapshot,
	keys []adaptive.FeatureKey,
) *adaptive.PolicySnapshot {
	t.Helper()
	schema := make([]adaptive.SnapshotFeatureDefinition, len(keys))
	for index, key := range keys {
		schema[index] = adaptive.SnapshotFeatureDefinition{
			Key:   key,
			Scale: 1,
		}
	}
	replacement, err := adaptive.NewPolicySnapshot(adaptive.SnapshotSpec{
		Scope:            snapshot.Scope(),
		ChampionActionID: snapshot.ChampionActionID(),
		FeatureSchema:    schema,
		Actions:          snapshot.Actions(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return replacement
}

func sortedFeatureKeys(
	keys []adaptive.FeatureKey,
	extra adaptive.FeatureKey,
) []adaptive.FeatureKey {
	sorted := append(slices.Clone(keys), extra)
	slices.Sort(sorted)
	return sorted
}
