package reasoningcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type policyReaderFunc func(context.Context) (adaptive.Policy, error)

func (fn policyReaderFunc) CurrentPolicy(ctx context.Context) (adaptive.Policy, error) {
	return fn(ctx)
}

type snapshotLoaderFunc func(
	context.Context,
	uuid.UUID,
	uuid.UUID,
) (*adaptive.PolicySnapshot, error)

func (fn snapshotLoaderFunc) Load(
	ctx context.Context,
	ownerID uuid.UUID,
	snapshotID uuid.UUID,
) (*adaptive.PolicySnapshot, error) {
	return fn(ctx, ownerID, snapshotID)
}

func TestRuntimeSourceReloadsExactReasoningStateForEveryDecision(t *testing.T) {
	input := testInput()
	snapshot := scoredReasoningSnapshot(
		t,
		input,
		"policy-v1",
		adaptive.FeatureMessageCount,
	)
	policy := runtimePolicy(t, snapshot, adaptive.PolicyShadow)
	policyReads := 0
	snapshotReads := 0
	source := newRuntimeSource(
		policyReaderFunc(func(context.Context) (adaptive.Policy, error) {
			policyReads++
			return policy, nil
		}),
		snapshotLoaderFunc(func(
			_ context.Context,
			ownerID uuid.UUID,
			snapshotID uuid.UUID,
		) (*adaptive.PolicySnapshot, error) {
			snapshotReads++
			if ownerID != input.OwnerID || snapshotID != snapshot.ID() {
				t.Fatalf("snapshot load scope = %s/%s", ownerID, snapshotID)
			}
			return snapshot, nil
		}),
		input.ProviderID,
		input.ModelID,
	)

	for range 2 {
		decision, err := source.DecideReasoning(t.Context(), input)
		if err != nil {
			t.Fatalf("DecideReasoning: %v", err)
		}
		if decision.Policy.Epoch != policy.Epoch ||
			decision.Policy.Version != policy.Version ||
			decision.Snapshot != snapshot ||
			decision.Environment != adaptive.EvaluationProductionCanary ||
			decision.RecommendedTier != prompt.ReasoningTierHigh {
			t.Fatalf("decision = %+v", decision)
		}
	}
	if policyReads != 2 || snapshotReads != 2 {
		t.Fatalf(
			"decision reads = %d policy/%d snapshot, want 2/2",
			policyReads, snapshotReads,
		)
	}
}

func TestRuntimeSourceKeepsDisabledModesStaticWithoutSnapshotRead(t *testing.T) {
	for _, mode := range []adaptive.PolicyMode{
		adaptive.PolicyOff,
		adaptive.PolicyRollback,
	} {
		t.Run(string(mode), func(t *testing.T) {
			source := newRuntimeSource(
				policyReaderFunc(func(context.Context) (adaptive.Policy, error) {
					return adaptive.Policy{
						Epoch: 1, Version: "policy-v1", Mode: mode,
					}, nil
				}),
				snapshotLoaderFunc(func(
					context.Context,
					uuid.UUID,
					uuid.UUID,
				) (*adaptive.PolicySnapshot, error) {
					t.Fatal("disabled policy loaded a snapshot")
					return nil, nil
				}),
				"openrouter",
				"test-model",
			)

			decision, err := source.DecideReasoning(t.Context(), testInput())
			if err != nil || decision.Policy.Mode != mode ||
				decision.Snapshot != nil {
				t.Fatalf("disabled decision = (%+v, %v)", decision, err)
			}
		})
	}
}

func TestRuntimeSourceFailsClosedAtTrustBoundaries(t *testing.T) {
	input := testInput()
	snapshot := scoredReasoningSnapshot(
		t,
		input,
		"policy-v1",
		adaptive.FeatureMessageCount,
	)
	unsupportedSnapshot := scoredReasoningSnapshot(
		t,
		input,
		"policy-v1",
		adaptive.FeatureCandidateCount,
	)
	validPolicy := runtimePolicy(t, snapshot, adaptive.PolicyShadow)
	boundaryErr := errors.New("boundary failed")

	tests := []struct {
		name      string
		input     agent.ReasoningControlInput
		policy    adaptive.Policy
		policyErr error
		snapshot  *adaptive.PolicySnapshot
		loadErr   error
	}{
		{
			name: "policy read", input: input, policy: validPolicy,
			policyErr: boundaryErr, snapshot: snapshot,
		},
		{
			name: "policy epoch", input: input,
			policy: func() adaptive.Policy {
				policy := validPolicy
				policy.Epoch = 0
				return policy
			}(),
			snapshot: snapshot,
		},
		{
			name: "policy mode", input: input,
			policy: func() adaptive.Policy {
				policy := validPolicy
				policy.Mode = adaptive.PolicyMode("invalid")
				return policy
			}(),
			snapshot: snapshot,
		},
		{
			name: "input provider", input: func() agent.ReasoningControlInput {
				changed := input
				changed.ProviderID = "other"
				return changed
			}(),
			policy: validPolicy, snapshot: snapshot,
		},
		{
			name: "policy config", input: input,
			policy: func() adaptive.Policy {
				policy := validPolicy
				policy.Config = json.RawMessage(`{"unknown":true}`)
				return policy
			}(),
			snapshot: snapshot,
		},
		{
			name: "snapshot read", input: input, policy: validPolicy,
			snapshot: snapshot, loadErr: boundaryErr,
		},
		{
			name: "snapshot digest", input: input,
			policy: func() adaptive.Policy {
				policy := validPolicy
				policy.Config = runtimePolicyJSON(
					t,
					snapshot,
					strings.Repeat("a", 64),
					adaptive.DomainReasoning,
				)
				return policy
			}(),
			snapshot: snapshot,
		},
		{
			name: "uppercase cohort digest", input: input,
			policy: func() adaptive.Policy {
				policy := validPolicy
				policy.Config = runtimePolicyJSONWithCohortSHA(
					t,
					snapshot,
					snapshot.SHA256(),
					adaptive.DomainReasoning,
					strings.ToUpper(strings.Repeat("b", 64)),
				)
				return policy
			}(),
			snapshot: snapshot,
		},
		{
			name: "snapshot feature", input: input,
			policy: func() adaptive.Policy {
				policy := validPolicy
				policy.Config = runtimePolicyJSON(
					t,
					unsupportedSnapshot,
					unsupportedSnapshot.SHA256(),
					adaptive.DomainReasoning,
				)
				return policy
			}(),
			snapshot: unsupportedSnapshot,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := newRuntimeSource(
				policyReaderFunc(func(context.Context) (adaptive.Policy, error) {
					return test.policy, test.policyErr
				}),
				snapshotLoaderFunc(func(
					context.Context,
					uuid.UUID,
					uuid.UUID,
				) (*adaptive.PolicySnapshot, error) {
					return test.snapshot, test.loadErr
				}),
				input.ProviderID,
				input.ModelID,
			)
			if _, err := source.DecideReasoning(
				t.Context(),
				test.input,
			); err == nil {
				t.Fatal("untrusted runtime state was accepted")
			}
		})
	}
}

