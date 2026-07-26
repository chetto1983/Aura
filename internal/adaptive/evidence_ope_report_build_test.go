package adaptive

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBuildUnavailableOPEReports_ComputesSelectedIntentionDiagnostics(t *testing.T) {
	cohort, look, ledger, qualityModel, harmModel, opePlan := opeReportFixture(t)

	reports, err := buildUnavailableOPEReports(
		cohort, look, ledger, qualityModel, harmModel, opePlan,
	)
	if err != nil {
		t.Fatalf("buildUnavailableOPEReports() error = %v", err)
	}
	for name, report := range map[string]EndpointOPEReport{
		"quality": reports.Quality,
		"harm":    reports.Harm,
	} {
		if !report.Eligible || len(report.IneligibleReasons) != 0 {
			t.Fatalf("%s report eligibility = %v, reasons = %v", name, report.Eligible, report.IneligibleReasons)
		}
		if report.IneligibleRowCount != 0 || report.DR == nil ||
			report.SNIPS == nil || report.ESS == nil ||
			report.ObservedMinimumOverlap == nil ||
			report.DRLower == nil || report.DRUpper == nil {
			t.Fatalf("%s report lacks production diagnostics: %#v", name, report)
		}
	}
	if got := *reports.Quality.DR; got != 1 {
		t.Fatalf("quality DR = %v, want 1", got)
	}
	if got := *reports.Quality.SNIPS; got != 1 {
		t.Fatalf("quality SNIPS = %v, want 1", got)
	}
	if got := *reports.Quality.ESS; got != 1 {
		t.Fatalf("quality ESS = %v, want 1", got)
	}
	if got := *reports.Harm.DR; got != 0 {
		t.Fatalf("harm DR = %v, want 0", got)
	}
	if reports.Quality.OutcomeModelSHA256 == reports.Harm.OutcomeModelSHA256 ||
		reports.Quality.PredictionSHA256 == reports.Harm.PredictionSHA256 ||
		reports.QualitySHA256 == reports.HarmSHA256 {
		t.Fatal("quality and harm OPE evidence is not endpoint-distinct")
	}
	if ledger.Members[0].Delivery != nil || ledger.Members[0].OPEEligible {
		t.Fatal("fixture no longer proves selected-intention eligibility with unknown delivery")
	}
}

func TestBuildUnavailableOPEReports_ComputesRowSpecificTargetPolicy(t *testing.T) {
	cohort, look, ledger, qualityModel, harmModel, opePlan := opeReportFixture(t)
	ledger.Members[1].Assignment.RecommendedActionID = "direct"
	ledger.Members[1].Assignment.IntendedActionID = "direct"
	ledger.Members[1].Assignment.ActionProbabilities = []ActionProbability{
		{ActionID: "deep", Probability: 0},
		{ActionID: "direct", Probability: 0.5},
		{ActionID: "static", Probability: 0.5},
	}

	reports, err := buildUnavailableOPEReports(
		cohort, look, ledger, qualityModel, harmModel, opePlan,
	)
	if err != nil {
		t.Fatalf("buildUnavailableOPEReports() error = %v", err)
	}
	for name, report := range map[string]EndpointOPEReport{
		"quality": reports.Quality,
		"harm":    reports.Harm,
	} {
		if !report.Eligible || len(report.IneligibleReasons) != 0 {
			t.Fatalf("%s report eligibility = %v, reasons = %v", name, report.Eligible, report.IneligibleReasons)
		}
		if report.DR == nil || report.SNIPS == nil || report.ESS == nil {
			t.Fatalf("%s report lacks row-specific diagnostics", name)
		}
	}
}

func TestEvidenceArtifactRegistrationRejectsNoncanonicalOPEArtifacts(
	t *testing.T,
) {
	_, _, _, qualityModel, harmModel, opePlan := opeReportFixture(t)
	for _, registration := range []EvidenceArtifactRegistration{
		qualityModel, harmModel, opePlan,
	} {
		registration.Canonical = append(registration.Canonical, '\n')
		registration.Ref.ArtifactSHA256 = sha256Hex(registration.Canonical)
		if err := registration.validate(registration.Ref.Kind); err == nil {
			t.Fatalf("%s registration accepted noncanonical JSON", registration.Ref.Kind)
		}
	}
}

