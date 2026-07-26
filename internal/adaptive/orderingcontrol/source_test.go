package orderingcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type policyReaderFunc func(context.Context) (adaptive.Policy, error)

func (fn policyReaderFunc) CurrentPolicy(
	ctx context.Context,
) (adaptive.Policy, error) {
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

func TestRuntimeSourceLoadsExactScopedSnapshot(t *testing.T) {
	input := toolInput()
	decision := toolShadowDecision(t, input)
	scope := decision.Snapshot.Scope()
	config, err := json.Marshal(map[string]any{
		"owner_id":        scope.OwnerID,
		"domain":          scope.Domain,
		"provider_id":     scope.ProviderID,
		"model_id":        scope.ModelID,
		"snapshot_id":     decision.Snapshot.ID(),
		"snapshot_sha256": decision.Snapshot.SHA256(),
		"cohort_id":       uuid.Must(uuid.NewV7()),
		"cohort_sha256":   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	policy := decision.Policy
	policy.Config = config
	loaded := 0
	source := newRuntimeSource(
		policyReaderFunc(func(context.Context) (adaptive.Policy, error) {
			return policy, nil
		}),
		snapshotLoaderFunc(func(
			_ context.Context,
			ownerID uuid.UUID,
			snapshotID uuid.UUID,
		) (*adaptive.PolicySnapshot, error) {
			loaded++
			if ownerID != scope.OwnerID ||
				snapshotID != decision.Snapshot.ID() {
				t.Fatalf(
					"snapshot scope = %s/%s",
					ownerID,
					snapshotID,
				)
			}
			return decision.Snapshot, nil
		}),
		scope.ProviderID,
		scope.ModelID,
	)

	got, err := source.decideToolDiscovery(t.Context(), input)
	if err != nil {
		t.Fatalf("decideToolDiscovery: %v", err)
	}
	if loaded != 1 || got.Snapshot != decision.Snapshot ||
		got.Policy.Version != policy.Version ||
		got.ProviderID != scope.ProviderID ||
		got.ModelID != scope.ModelID {
		t.Fatalf("decision = %+v, loaded=%d", got, loaded)
	}
}

func TestRuntimeSourceRejectsPolicyScopeDrift(t *testing.T) {
	input := toolInput()
	decision := toolShadowDecision(t, input)
	config := json.RawMessage(`{
		"owner_id":"00000000-0000-0000-0000-000000000001",
		"domain":"skill_routing",
		"provider_id":"openrouter",
		"model_id":"production-model",
		"snapshot_id":"00000000-0000-0000-0000-000000000002",
		"snapshot_sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"cohort_id":"00000000-0000-0000-0000-000000000003",
		"cohort_sha256":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	}`)
	policy := decision.Policy
	policy.Config = config
	source := newRuntimeSource(
		policyReaderFunc(func(context.Context) (adaptive.Policy, error) {
			return policy, nil
		}),
		snapshotLoaderFunc(func(
			context.Context,
			uuid.UUID,
			uuid.UUID,
		) (*adaptive.PolicySnapshot, error) {
			t.Fatal("scope drift loaded a snapshot")
			return nil, nil
		}),
		"openrouter",
		"production-model",
	)

	if _, err := source.decideToolDiscovery(
		t.Context(),
		tools.ToolDiscoveryInput{
			OwnerID: input.OwnerID, RequestID: input.RequestID,
			CandidateIDs: input.CandidateIDs,
		},
	); err == nil {
		t.Fatal("scope drift was accepted")
	}
}

func TestRuntimeSourceFailsClosedAtPolicyAndSnapshotBoundaries(t *testing.T) {
	input := toolInput()
	decision := toolShadowDecision(t, input)
	validPolicy := decision.Policy
	validPolicy.Config = runtimePolicyConfigJSON(t, decision)
	boundaryErr := errors.New("boundary failed")

	t.Run("unavailable", func(t *testing.T) {
		var source *runtimeSource
		if _, err := source.decideToolDiscovery(
			t.Context(),
			input,
		); err == nil {
			t.Fatal("nil source was accepted")
		}
	})
	t.Run("policy read", func(t *testing.T) {
		source := newRuntimeSource(
			policyReaderFunc(func(context.Context) (adaptive.Policy, error) {
				return adaptive.Policy{}, boundaryErr
			}),
			snapshotLoaderFunc(func(
				context.Context,
				uuid.UUID,
				uuid.UUID,
			) (*adaptive.PolicySnapshot, error) {
				return nil, nil
			}),
			"openrouter",
			"production-model",
		)
		if _, err := source.decideToolDiscovery(
			t.Context(),
			input,
		); !errors.Is(err, boundaryErr) {
			t.Fatalf("policy error = %v", err)
		}
	})
	t.Run("disabled", func(t *testing.T) {
		policy := validPolicy
		policy.Mode = adaptive.PolicyOff
		source := newRuntimeSource(
			policyReaderFunc(func(context.Context) (adaptive.Policy, error) {
				return policy, nil
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
			"production-model",
		)
		got, err := source.decideToolDiscovery(t.Context(), input)
		if err != nil || got.Policy.Mode != adaptive.PolicyOff {
			t.Fatalf("disabled decision = (%+v, %v)", got, err)
		}
	})
	t.Run("invalid mode", func(t *testing.T) {
		policy := validPolicy
		policy.Mode = adaptive.PolicyMode("invalid")
		source := runtimeSourceForPolicy(policy, decision.Snapshot)
		if _, err := source.decideToolDiscovery(
			t.Context(),
			input,
		); err == nil {
			t.Fatal("invalid policy mode was accepted")
		}
	})
	t.Run("invalid owner", func(t *testing.T) {
		source := runtimeSourceForPolicy(validPolicy, decision.Snapshot)
		invalid := input
		invalid.OwnerID = "not-a-uuid"
		if _, err := source.decideToolDiscovery(
			t.Context(),
			invalid,
		); err == nil {
			t.Fatal("invalid owner was accepted")
		}
	})
	t.Run("invalid config", func(t *testing.T) {
		policy := validPolicy
		policy.Config = json.RawMessage(`{"unknown":true}`)
		source := runtimeSourceForPolicy(policy, decision.Snapshot)
		if _, err := source.decideToolDiscovery(
			t.Context(),
			input,
		); err == nil {
			t.Fatal("unknown policy config field was accepted")
		}
	})
	t.Run("snapshot load", func(t *testing.T) {
		source := newRuntimeSource(
			policyReaderFunc(func(context.Context) (adaptive.Policy, error) {
				return validPolicy, nil
			}),
			snapshotLoaderFunc(func(
				context.Context,
				uuid.UUID,
				uuid.UUID,
			) (*adaptive.PolicySnapshot, error) {
				return nil, boundaryErr
			}),
			"openrouter",
			"production-model",
		)
		if _, err := source.decideToolDiscovery(
			t.Context(),
			input,
		); !errors.Is(err, boundaryErr) {
			t.Fatalf("snapshot error = %v", err)
		}
	})
	t.Run("snapshot digest", func(t *testing.T) {
		policy := validPolicy
		var config map[string]any
		if err := json.Unmarshal(policy.Config, &config); err != nil {
			t.Fatalf("unmarshal config: %v", err)
		}
		config["snapshot_sha256"] = "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd"
		payload, err := json.Marshal(config)
		if err != nil {
			t.Fatalf("marshal config: %v", err)
		}
		policy.Config = payload
		source := runtimeSourceForPolicy(policy, decision.Snapshot)
		if _, err := source.decideToolDiscovery(
			t.Context(),
			input,
		); err == nil {
			t.Fatal("snapshot digest drift was accepted")
		}
	})
}

func TestRuntimeSourceHelpersRejectInvalidArtifacts(t *testing.T) {
	if newRuntimeSource(nil, nil, "", "") != nil {
		t.Fatal("invalid source dependencies were accepted")
	}
	if NewToolDiscovery(nil, "openrouter", "model") != nil {
		t.Fatal("nil pool produced a discovery control")
	}
	if NewToolDiscovery(
		new(pgxpool.Pool),
		"openrouter",
		"model",
	) == nil {
		t.Fatal("live pool did not produce a discovery control")
	}
	if _, err := decodeRuntimePolicyConfig(
		[]byte(`{} {}`),
	); err == nil {
		t.Fatal("trailing policy config was accepted")
	}
	if validRuntimeSHA256("short") {
		t.Fatal("short digest was accepted")
	}
	if _, err := snapshotFeatureValues(
		nil,
		map[adaptive.FeatureKey]float64{},
	); err == nil {
		t.Fatal("nil snapshot was accepted")
	}
	input := toolInput()
	decision := toolShadowDecision(t, input)
	if _, err := snapshotFeatureValues(
		decision.Snapshot,
		map[adaptive.FeatureKey]float64{
			adaptive.FeatureCandidateCount: 2,
		},
	); err == nil {
		t.Fatal("incomplete feature projection was accepted")
	}
}

func runtimePolicyConfigJSON(
	t *testing.T,
	decision toolDecision,
) json.RawMessage {
	t.Helper()
	scope := decision.Snapshot.Scope()
	config, err := json.Marshal(map[string]any{
		"owner_id":        scope.OwnerID,
		"domain":          scope.Domain,
		"provider_id":     scope.ProviderID,
		"model_id":        scope.ModelID,
		"snapshot_id":     decision.Snapshot.ID(),
		"snapshot_sha256": decision.Snapshot.SHA256(),
		"cohort_id":       uuid.Must(uuid.NewV7()),
		"cohort_sha256":   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	return config
}

func runtimeSourceForPolicy(
	policy adaptive.Policy,
	snapshot *adaptive.PolicySnapshot,
) *runtimeSource {
	return newRuntimeSource(
		policyReaderFunc(func(context.Context) (adaptive.Policy, error) {
			return policy, nil
		}),
		snapshotLoaderFunc(func(
			context.Context,
			uuid.UUID,
			uuid.UUID,
		) (*adaptive.PolicySnapshot, error) {
			return snapshot, nil
		}),
		"openrouter",
		"production-model",
	)
}
