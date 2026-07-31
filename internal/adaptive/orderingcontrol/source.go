package orderingcontrol

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

// SnapshotLoader reads one immutable snapshot by owner and deterministic ID.
type SnapshotLoader interface {
	Load(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (*adaptive.PolicySnapshot, error)
}

type runtimeSource struct {
	policies    adaptive.PolicyReader
	snapshots   SnapshotLoader
	providerID  string
	modelID     string
	environment adaptive.EvaluationEnvironment
}

type runtimePolicyConfig struct {
	OwnerID      uuid.UUID       `json:"owner_id"`
	Domain       adaptive.Domain `json:"domain"`
	ProviderID   string          `json:"provider_id"`
	ModelID      string          `json:"model_id"`
	SnapshotID   uuid.UUID       `json:"snapshot_id"`
	SnapshotSHA  string          `json:"snapshot_sha256"`
	CohortID     uuid.UUID       `json:"cohort_id"`
	CohortSHA256 string          `json:"cohort_sha256"`
}

func newRuntimeSource(
	policies adaptive.PolicyReader,
	snapshots SnapshotLoader,
	providerID string,
	modelID string,
) *runtimeSource {
	return newRuntimeSourceForEnvironment(
		policies,
		snapshots,
		providerID,
		modelID,
		adaptive.EvaluationProductionCanary,
	)
}

func newOfflineRuntimeSource(
	policies adaptive.PolicyReader,
	snapshots SnapshotLoader,
	providerID string,
	modelID string,
) *runtimeSource {
	return newRuntimeSourceForEnvironment(
		policies,
		snapshots,
		providerID,
		modelID,
		adaptive.EvaluationOffline,
	)
}

func newRuntimeSourceForEnvironment(
	policies adaptive.PolicyReader,
	snapshots SnapshotLoader,
	providerID string,
	modelID string,
	environment adaptive.EvaluationEnvironment,
) *runtimeSource {
	if policies == nil || snapshots == nil ||
		providerID == "" || modelID == "" ||
		(environment != adaptive.EvaluationProductionCanary &&
			environment != adaptive.EvaluationOffline) {
		return nil
	}
	return &runtimeSource{
		policies: policies, snapshots: snapshots,
		providerID: providerID, modelID: modelID,
		environment: environment,
	}
}

func (source *runtimeSource) decideSkillRouting(
	ctx context.Context,
	input tools.SkillRoutingInput,
) (skillDecision, error) {
	return source.decideOrdering(
		ctx,
		input.OwnerID,
		adaptive.DomainSkillRouting,
		adaptive.PointSkillRouting,
		map[adaptive.FeatureKey]float64{
			adaptive.FeatureQueryLength: float64(input.QueryLength),
			adaptive.FeatureSkillCandidateCount: float64(
				len(input.CandidateIDs),
			),
		},
	)
}

func (source *runtimeSource) decideDocumentRetrieval(
	ctx context.Context,
	input tools.DocumentRetrievalInput,
) (toolDecision, error) {
	return source.decideOrdering(
		ctx,
		input.OwnerID,
		adaptive.DomainKnowledge,
		adaptive.PointKnowledge,
		map[adaptive.FeatureKey]float64{
			adaptive.FeatureCandidateCount: float64(len(input.PlanIDs)),
			adaptive.FeatureQueryLength:    float64(input.QueryLength),
			adaptive.FeatureRetrievalLimit: float64(input.MaxResults),
		},
	)
}

func (source *runtimeSource) decideMemoryRecall(
	ctx context.Context,
	input runner.DynamicRecallInput,
) (toolDecision, error) {
	return source.decideOrdering(
		ctx,
		input.OwnerID.String(),
		adaptive.DomainMemoryRecall,
		adaptive.PointMemoryRecall,
		map[adaptive.FeatureKey]float64{
			adaptive.FeatureQueryLength: float64(len([]rune(input.Query))),
			adaptive.FeatureRecallLimit: float64(input.MaxItems),
		},
	)
}

func (source *runtimeSource) decideOrdering(
	ctx context.Context,
	owner string,
	domain adaptive.Domain,
	point adaptive.DecisionPoint,
	values map[adaptive.FeatureKey]float64,
) (toolDecision, error) {
	if source == nil || source.policies == nil ||
		source.snapshots == nil ||
		(source.environment != adaptive.EvaluationProductionCanary &&
			source.environment != adaptive.EvaluationOffline) {
		return toolDecision{}, errors.New(
			"adaptive ordering decision source is unavailable",
		)
	}
	policy, err := source.policies.CurrentPolicy(ctx)
	if err != nil {
		return toolDecision{}, err
	}
	if policy.Mode == adaptive.PolicyOff ||
		policy.Mode == adaptive.PolicyRollback {
		return toolDecision{Policy: policy}, nil
	}
	if source.environment == adaptive.EvaluationOffline &&
		(policy.Mode != adaptive.PolicyShadow ||
			policy.RolloutBPS != 0) {
		return toolDecision{}, errors.New(
			"adaptive offline ordering requires shadow mode at rollout zero",
		)
	}
	if policy.Mode != adaptive.PolicyShadow &&
		policy.Mode != adaptive.PolicyCanary &&
		policy.Mode != adaptive.PolicyActive {
		return toolDecision{}, fmt.Errorf(
			"adaptive ordering policy mode %q is invalid",
			policy.Mode,
		)
	}
	ownerID, err := uuid.Parse(owner)
	if err != nil || ownerID == uuid.Nil {
		return toolDecision{}, errors.New(
			"adaptive ordering owner identity is invalid",
		)
	}
	config, err := decodeRuntimePolicyConfig(policy.Config)
	if err != nil {
		return toolDecision{}, err
	}
	if err := source.validateScope(
		ownerID,
		domain,
		config,
	); err != nil {
		return toolDecision{}, err
	}
	snapshot, err := source.snapshots.Load(
		ctx,
		config.OwnerID,
		config.SnapshotID,
	)
	if err != nil {
		return toolDecision{}, err
	}
	if snapshot.SHA256() != config.SnapshotSHA {
		return toolDecision{}, errors.New(
			"adaptive ordering snapshot digest does not match policy",
		)
	}
	features, err := snapshotFeatureValues(snapshot, values)
	if err != nil {
		return toolDecision{}, err
	}
	scored := snapshot.Score(adaptive.SnapshotQuery{
		OwnerID:       ownerID,
		Domain:        domain,
		Point:         point,
		ProviderID:    source.providerID,
		ModelID:       source.modelID,
		PolicyVersion: policy.Version,
		Features:      features,
	})
	return toolDecision{
		Policy: policy, Snapshot: snapshot,
		Environment:       source.environment,
		RecommendedAction: scored.ActionID,
		ProviderID:        source.providerID, ModelID: source.modelID,
	}, nil
}

func (source *runtimeSource) validateScope(
	ownerID uuid.UUID,
	domain adaptive.Domain,
	config runtimePolicyConfig,
) error {
	if config.OwnerID != ownerID ||
		config.Domain != domain ||
		config.ProviderID != source.providerID ||
		config.ModelID != source.modelID ||
		config.SnapshotID == uuid.Nil ||
		config.CohortID == uuid.Nil ||
		!validRuntimeSHA256(config.SnapshotSHA) ||
		!validRuntimeSHA256(config.CohortSHA256) {
		return errors.New(
			"adaptive runtime policy scope does not match decision",
		)
	}
	return nil
}

func decodeRuntimePolicyConfig(
	payload []byte,
) (runtimePolicyConfig, error) {
	var config runtimePolicyConfig
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return runtimePolicyConfig{}, fmt.Errorf(
			"decode adaptive runtime policy config: %w",
			err,
		)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return runtimePolicyConfig{}, errors.New(
			"adaptive runtime policy config contains trailing content",
		)
	}
	return config, nil
}

func validRuntimeSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func snapshotFeatureValues(
	snapshot *adaptive.PolicySnapshot,
	values map[adaptive.FeatureKey]float64,
) ([]adaptive.SnapshotFeatureValue, error) {
	if snapshot == nil {
		return nil, errors.New(
			"adaptive runtime policy snapshot is unavailable",
		)
	}
	schema := snapshot.FeatureSchema()
	keys := make([]adaptive.FeatureKey, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if len(schema) != len(keys) {
		return nil, errors.New(
			"adaptive runtime feature schema is not frozen",
		)
	}
	features := make([]adaptive.SnapshotFeatureValue, len(schema))
	for index, definition := range schema {
		if definition.Key != keys[index] {
			return nil, errors.New(
				"adaptive runtime feature schema is not frozen",
			)
		}
		value, ok := values[definition.Key]
		if !ok {
			return nil, fmt.Errorf(
				"adaptive runtime feature %q is unsupported",
				definition.Key,
			)
		}
		features[index] = adaptive.SnapshotFeatureValue{
			Key: definition.Key, Value: value,
		}
	}
	return features, nil
}