type opeReportTestModel struct {
	SchemaID               string                 `json:"schema_id"`
	Revision               int                    `json:"revision"`
	EndpointKind           EvidenceEndpointKind   `json:"endpoint_kind"`
	Evaluator              CanonicalEvaluator     `json:"evaluator"`
	RewardDefinitionSHA256 string                 `json:"reward_definition_sha256"`
	TrainingCutoff         time.Time              `json:"training_cutoff"`
	TrainingLedgerSHA256   string                 `json:"training_ledger_sha256"`
	ActionSchemaSHA256     string                 `json:"action_schema_sha256"`
	FeatureSchemaSHA256    string                 `json:"feature_schema_sha256"`
	AlgorithmID            string                 `json:"algorithm_id"`
	AlgorithmRevision      int                    `json:"algorithm_revision"`
	TargetPolicySHA256     string                 `json:"target_policy_sha256"`
	ClipMin                ExactRational          `json:"clip_min"`
	ClipMax                ExactRational          `json:"clip_max"`
	NoSupportFallback      ExactRational          `json:"no_support_fallback"`
	PredictionSHA256       string                 `json:"prediction_sha256"`
	Predictions            []ActionMeanPrediction `json:"predictions"`
}

type opeReportTestModelRef struct {
	SchemaID         string               `json:"schema_id"`
	Revision         int                  `json:"revision"`
	EndpointKind     EvidenceEndpointKind `json:"endpoint_kind"`
	ArtifactID       string               `json:"artifact_id"`
	ArtifactSHA256   string               `json:"artifact_sha256"`
	PredictionSHA256 string               `json:"prediction_sha256"`
}

type opeReportTestUncertainty struct {
	AlgorithmID      string        `json:"algorithm_id"`
	Revision         int           `json:"revision"`
	Confidence       ExactRational `json:"confidence"`
	Replicates       int           `json:"replicates"`
	Unit             string        `json:"unit"`
	IntervalTarget   string        `json:"interval_target"`
	SeedDomain       string        `json:"seed_domain"`
	RoundingRevision string        `json:"rounding_revision"`
}

type opeReportTestPlan struct {
	SchemaID               string                   `json:"schema_id"`
	Revision               int                      `json:"revision"`
	Estimand               string                   `json:"estimand"`
	SupportedActions       []string                 `json:"supported_actions"`
	TargetPolicyID         string                   `json:"target_policy_id"`
	TargetPolicyRevision   int                      `json:"target_policy_revision"`
	TargetPolicySHA256     string                   `json:"target_policy_sha256"`
	BehaviorPolicyID       string                   `json:"behavior_policy_id"`
	BehaviorPolicyRevision int                      `json:"behavior_policy_revision"`
	BehaviorPolicySHA256   string                   `json:"behavior_policy_sha256"`
	EndpointModels         []opeReportTestModelRef  `json:"endpoint_models"`
	WeightClipping         string                   `json:"weight_clipping"`
	MinimumESS             ExactRational            `json:"minimum_ess"`
	MinimumOverlap         ExactRational            `json:"minimum_overlap"`
	Uncertainty            opeReportTestUncertainty `json:"uncertainty"`
}