func TestNewRequiresCompleteProductionBinding(t *testing.T) {
	pool := &pgxpool.Pool{}
	if New(nil, "openrouter", "test-model") != nil ||
		New(pool, "", "test-model") != nil ||
		New(pool, "openrouter", "") != nil {
		t.Fatal("incomplete production binding returned a control")
	}
	if New(pool, "openrouter", "test-model") == nil {
		t.Fatal("complete production binding did not return a control")
	}
}

func runtimePolicy(
	t *testing.T,
	snapshot *adaptive.PolicySnapshot,
	mode adaptive.PolicyMode,
) adaptive.Policy {
	t.Helper()
	return adaptive.Policy{
		Epoch: 1, Version: "policy-v1", Mode: mode,
		Config: runtimePolicyJSON(
			t,
			snapshot,
			snapshot.SHA256(),
			adaptive.DomainReasoning,
		),
	}
}

func runtimePolicyJSON(
	t *testing.T,
	snapshot *adaptive.PolicySnapshot,
	snapshotSHA string,
	domain adaptive.Domain,
) json.RawMessage {
	t.Helper()
	return runtimePolicyJSONWithCohortSHA(
		t,
		snapshot,
		snapshotSHA,
		domain,
		strings.Repeat("b", 64),
	)
}

func runtimePolicyJSONWithCohortSHA(
	t *testing.T,
	snapshot *adaptive.PolicySnapshot,
	snapshotSHA string,
	domain adaptive.Domain,
	cohortSHA string,
) json.RawMessage {
	t.Helper()
	scope := snapshot.Scope()
	payload, err := json.Marshal(map[string]any{
		"owner_id":        scope.OwnerID,
		"domain":          domain,
		"provider_id":     scope.ProviderID,
		"model_id":        scope.ModelID,
		"snapshot_id":     snapshot.ID(),
		"snapshot_sha256": snapshotSHA,
		"cohort_id":       uuid.Must(uuid.NewV7()),
		"cohort_sha256":   cohortSHA,
	})
	if err != nil {
		t.Fatalf("marshal runtime policy: %v", err)
	}
	return payload
}

func scoredReasoningSnapshot(
	t *testing.T,
	input agent.ReasoningControlInput,
	policyVersion string,
	feature adaptive.FeatureKey,
) *adaptive.PolicySnapshot {
	t.Helper()
	actions := make([]adaptive.SnapshotAction, len(actionCatalog))
	utilities := []float64{0.9, 0.4, 0.3, 0.5}
	for index, actionID := range actionCatalog {
		actions[index] = adaptive.SnapshotAction{
			ActionID: actionID,
			Examples: []adaptive.SnapshotExample{{
				OutcomeID: uuid.Must(uuid.NewV7()),
				OwnerID:   input.OwnerID, Domain: adaptive.DomainReasoning,
				Point:      adaptive.PointReasoning,
				ProviderID: input.ProviderID, ModelID: input.ModelID,
				ObservedAt: time.Unix(1, 0).UTC(), Eligible: true,
				Utility: utilities[index],
				Features: []adaptive.SnapshotFeatureValue{{
					Key: feature, Value: float64(input.MessageCount),
				}},
			}},
		}
	}
	snapshot, err := adaptive.NewPolicySnapshot(adaptive.SnapshotSpec{
		Scope: adaptive.SnapshotScope{
			OwnerID: input.OwnerID, Domain: adaptive.DomainReasoning,
			Point: adaptive.PointReasoning, ProviderID: input.ProviderID,
			ModelID: input.ModelID, PolicyVersion: policyVersion,
			TrainingCutoff: time.Unix(2, 0).UTC(),
		},
		ChampionActionID: adaptive.StaticActionID,
		FeatureSchema: []adaptive.SnapshotFeatureDefinition{{
			Key: feature, Center: 0, Scale: 1,
		}},
		Actions: actions,
	})
	if err != nil {
		t.Fatalf("NewPolicySnapshot: %v", err)
	}
	return snapshot
}
