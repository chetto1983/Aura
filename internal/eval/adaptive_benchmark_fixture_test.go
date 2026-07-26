package eval

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/google/uuid"
)

func benchmarkSHA256(payload []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(payload))
}

type benchmarkOutcomeModelArtifact struct {
	SchemaID               string                          `json:"schema_id"`
	Revision               int                             `json:"revision"`
	EndpointKind           adaptive.EvidenceEndpointKind   `json:"endpoint_kind"`
	Evaluator              adaptive.CanonicalEvaluator     `json:"evaluator"`
	RewardDefinitionSHA256 string                          `json:"reward_definition_sha256"`
	TrainingCutoff         time.Time                       `json:"training_cutoff"`
	TrainingLedgerSHA256   string                          `json:"training_ledger_sha256"`
	ActionSchemaSHA256     string                          `json:"action_schema_sha256"`
	FeatureSchemaSHA256    string                          `json:"feature_schema_sha256"`
	AlgorithmID            string                          `json:"algorithm_id"`
	AlgorithmRevision      int                             `json:"algorithm_revision"`
	TargetPolicySHA256     string                          `json:"target_policy_sha256"`
	ClipMin                adaptive.ExactRational          `json:"clip_min"`
	ClipMax                adaptive.ExactRational          `json:"clip_max"`
	NoSupportFallback      adaptive.ExactRational          `json:"no_support_fallback"`
	PredictionSHA256       string                          `json:"prediction_sha256"`
	Predictions            []adaptive.ActionMeanPrediction `json:"predictions"`
}

type benchmarkOPEPlanArtifact struct {
	SchemaID               string                     `json:"schema_id"`
	Revision               int                        `json:"revision"`
	Estimand               string                     `json:"estimand"`
	SupportedActions       []string                   `json:"supported_actions"`
	TargetPolicyID         string                     `json:"target_policy_id"`
	TargetPolicyRevision   int                        `json:"target_policy_revision"`
	TargetPolicySHA256     string                     `json:"target_policy_sha256"`
	BehaviorPolicyID       string                     `json:"behavior_policy_id"`
	BehaviorPolicyRevision int                        `json:"behavior_policy_revision"`
	BehaviorPolicySHA256   string                     `json:"behavior_policy_sha256"`
	EndpointModels         []benchmarkOutcomeModelRef `json:"endpoint_models"`
	WeightClipping         string                     `json:"weight_clipping"`
	MinimumESS             adaptive.ExactRational     `json:"minimum_ess"`
	MinimumOverlap         adaptive.ExactRational     `json:"minimum_overlap"`
	Uncertainty            benchmarkOPEUncertainty    `json:"uncertainty"`
}

type benchmarkOutcomeModelRef struct {
	SchemaID         string                        `json:"schema_id"`
	Revision         int                           `json:"revision"`
	EndpointKind     adaptive.EvidenceEndpointKind `json:"endpoint_kind"`
	ArtifactID       string                        `json:"artifact_id"`
	ArtifactSHA256   string                        `json:"artifact_sha256"`
	PredictionSHA256 string                        `json:"prediction_sha256"`
}

type benchmarkOPEUncertainty struct {
	AlgorithmID      string                 `json:"algorithm_id"`
	Revision         int                    `json:"revision"`
	Confidence       adaptive.ExactRational `json:"confidence"`
	Replicates       int                    `json:"replicates"`
	Unit             string                 `json:"unit"`
	IntervalTarget   string                 `json:"interval_target"`
	SeedDomain       string                 `json:"seed_domain"`
	RoundingRevision string                 `json:"rounding_revision"`
}