func opeReportFixture(
	t *testing.T,
) (*FocalCohort, EvidenceLook, CohortLedger, EvidenceArtifactRegistration, EvidenceArtifactRegistration, EvidenceArtifactRegistration) {
	t.Helper()
	base, snapshot := randomizedFocalCohortUnitFixture(t)
	actions := cohortActionIDs(base.Actions())
	actionSchema := ActionSchemaArtifact{
		SchemaID: "aura.adaptive.action-schema/v1", Revision: 1, Actions: actions,
	}
	actionHash, err := actionSchema.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	target := TargetPolicyManifest{
		SchemaID: "aura.adaptive.policy-manifest/target/v1", Revision: 1,
		PolicyID: "aura/deterministic-challenger", PolicyRevision: 1,
		ActionSchemaSHA256: actionHash, Rule: "snapshot_recommended_action",
	}
	targetHash, err := target.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	behavior := BehaviorPolicyManifest{
		SchemaID: "aura.adaptive.policy-manifest/behavior/v1", Revision: 1,
		PolicyID: "aura/equal-bernoulli-arm-mixture", PolicyRevision: 1,
		ActionSchemaSHA256:      actionHash,
		RandomizationPlanSHA256: base.v2.document.RandomizationPlanRef.ArtifactSHA256,
		Rule:                    "marginalize_deterministic_arm_actions",
	}
	behaviorHash, err := behavior.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	qualityModel := opeReportModelRegistration(
		t, base, EvidenceEndpointQuality, actionHash, targetHash,
		[]ActionMeanPrediction{
			{ActionID: "deep", SupportCount: 5, SuccessCount: 4, QHat: MustExactRational(4, 5)},
			{ActionID: "direct", SupportCount: 4, SuccessCount: 2, QHat: MustExactRational(1, 2)},
			{ActionID: "static", SupportCount: 5, SuccessCount: 2, QHat: MustExactRational(2, 5)},
		},
	)
	harmModel := opeReportModelRegistration(
		t, base, EvidenceEndpointHarm, actionHash, targetHash,
		[]ActionMeanPrediction{
			{ActionID: "deep", SupportCount: 5, SuccessCount: 1, QHat: MustExactRational(1, 5)},
			{ActionID: "direct", SupportCount: 4, SuccessCount: 0, QHat: MustExactRational(0, 1)},
			{ActionID: "static", SupportCount: 5, SuccessCount: 1, QHat: MustExactRational(1, 5)},
		},
	)
	qualityRef := opeReportModelRef(t, qualityModel)
	harmRef := opeReportModelRef(t, harmModel)
	plan := opeReportTestPlan{
		SchemaID: "aura.adaptive.ope-plan/v1", Revision: 1,
		Estimand: "selected_intention", SupportedActions: actions,
		TargetPolicyID: "aura/deterministic-challenger", TargetPolicyRevision: 1,
		TargetPolicySHA256: targetHash,
		BehaviorPolicyID:   "aura/equal-bernoulli-arm-mixture", BehaviorPolicyRevision: 1,
		BehaviorPolicySHA256: behaviorHash,
		EndpointModels:       []opeReportTestModelRef{qualityRef, harmRef},
		WeightClipping:       "none", MinimumESS: MustExactRational(1, 1),
		MinimumOverlap: MustExactRational(1, 2),
		Uncertainty: opeReportTestUncertainty{
			AlgorithmID: "cluster-percentile-bootstrap-sha256-v1", Revision: 1,
			Confidence: MustExactRational(19, 20), Replicates: 10_000,
			Unit: "interference_cluster", IntervalTarget: "dr",
			SeedDomain:       "aura/ope-cluster-bootstrap-sha256-v1",
			RoundingRevision: "neumaier-binary64-no-fma-outward-8dp-v1",
		},
	}
	opePlan := opeReportRegistration(t, "ope_plan", "aura.adaptive.ope-plan/v1", plan, 0x93)
	refs := validCohortV2ChildRefs()
	refs.QualityOutcomeModel = qualityModel.Ref
	refs.HarmOutcomeModel = harmModel.Ref
	refs.OPEPlan = opePlan.Ref
	cohort, err := NewRandomizedFocalCohort(base.spec, snapshot, refs)
	if err != nil {
		t.Fatal(err)
	}
	look := EvidenceLook{
		Number: 1, Cutoff: cohort.Cutoff(), Alpha: MustExactRational(1, 200),
		CumulativeAlpha: MustExactRational(1, 200), PredecessorRule: "none",
	}
	ledger := opeReportLedger(cohort)
	return cohort, look, ledger, qualityModel, harmModel, opePlan
}

func opeReportModelRegistration(
	t *testing.T,
	cohort *FocalCohort,
	endpoint EvidenceEndpointKind,
	actionHash string,
	targetHash string,
	predictions []ActionMeanPrediction,
) EvidenceArtifactRegistration {
	t.Helper()
	frozen := cohort.PrimaryQualityEndpoint()
	missingRule := "quality-missing-challenger-0-baseline-1-v1"
	kind := "quality_outcome_model"
	seed := byte(0x71)
	if endpoint == EvidenceEndpointHarm {
		frozen = cohort.PrimaryHarmEndpoint()
		missingRule = "harm-missing-challenger-1-baseline-0-v1"
		kind = "harm_outcome_model"
		seed = 0x72
	}
	reward := BinaryRewardDefinition{
		SchemaID: "aura.adaptive.reward-definition/binary-endpoint/v1", Revision: 1,
		EndpointKind: string(endpoint), EndpointID: frozen.ID,
		Evaluator: canonicalEvaluator(frozen.Evaluator), RewardType: "binary",
		SuccessValue: MustExactRational(1, 1), FailureValue: MustExactRational(0, 1),
		MissingRuleID: missingRule,
	}
	rewardHash, err := reward.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	prediction := PredictionVector{
		SchemaID: "aura.adaptive.prediction-vector/action-mean/v1", Revision: 1,
		EndpointKind: string(endpoint), ActionSchemaSHA256: actionHash,
		RewardDefinitionSHA256: rewardHash, Predictions: predictions,
	}
	predictionHash, err := prediction.SHA256()
	if err != nil {
		t.Fatal(err)
	}
	emptyHash, err := (EmptyFeatureSchemaArtifact{
		SchemaID: "aura.adaptive.feature-schema/empty/v1", Revision: 1,
		Features: []struct{}{},
	}).SHA256()
	if err != nil {
		t.Fatal(err)
	}
	model := opeReportTestModel{
		SchemaID: "aura.adaptive.outcome-model/action-mean/v1", Revision: 1,
		EndpointKind: endpoint, Evaluator: canonicalEvaluator(frozen.Evaluator),
		RewardDefinitionSHA256: rewardHash,
		TrainingCutoff:         cohort.Cutoff().Add(-2 * time.Hour),
		TrainingLedgerSHA256:   repeatedSHA("8"), ActionSchemaSHA256: actionHash,
		FeatureSchemaSHA256: emptyHash,
		AlgorithmID:         "aura/endpoint-action-mean", AlgorithmRevision: 1,
		TargetPolicySHA256: targetHash,
		ClipMin:            MustExactRational(0, 1), ClipMax: MustExactRational(1, 1),
		NoSupportFallback: MustExactRational(0, 1),
		PredictionSHA256:  predictionHash, Predictions: predictions,
	}
	return opeReportRegistration(t, kind, "aura.adaptive.outcome-model/action-mean/v1", model, seed)
}

