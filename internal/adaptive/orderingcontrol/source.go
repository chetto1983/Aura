package orderingcontrol

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type snapshotLoader interface {
	Load(
		context.Context,
		uuid.UUID,
		uuid.UUID,
	) (*adaptive.PolicySnapshot, error)
}

type runtimeSource struct {
	policies   adaptive.PolicyReader
	snapshots  snapshotLoader
	providerID string
	modelID    string
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
	snapshots snapshotLoader,
	providerID string,
	modelID string,
) *runtimeSource {
	if policies == nil || snapshots == nil ||
		providerID == "" || modelID == "" {
		return nil
	}
	return &runtimeSource{
		policies: policies, snapshots: snapshots,
		providerID: providerID, modelID: modelID,
	}
}

// NewToolDiscovery binds typed discovery decisions to authoritative runtime
// policy, immutable snapshots, and schema-2 ledger persistence.
func NewToolDiscovery(
	pool *pgxpool.Pool,
	providerID string,
	modelID string,
) tools.ToolDiscoveryControl {
	if pool == nil {
		return nil
	}
	return newToolDiscovery(
		newRuntimeSource(
			adaptive.NewPolicyStore(pool),
			adaptive.NewSnapshotStore(pool),
			providerID,
			modelID,
		),
		adaptive.NewStore(pool, adaptive.StoreConfig{}),
	)
}

func (source *runtimeSource) decideToolDiscovery(
	ctx context.Context,
	input tools.ToolDiscoveryInput,
) (toolDecision, error) {
	return source.decideOrdering(
		ctx,
		input.OwnerID,
		adaptive.DomainToolDiscovery,
		adaptive.PointToolDiscovery,
		map[adaptive.FeatureKey]float64{
			adaptive.FeatureCandidateCount: float64(len(input.CandidateIDs)),
			adaptive.FeatureQueryLength:    float64(input.QueryLength),
			adaptive.FeatureRetrievalLimit: float64(input.MaxResults),
		},
	)
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

func (source *runtimeSource) decideOrdering(
	ctx context.Context,
	owner string,
	domain adaptive.Domain,
	point adaptive.DecisionPoint,
	values map[adaptive.FeatureKey]float64,
) (toolDecision, error) {
	if source == nil || source.policies == nil ||
		source.snapshots == nil {
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
		Environment:       adaptive.EvaluationProductionCanary,
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
	features := make([]adaptive.SnapshotFeatureValue, len(schema))
	for index, definition := range schema {
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
