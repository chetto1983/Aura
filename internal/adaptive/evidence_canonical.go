package adaptive

import (
	"encoding/json"
	"errors"
	"fmt"
)

type canonicalEvidenceArtifact interface {
	validateCanonical() error
}

func canonicalArtifactSHA256(artifact canonicalEvidenceArtifact) (string, error) {
	if err := artifact.validateCanonical(); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(artifact)
	if err != nil {
		return "", fmt.Errorf("marshal canonical evidence artifact: %w", err)
	}
	return sha256Hex(canonical), nil
}

// ActionSchemaArtifact freezes the ordered action catalog.
type ActionSchemaArtifact struct {
	SchemaID string   `json:"schema_id"`
	Revision int      `json:"revision"`
	Actions  []string `json:"actions"`
}

func (artifact ActionSchemaArtifact) validateCanonical() error {
	if artifact.SchemaID != "aura.adaptive.action-schema/v1" ||
		artifact.Revision != 1 ||
		len(artifact.Actions) == 0 {
		return errors.New("action schema artifact is invalid")
	}
	for index, actionID := range artifact.Actions {
		if actionID == "" || index > 0 && artifact.Actions[index-1] >= actionID {
			return errors.New("action schema actions are not in unique canonical order")
		}
	}
	return nil
}

// SHA256 returns the canonical action-schema content address.
func (artifact ActionSchemaArtifact) SHA256() (string, error) {
	return canonicalArtifactSHA256(artifact)
}

// EmptyFeatureSchemaArtifact is permitted only for context-free endpoint models.
type EmptyFeatureSchemaArtifact struct {
	SchemaID string     `json:"schema_id"`
	Revision int        `json:"revision"`
	Features []struct{} `json:"features"`
}

func (artifact EmptyFeatureSchemaArtifact) validateCanonical() error {
	if artifact.SchemaID != "aura.adaptive.feature-schema/empty/v1" ||
		artifact.Revision != 1 ||
		artifact.Features == nil ||
		len(artifact.Features) != 0 {
		return errors.New("empty feature schema artifact is invalid")
	}
	return nil
}

// SHA256 returns the universal empty-feature-schema content address.
func (artifact EmptyFeatureSchemaArtifact) SHA256() (string, error) {
	return canonicalArtifactSHA256(artifact)
}

// TargetPolicyManifest freezes the deterministic challenger policy.
type TargetPolicyManifest struct {
	SchemaID           string `json:"schema_id"`
	Revision           int    `json:"revision"`
	PolicyID           string `json:"policy_id"`
	PolicyRevision     int    `json:"policy_revision"`
	ActionSchemaSHA256 string `json:"action_schema_sha256"`
	Rule               string `json:"rule"`
}

func (manifest TargetPolicyManifest) validateCanonical() error {
	if manifest.SchemaID != "aura.adaptive.policy-manifest/target/v1" ||
		manifest.Revision != 1 ||
		manifest.PolicyID != "aura/deterministic-challenger" ||
		manifest.PolicyRevision != 1 ||
		!validSHA256(manifest.ActionSchemaSHA256) ||
		manifest.Rule != "snapshot_recommended_action" {
		return errors.New("target policy manifest is invalid")
	}
	return nil
}

// SHA256 returns the canonical target-policy content address.
func (manifest TargetPolicyManifest) SHA256() (string, error) {
	return canonicalArtifactSHA256(manifest)
}

// BehaviorPolicyManifest freezes the equal-Bernoulli action mixture.
type BehaviorPolicyManifest struct {
	SchemaID                string `json:"schema_id"`
	Revision                int    `json:"revision"`
	PolicyID                string `json:"policy_id"`
	PolicyRevision          int    `json:"policy_revision"`
	ActionSchemaSHA256      string `json:"action_schema_sha256"`
	RandomizationPlanSHA256 string `json:"randomization_plan_sha256"`
	Rule                    string `json:"rule"`
}

func (manifest BehaviorPolicyManifest) validateCanonical() error {
	if manifest.SchemaID != "aura.adaptive.policy-manifest/behavior/v1" ||
		manifest.Revision != 1 ||
		manifest.PolicyID != "aura/equal-bernoulli-arm-mixture" ||
		manifest.PolicyRevision != 1 ||
		!validSHA256(manifest.ActionSchemaSHA256) ||
		!validSHA256(manifest.RandomizationPlanSHA256) ||
		manifest.Rule != "marginalize_deterministic_arm_actions" {
		return errors.New("behavior policy manifest is invalid")
	}
	return nil
}

// SHA256 returns the canonical behavior-policy content address.
func (manifest BehaviorPolicyManifest) SHA256() (string, error) {
	return canonicalArtifactSHA256(manifest)
}

// CanonicalEvaluator is the hash-critical endpoint evaluator identity.
type CanonicalEvaluator struct {
	Kind             string `json:"kind"`
	ID               string `json:"id"`
	Version          string `json:"version"`
	RubricID         string `json:"rubric_id"`
	CalibrationID    string `json:"calibration_id"`
	ProvenanceSHA256 string `json:"provenance_sha256"`
}

func (evaluator CanonicalEvaluator) valid() bool {
	return evaluator.Kind != "" &&
		evaluator.ID != "" &&
		evaluator.Version != "" &&
		evaluator.RubricID != "" &&
		evaluator.CalibrationID != "" &&
		validSHA256(evaluator.ProvenanceSHA256)
}