func opeReportModelRef(t *testing.T, registration EvidenceArtifactRegistration) opeReportTestModelRef {
	t.Helper()
	var model opeReportTestModel
	if err := json.Unmarshal(registration.Canonical, &model); err != nil {
		t.Fatal(err)
	}
	return opeReportTestModelRef{
		SchemaID: "aura.adaptive.outcome-model-ref/v1", Revision: 1,
		EndpointKind: model.EndpointKind, ArtifactID: registration.Ref.ArtifactID,
		ArtifactSHA256:   registration.Ref.ArtifactSHA256,
		PredictionSHA256: model.PredictionSHA256,
	}
}

func opeReportRegistration(
	t *testing.T,
	kind string,
	schema string,
	value any,
	seed byte,
) EvidenceArtifactRegistration {
	t.Helper()
	canonical, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return EvidenceArtifactRegistration{
		Ref: ChildArtifactRef{
			SchemaID: ChildArtifactRefSchemaID, Revision: 1, Kind: kind,
			ArtifactSchemaID: schema, ArtifactRevision: 1,
			ArtifactID: uuid.NewSHA1(
				uuid.MustParse("f6885667-9675-45df-9ef2-94ea92ad2c82"),
				[]byte{seed},
			).String(),
			ArtifactSHA256: sha256Hex(canonical),
		},
		Canonical: canonical,
	}
}

func opeReportLedger(cohort *FocalCohort) CohortLedger {
	claimedAt := cohort.Cutoff().Add(-time.Hour)
	probabilities := []ActionProbability{
		{ActionID: "deep", Probability: 0.5},
		{ActionID: "direct", Probability: 0},
		{ActionID: "static", Probability: 0.5},
	}
	member := func(index int, intended string, quality, harm bool) CohortMember {
		claimID := uuid.NewSHA1(uuid.NameSpaceOID, []byte{byte(index), 1})
		requestID := uuid.NewSHA1(uuid.NameSpaceOID, []byte{byte(index), 2})
		assignmentID := uuid.NewSHA1(uuid.NameSpaceOID, []byte{byte(index), 3})
		qualityValue, harmValue := 0.0, 0.0
		if quality {
			qualityValue = 1
		}
		if harm {
			harmValue = 1
		}
		return CohortMember{
			Claim: FocalClaim{
				ID: claimID, OwnerID: cohort.Scope().OwnerID, CohortID: cohort.ID(),
				RequestID: requestID, AssignmentID: assignmentID, ClaimedAt: claimedAt,
				AnalysisStratumSchemaSHA256:     cohort.v2.randomizationPlan.AnalysisStratumSchemaSHA256,
				AnalysisStratumID:               repeatedSHA(string(rune('a' + index))),
				InterferenceClusterSchemaSHA256: repeatedSHA("c"),
				InterferenceClusterID:           repeatedSHA(string(rune('d' + index))),
			},
			Assignment: Assignment{
				AssignmentID: assignmentID, RequestID: requestID,
				ChampionActionID: "static", RecommendedActionID: "deep",
				IntendedActionID: intended, EligibleActions: []string{"deep", "direct", "static"},
				ActionProbabilities: slices.Clone(probabilities),
			},
			Quality: &CohortEffectiveObservation{
				Status: OutcomeObserved, Measures: OutcomeMeasures{Quality: &qualityValue},
			},
			Harm: &CohortEffectiveObservation{
				Status: OutcomeObserved, Measures: OutcomeMeasures{Harm: &harmValue},
			},
			Observed: true,
		}
	}
	members := []CohortMember{
		member(0, "static", false, false),
		member(1, "deep", true, false),
	}
	return CohortLedger{
		CohortID: cohort.ID(), CohortSHA256: cohort.SHA256(),
		State: CohortLedgerClosed, Members: members, SHA256: repeatedSHA("9"),
	}
}
