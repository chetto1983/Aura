package reasoningcontrol

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
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

// New binds per-round reasoning decisions to fresh PostgreSQL policy and
// immutable-snapshot reads.
func New(
	pool *pgxpool.Pool,
	providerID string,
	modelID string,
) agent.ReasoningControl {
	if pool == nil || providerID == "" || modelID == "" {
		return nil
	}
	return newControl(
		newRuntimeSource(
			adaptive.NewPolicyStore(pool),
			adaptive.NewSnapshotStore(pool),
			providerID,
			modelID,
		),
		adaptive.NewStore(pool, adaptive.StoreConfig{}),
	)
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

func (source *runtimeSource) DecideReasoning(
	ctx context.Context,
	input agent.ReasoningControlInput,
) (Decision, error) {
	if source == nil || source.policies == nil || source.snapshots == nil ||
		(source.environment != adaptive.EvaluationProductionCanary &&
			source.environment != adaptive.EvaluationOffline) {
		return Decision{}, errors.New(
			"adaptive reasoning decision source is unavailable",
		)
	}
	policy, err := source.policies.CurrentPolicy(ctx)
	if err != nil {
		return Decision{}, err
	}
	if policy.Mode == adaptive.PolicyOff ||
		policy.Mode == adaptive.PolicyRollback {
		return Decision{Policy: policy}, nil
	}
	if source.environment == adaptive.EvaluationOffline &&
		(policy.Mode != adaptive.PolicyShadow ||
			policy.RolloutBPS != 0) {
		return Decision{}, errors.New(
			"adaptive offline reasoning requires shadow mode at rollout zero",
		)
	}
	if policy.Epoch <= 0 || policy.Version == "" ||
		(policy.Mode != adaptive.PolicyShadow &&
			policy.Mode != adaptive.PolicyCanary &&
			policy.Mode != adaptive.PolicyActive) {
		return Decision{}, errors.New(
			"adaptive reasoning policy state is invalid",
		)
	}
	if source.environment == adaptive.EvaluationOffline &&
		input.Override != "" {
		return Decision{}, errors.New(
			"adaptive offline reasoning forbids operator overrides",
		)
	}
	if input.OwnerID == uuid.Nil ||
		input.ProviderID != source.providerID ||
		input.ModelID != source.modelID ||
		input.MessageCount < 0 ||
		input.MessageCount > 1_000_000_000 {
		return Decision{}, errors.New(
			"adaptive reasoning model-round scope is invalid",
		)
	}
	config, err := decodeRuntimePolicyConfig(policy.Config)
	if err != nil {
		return Decision{}, err
	}
	if err := source.validateConfig(input.OwnerID, config); err != nil {
		return Decision{}, err
	}
	snapshot, err := source.snapshots.Load(
		ctx,
		config.OwnerID,
		config.SnapshotID,
	)
	if err != nil {
		return Decision{}, err
	}
	if snapshot == nil || snapshot.SHA256() != config.SnapshotSHA {
		return Decision{}, errors.New(
			"adaptive reasoning snapshot digest does not match policy",
		)
	}
	if err := source.validateSnapshot(
		input.OwnerID,
		policy.Version,
		snapshot,
	); err != nil {
		return Decision{}, err
	}
	scored := snapshot.Score(adaptive.SnapshotQuery{
		OwnerID: input.OwnerID, Domain: adaptive.DomainReasoning,
		Point:      adaptive.PointReasoning,
		ProviderID: source.providerID, ModelID: source.modelID,
		PolicyVersion: policy.Version,
		Features: []adaptive.SnapshotFeatureValue{{
			Key:   adaptive.FeatureMessageCount,
			Value: float64(input.MessageCount),
		}},
	})
	recommendedTier, ok := tierForAction(scored.ActionID)
	if !ok {
		return Decision{}, errors.New(
			"adaptive reasoning snapshot selected an unsupported action",
		)
	}
	return Decision{
		Policy: policy, Snapshot: snapshot,
		Environment:     source.environment,
		RecommendedTier: recommendedTier,
	}, nil
}

func (source *runtimeSource) validateConfig(
	ownerID uuid.UUID,
	config runtimePolicyConfig,
) error {
	if config.OwnerID != ownerID ||
		config.Domain != adaptive.DomainReasoning ||
		config.ProviderID != source.providerID ||
		config.ModelID != source.modelID ||
		config.SnapshotID == uuid.Nil ||
		config.CohortID == uuid.Nil ||
		!validRuntimeSHA256(config.SnapshotSHA) ||
		!validRuntimeSHA256(config.CohortSHA256) {
		return errors.New(
			"adaptive reasoning policy scope does not match decision",
		)
	}
	return nil
}

func (source *runtimeSource) validateSnapshot(
	ownerID uuid.UUID,
	policyVersion string,
	snapshot *adaptive.PolicySnapshot,
) error {
	scope := snapshot.Scope()
	if scope.OwnerID != ownerID ||
		scope.Domain != adaptive.DomainReasoning ||
		scope.Point != adaptive.PointReasoning ||
		scope.ProviderID != source.providerID ||
		scope.ModelID != source.modelID ||
		scope.PolicyVersion != policyVersion ||
		snapshot.ChampionActionID() != adaptive.StaticActionID {
		return errors.New(
			"adaptive reasoning snapshot scope does not match decision",
		)
	}
	features := snapshot.FeatureSchema()
	if len(features) != 1 ||
		features[0].Key != adaptive.FeatureMessageCount {
		return errors.New(
			"adaptive reasoning snapshot feature schema is unsupported",
		)
	}
	actions := snapshot.Actions()
	actionIDs := make([]string, len(actions))
	for index, action := range actions {
		actionIDs[index] = action.ActionID
	}
	if !slices.Equal(actionIDs, actionCatalog) {
		return errors.New(
			"adaptive reasoning snapshot action catalog is not frozen",
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
			"decode adaptive reasoning policy config: %w",
			err,
		)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return runtimePolicyConfig{}, errors.New(
			"adaptive reasoning policy config contains trailing content",
		)
	}
	return config, nil
}

func validRuntimeSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func tierForAction(actionID string) (prompt.ReasoningTier, bool) {
	switch agent.ReasoningAction(actionID) {
	case agent.ReasoningActionHigh:
		return prompt.ReasoningTierHigh, true
	case agent.ReasoningActionLow:
		return prompt.ReasoningTierLow, true
	case agent.ReasoningActionNone:
		return prompt.ReasoningTierNone, true
	case agent.ReasoningActionStatic:
		return "", true
	default:
		return "", false
	}
}

var _ DecisionSource = (*runtimeSource)(nil)