// BinaryRewardDefinition freezes one endpoint-specific binary reward.
type BinaryRewardDefinition struct {
	SchemaID      string             `json:"schema_id"`
	Revision      int                `json:"revision"`
	EndpointKind  string             `json:"endpoint_kind"`
	EndpointID    string             `json:"endpoint_id"`
	Evaluator     CanonicalEvaluator `json:"evaluator"`
	RewardType    string             `json:"reward_type"`
	SuccessValue  ExactRational      `json:"success_value"`
	FailureValue  ExactRational      `json:"failure_value"`
	MissingRuleID string             `json:"missing_rule_id"`
}

func (definition BinaryRewardDefinition) validateCanonical() error {
	if definition.SchemaID != "aura.adaptive.reward-definition/binary-endpoint/v1" ||
		definition.Revision != 1 ||
		!validEvidenceEndpoint(definition.EndpointKind) ||
		definition.EndpointID == "" ||
		!definition.Evaluator.valid() ||
		definition.RewardType != "binary" ||
		definition.SuccessValue.Cmp(MustExactRational(1, 1)) != 0 ||
		definition.FailureValue.Cmp(MustExactRational(0, 1)) != 0 ||
		definition.MissingRuleID == "" {
		return errors.New("binary reward definition is invalid")
	}
	return nil
}

// SHA256 returns the endpoint-specific reward content address.
func (definition BinaryRewardDefinition) SHA256() (string, error) {
	return canonicalArtifactSHA256(definition)
}

// ActionMeanPrediction is one ordered context-free endpoint prediction.
type ActionMeanPrediction struct {
	ActionID     string        `json:"action_id"`
	SupportCount uint64        `json:"support_count"`
	SuccessCount uint64        `json:"success_count"`
	QHat         ExactRational `json:"qhat"`
}

// PredictionVector binds the exact prediction bytes used by an endpoint report.
type PredictionVector struct {
	SchemaID               string                 `json:"schema_id"`
	Revision               int                    `json:"revision"`
	EndpointKind           string                 `json:"endpoint_kind"`
	ActionSchemaSHA256     string                 `json:"action_schema_sha256"`
	RewardDefinitionSHA256 string                 `json:"reward_definition_sha256"`
	Predictions            []ActionMeanPrediction `json:"predictions"`
}

func (vector PredictionVector) validateCanonical() error {
	if vector.SchemaID != "aura.adaptive.prediction-vector/action-mean/v1" ||
		vector.Revision != 1 ||
		!validEvidenceEndpoint(vector.EndpointKind) ||
		!validSHA256(vector.ActionSchemaSHA256) ||
		!validSHA256(vector.RewardDefinitionSHA256) ||
		len(vector.Predictions) == 0 {
		return errors.New("prediction vector is invalid")
	}
	for index, prediction := range vector.Predictions {
		if prediction.ActionID == "" ||
			index > 0 && vector.Predictions[index-1].ActionID >= prediction.ActionID ||
			prediction.SuccessCount > prediction.SupportCount ||
			prediction.QHat.Cmp(MustExactRational(0, 1)) < 0 ||
			prediction.QHat.Cmp(MustExactRational(1, 1)) > 0 {
			return errors.New("prediction vector row is invalid")
		}
		if prediction.SupportCount > 0 {
			expected := MustExactRational(
				int64(prediction.SuccessCount),
				prediction.SupportCount,
			)
			if prediction.QHat.Cmp(expected) != 0 {
				return errors.New("prediction qhat does not match its counts")
			}
		}
	}
	return nil
}

// SHA256 returns the endpoint-specific prediction content address.
func (vector PredictionVector) SHA256() (string, error) {
	return canonicalArtifactSHA256(vector)
}

func validEvidenceEndpoint(endpoint string) bool {
	return endpoint == string(EvidenceEndpointQuality) ||
		endpoint == string(EvidenceEndpointHarm)
}

// EvidenceEndpointKind prevents quality/harm report substitution.
type EvidenceEndpointKind string

// The two endpoint kinds are frozen: quality and harm must carry different report
// IDs and hashes, so one can never be substituted for the other.
const (
	EvidenceEndpointQuality EvidenceEndpointKind = "quality"
	EvidenceEndpointHarm    EvidenceEndpointKind = "harm"
)

// BinaryITTMember is one assigned member before endpoint-specific missing substitution.
type BinaryITTMember struct {
	Treated bool
	Outcome *bool
}

// BuildEndpointCounts applies the frozen worst-case missing rule.
func BuildEndpointCounts(
	analysisStratumID string,
	endpoint EvidenceEndpointKind,
	members []BinaryITTMember,
) (BinaryATEStratum, error) {
	if analysisStratumID == "" || len(members) == 0 {
		return BinaryATEStratum{}, errors.New("endpoint stratum is empty")
	}
	if endpoint != EvidenceEndpointQuality && endpoint != EvidenceEndpointHarm {
		return BinaryATEStratum{}, errors.New("endpoint kind is invalid")
	}
	counts := BinaryATEStratum{
		AnalysisStratumID: analysisStratumID,
		Population:        len(members),
	}
	for _, member := range members {
		success := false
		if member.Outcome != nil {
			success = *member.Outcome
		} else if endpoint == EvidenceEndpointQuality {
			success = !member.Treated
		} else {
			success = member.Treated
		}
		if member.Treated {
			counts.Treated++
			if success {
				counts.TreatedSuccesses++
			}
		} else if success {
			counts.ControlSuccesses++
		}
	}
	return counts, nil
}