func benchmarkAdmissionArtifacts(
	t *testing.T,
) []adaptive.EvidenceArtifactRegistration {
	t.Helper()
	interference := adaptive.InterferencePlanArtifact{
		SchemaID: "aura.adaptive.interference-plan/v1", Revision: 1,
		ClusterSchemaID: "aura.adaptive.interference-cluster/conversation/v1",
		ClusterRevision: 1, ClusterSchemaSHA256: strings.Repeat("a", 64),
		ClusterKeys:    []string{"owner_id", "evaluation_conversation_id"},
		CarryoverScope: "conversation", RandomizedFocalUnitsPerCluster: 1,
		WithinClusterRule:        "arbitrary_carryover_one_focal",
		BetweenClusterAssumption: "no_treatment_induced_cross_conversation_interference",
		SharedStateRule:          "read_only_or_versioned",
		FoldUnit:                 "interference_cluster", ResamplingUnit: "interference_cluster",
	}
	interferenceCanonical, err := interference.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	cutoffs := make([]time.Time, 5)
	for index := range cutoffs {
		cutoffs[index] = time.Date(2026, 7, 27, index+1, 0, 0, 0, time.UTC)
	}
	look, err := adaptive.NewEvidenceLookPlanArtifact(cutoffs)
	if err != nil {
		t.Fatal(err)
	}
	lookCanonical, err := look.CanonicalJSON()
	if err != nil {
		t.Fatal(err)
	}
	quality := benchmarkOutcomeModel(t, adaptive.EvidenceEndpointQuality, "b")
	harm := benchmarkOutcomeModel(t, adaptive.EvidenceEndpointHarm, "c")
	ope := benchmarkTypedRegistration(
		t,
		"ope_plan",
		"aura.adaptive.ope-plan/v1",
		benchmarkOPEPlanArtifact{
			SchemaID: "aura.adaptive.ope-plan/v1", Revision: 1,
			SupportedActions: []string{}, EndpointModels: []benchmarkOutcomeModelRef{},
			MinimumESS:     adaptive.MustExactRational(1, 1),
			MinimumOverlap: adaptive.MustExactRational(1, 2),
			Uncertainty: benchmarkOPEUncertainty{
				Confidence: adaptive.MustExactRational(19, 20),
			},
		},
	)
	return []adaptive.EvidenceArtifactRegistration{
		benchmarkTypedBytes(
			t, "interference_plan",
			"aura.adaptive.interference-plan/v1", interferenceCanonical,
		),
		benchmarkTypedBytes(
			t, "look_plan",
			"aura.adaptive.look-plan/utc-cutoff/v1", lookCanonical,
		),
		quality, harm, ope,
	}
}

func benchmarkOutcomeModel(
	t *testing.T,
	endpoint adaptive.EvidenceEndpointKind,
	hashDigit string,
) adaptive.EvidenceArtifactRegistration {
	t.Helper()
	return benchmarkTypedRegistration(
		t,
		string(endpoint)+"_outcome_model",
		"aura.adaptive.outcome-model/action-mean/v1",
		benchmarkOutcomeModelArtifact{
			SchemaID: "aura.adaptive.outcome-model/action-mean/v1", Revision: 1,
			EndpointKind: endpoint,
			Evaluator: adaptive.CanonicalEvaluator{
				Kind: "deterministic", ID: "fixture", Version: "v1",
				RubricID: "fixture", CalibrationID: "fixture",
				ProvenanceSHA256: strings.Repeat(hashDigit, 64),
			},
			RewardDefinitionSHA256: strings.Repeat("d", 64),
			TrainingCutoff:         time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC),
			TrainingLedgerSHA256:   strings.Repeat("e", 64),
			ActionSchemaSHA256:     strings.Repeat("f", 64),
			FeatureSchemaSHA256:    strings.Repeat("1", 64),
			AlgorithmID:            "fixture", AlgorithmRevision: 1,
			TargetPolicySHA256: strings.Repeat("2", 64),
			ClipMin:            adaptive.MustExactRational(0, 1),
			ClipMax:            adaptive.MustExactRational(1, 1),
			NoSupportFallback:  adaptive.MustExactRational(0, 1),
			PredictionSHA256:   strings.Repeat(hashDigit, 64),
			Predictions:        []adaptive.ActionMeanPrediction{},
		},
	)
}

func benchmarkTypedRegistration(
	t *testing.T,
	kind string,
	schemaID string,
	value any,
) adaptive.EvidenceArtifactRegistration {
	t.Helper()
	canonical, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return benchmarkTypedBytes(t, kind, schemaID, canonical)
}

func benchmarkTypedBytes(
	t *testing.T,
	kind string,
	schemaID string,
	canonical []byte,
) adaptive.EvidenceArtifactRegistration {
	t.Helper()
	registration := adaptive.EvidenceArtifactRegistration{
		Ref: adaptive.ChildArtifactRef{
			SchemaID: adaptive.ChildArtifactRefSchemaID, Revision: 1,
			Kind: kind, ArtifactSchemaID: schemaID, ArtifactRevision: 1,
			ArtifactID:     uuid.Must(uuid.NewV7()).String(),
			ArtifactSHA256: benchmarkSHA256(canonical),
		},
		Canonical: canonical,
	}
	if err := adaptive.ValidateAdmissionArtifactRegistration(registration); err != nil {
		t.Fatalf("invalid typed fixture %s: %v", kind, err)
	}
	return registration
}
