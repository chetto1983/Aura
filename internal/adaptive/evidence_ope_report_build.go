package adaptive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"time"
)

type storedOutcomeModelRef struct {
	SchemaID         string               `json:"schema_id"`
	Revision         int                  `json:"revision"`
	EndpointKind     EvidenceEndpointKind `json:"endpoint_kind"`
	ArtifactID       string               `json:"artifact_id"`
	ArtifactSHA256   string               `json:"artifact_sha256"`
	PredictionSHA256 string               `json:"prediction_sha256"`
}
type storedOPEUncertainty struct {
	AlgorithmID      string        `json:"algorithm_id"`
	Revision         int           `json:"revision"`
	Confidence       ExactRational `json:"confidence"`
	Replicates       int           `json:"replicates"`
	Unit             string        `json:"unit"`
	IntervalTarget   string        `json:"interval_target"`
	SeedDomain       string        `json:"seed_domain"`
	RoundingRevision string        `json:"rounding_revision"`
}
type storedOPEPlanMetadata struct {
	SchemaID               string                  `json:"schema_id"`
	Revision               int                     `json:"revision"`
	Estimand               string                  `json:"estimand"`
	SupportedActions       []string                `json:"supported_actions"`
	TargetPolicyID         string                  `json:"target_policy_id"`
	TargetPolicyRevision   int                     `json:"target_policy_revision"`
	TargetPolicySHA256     string                  `json:"target_policy_sha256"`
	BehaviorPolicyID       string                  `json:"behavior_policy_id"`
	BehaviorPolicyRevision int                     `json:"behavior_policy_revision"`
	BehaviorPolicySHA256   string                  `json:"behavior_policy_sha256"`
	EndpointModels         []storedOutcomeModelRef `json:"endpoint_models"`
	WeightClipping         string                  `json:"weight_clipping"`
	MinimumESS             ExactRational           `json:"minimum_ess"`
	MinimumOverlap         ExactRational           `json:"minimum_overlap"`
	Uncertainty            storedOPEUncertainty    `json:"uncertainty"`
}
type storedOutcomeModelMetadata struct {
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
type opeReportPair struct {
	Quality, Harm             EndpointOPEReport
	QualitySHA256, HarmSHA256 string
}

func buildUnavailableOPEReports(cohort *FocalCohort, look EvidenceLook, ledger CohortLedger, qualityModel, harmModel, opePlan EvidenceArtifactRegistration) (opeReportPair, error) {
	if cohort == nil || !cohort.randomizationEligible() ||
		ledger.State != CohortLedgerClosed || len(ledger.Members) == 0 ||
		ledger.CohortID != cohort.ID() || ledger.CohortSHA256 != cohort.SHA256() ||
		!validSHA256(ledger.SHA256) {
		return opeReportPair{}, errors.New("adaptive OPE requires a valid closed cohort ledger")
	}
	if opePlan.Ref != cohort.v2.document.OPEPlanRef {
		return opeReportPair{}, errors.New("stored adaptive OPE plan differs from cohort")
	}
	var plan storedOPEPlanMetadata
	if err := decodeCanonicalRegistration(opePlan, "ope_plan", &plan); err != nil {
		return opeReportPair{}, err
	}
	qualityRef, harmRef, err := endpointModelRefs(plan)
	if err != nil {
		return opeReportPair{}, err
	}
	if qualityRef.ArtifactID != qualityModel.Ref.ArtifactID ||
		qualityRef.ArtifactSHA256 != qualityModel.Ref.ArtifactSHA256 ||
		harmRef.ArtifactID != harmModel.Ref.ArtifactID ||
		harmRef.ArtifactSHA256 != harmModel.Ref.ArtifactSHA256 ||
		qualityModel.Ref != cohort.v2.document.QualityOutcomeModelRef ||
		harmModel.Ref != cohort.v2.document.HarmOutcomeModelRef {
		return opeReportPair{}, errors.New("stored adaptive endpoint model differs from OPE plan")
	}
	clusterSchema := ledger.Members[0].Claim.InterferenceClusterSchemaSHA256
	analysisSchema := cohort.v2.randomizationPlan.AnalysisStratumSchemaSHA256
	if !validSHA256(clusterSchema) {
		return opeReportPair{}, errors.New("adaptive OPE cluster schema is invalid")
	}
	for _, member := range ledger.Members {
		if member.Claim.InterferenceClusterSchemaSHA256 != clusterSchema ||
			member.Claim.AnalysisStratumSchemaSHA256 != analysisSchema {
			return opeReportPair{}, errors.New("adaptive OPE member schema differs within look")
		}
	}
	quality := newEndpointOPEReport(
		cohort, look, ledger, plan, clusterSchema,
		EvidenceEndpointQuality, qualityRef, qualityModel,
	)
	harm := newEndpointOPEReport(
		cohort, look, ledger, plan, clusterSchema,
		EvidenceEndpointHarm, harmRef, harmModel,
	)
	if reason := validateStoredOPEPlan(cohort, plan); reason != "" {
		markOPEReportIneligible(&quality, reason, uint64(len(ledger.Members)))
		markOPEReportIneligible(&harm, reason, uint64(len(ledger.Members)))
	} else {
		computeEndpointOPE(&quality, cohort, ledger, plan, qualityModel)
		computeEndpointOPE(&harm, cohort, ledger, plan, harmModel)
	}
	return finalizeOPEReports(quality, harm)
}
func decodeCanonicalRegistration(registration EvidenceArtifactRegistration, kind string, target any) error {
	if err := registration.validate(kind); err != nil {
		return err
	}
	if err := decodeStrictEvidence(registration.Canonical, target); err != nil {
		return errors.New("stored adaptive OPE artifact is not strict JSON")
	}
	canonical, err := json.Marshal(target)
	if err != nil || !bytes.Equal(canonical, registration.Canonical) {
		return errors.New("stored adaptive OPE artifact is noncanonical")
	}
	return nil
}
func endpointModelRefs(plan storedOPEPlanMetadata) (storedOutcomeModelRef, storedOutcomeModelRef, error) {
	if len(plan.EndpointModels) != 2 {
		return storedOutcomeModelRef{}, storedOutcomeModelRef{},
			errors.New("adaptive OPE plan lacks endpoint model references")
	}
	quality, harm := plan.EndpointModels[0], plan.EndpointModels[1]
	for _, ref := range plan.EndpointModels {
		if ref.SchemaID != "aura.adaptive.outcome-model-ref/v1" ||
			ref.Revision != 1 || !canonicalUUID(ref.ArtifactID) ||
			!validSHA256(ref.ArtifactSHA256) || !validSHA256(ref.PredictionSHA256) {
			return storedOutcomeModelRef{}, storedOutcomeModelRef{},
				errors.New("adaptive OPE endpoint model reference is invalid")
		}
	}
	if quality.EndpointKind != EvidenceEndpointQuality ||
		harm.EndpointKind != EvidenceEndpointHarm ||
		quality.ArtifactID == harm.ArtifactID ||
		quality.ArtifactSHA256 == harm.ArtifactSHA256 ||
		quality.PredictionSHA256 == harm.PredictionSHA256 {
		return storedOutcomeModelRef{}, storedOutcomeModelRef{},
			errors.New("adaptive OPE endpoint model references are not distinct")
	}
	return quality, harm, nil
}
func validateStoredOPEPlan(cohort *FocalCohort, plan storedOPEPlanMetadata) string {
	if plan.SchemaID != "aura.adaptive.ope-plan/v1" || plan.Revision != 1 ||
		plan.Estimand != "selected_intention" ||
		!slices.Equal(plan.SupportedActions, cohortActionIDs(cohort.Actions())) ||
		plan.TargetPolicyID != "aura/deterministic-challenger" ||
		plan.TargetPolicyRevision != 1 ||
		plan.BehaviorPolicyID != "aura/equal-bernoulli-arm-mixture" ||
		plan.BehaviorPolicyRevision != 1 || plan.WeightClipping != "none" ||
		plan.MinimumESS.Cmp(MustExactRational(0, 1)) <= 0 ||
		plan.MinimumOverlap.Cmp(MustExactRational(1, 2)) != 0 ||
		plan.Uncertainty.AlgorithmID != "cluster-percentile-bootstrap-sha256-v1" ||
		plan.Uncertainty.Revision != 1 ||
		plan.Uncertainty.Confidence.Cmp(MustExactRational(19, 20)) != 0 ||
		plan.Uncertainty.Replicates != 10_000 ||
		plan.Uncertainty.Unit != "interference_cluster" ||
		plan.Uncertainty.IntervalTarget != "dr" ||
		plan.Uncertainty.SeedDomain != "aura/ope-cluster-bootstrap-sha256-v1" ||
		plan.Uncertainty.RoundingRevision != "neumaier-binary64-no-fma-outward-8dp-v1" {
		return "ope_plan_invalid"
	}
	action := ActionSchemaArtifact{
		SchemaID: "aura.adaptive.action-schema/v1", Revision: 1,
		Actions: slices.Clone(plan.SupportedActions),
	}
	actionHash, err := action.SHA256()
	if err != nil {
		return "action_schema_invalid"
	}
	targetHash, err := (TargetPolicyManifest{
		SchemaID: "aura.adaptive.policy-manifest/target/v1", Revision: 1,
		PolicyID: plan.TargetPolicyID, PolicyRevision: plan.TargetPolicyRevision,
		ActionSchemaSHA256: actionHash, Rule: "snapshot_recommended_action",
	}).SHA256()
	if err != nil || targetHash != plan.TargetPolicySHA256 {
		return "target_policy_hash_mismatch"
	}
	behaviorHash, err := (BehaviorPolicyManifest{
		SchemaID: "aura.adaptive.policy-manifest/behavior/v1", Revision: 1,
		PolicyID: plan.BehaviorPolicyID, PolicyRevision: plan.BehaviorPolicyRevision,
		ActionSchemaSHA256:      actionHash,
		RandomizationPlanSHA256: cohort.v2.document.RandomizationPlanRef.ArtifactSHA256,
		Rule:                    "marginalize_deterministic_arm_actions",
	}).SHA256()
	if err != nil || behaviorHash != plan.BehaviorPolicySHA256 {
		return "behavior_policy_hash_mismatch"
	}
	return ""
}
func newEndpointOPEReport(cohort *FocalCohort, look EvidenceLook, ledger CohortLedger, plan storedOPEPlanMetadata, clusterSchema string, endpoint EvidenceEndpointKind, modelRef storedOutcomeModelRef, model EvidenceArtifactRegistration) EndpointOPEReport {
	frozen := cohort.PrimaryQualityEndpoint()
	if endpoint == EvidenceEndpointHarm {
		frozen = cohort.PrimaryHarmEndpoint()
	}
	scope := cohort.Scope()
	return EndpointOPEReport{
		SchemaID: "aura.adaptive.report/ope/v1", Revision: 1,
		ReportID: reportID(
			cohort.ID(), look.Number, string(endpoint)+"-ope", ledger.SHA256,
		),
		OwnerID: scope.OwnerID.String(), CohortID: cohort.ID().String(),
		LookNumber: look.Number, LookCutoff: look.Cutoff,
		EndpointKind: endpoint, EndpointID: frozen.ID,
		Evaluator:                       canonicalEvaluator(frozen.Evaluator),
		ClosedLedgerSHA256:              ledger.SHA256,
		AnalysisStratumSchemaSHA256:     cohort.v2.randomizationPlan.AnalysisStratumSchemaSHA256,
		InterferenceClusterSchemaSHA256: clusterSchema,
		OPEPlanSHA256:                   cohort.v2.document.OPEPlanRef.ArtifactSHA256,
		TargetPolicySHA256:              plan.TargetPolicySHA256,
		BehaviorPolicySHA256:            plan.BehaviorPolicySHA256,
		OutcomeModelID:                  model.Ref.ArtifactID,
		OutcomeModelSHA256:              model.Ref.ArtifactSHA256,
		PredictionSHA256:                modelRef.PredictionSHA256,
		IneligibleReasons:               []string{}, RowCount: uint64(len(ledger.Members)),
		IneligibleReasonCounts: []OPEIneligibleReasonCount{},
		MinimumESS:             plan.MinimumESS, MinimumOverlap: plan.MinimumOverlap,
		UncertaintyAlgorithmID: plan.Uncertainty.AlgorithmID,
		UncertaintyRevision:    plan.Uncertainty.Revision,
		Confidence:             plan.Uncertainty.Confidence,
	}
}
func computeEndpointOPE(report *EndpointOPEReport, cohort *FocalCohort, ledger CohortLedger, plan storedOPEPlanMetadata, registration EvidenceArtifactRegistration) {
	var model storedOutcomeModelMetadata
	if err := decodeCanonicalRegistration(
		registration, registration.Ref.Kind, &model,
	); err != nil {
		markOPEReportIneligible(report, "outcome_model_noncanonical", report.RowCount)
		return
	}
	artifacts, reason := endpointOPEArtifacts(
		cohort, ledger, plan, registration, model, report.EndpointKind,
	)
	if reason != "" {
		markOPEReportIneligible(report, reason, report.RowCount)
		return
	}
	actionHash, _ := artifacts.ActionSchema.SHA256()
	rows, overlap, failures := selectedIntentionRows(
		cohort, ledger.Members, plan.SupportedActions, report.EndpointKind,
	)
	if len(failures) != 0 {
		applyOPERowFailures(report, failures)
		return
	}
	report.ObservedMinimumOverlap = &overlap
	input := SelectedIntentionOPEInput{
		Plan: SelectedIntentionOPEPlan{
			EndpointKind:           report.EndpointKind,
			ActionSchemaSHA256:     actionHash,
			RewardDefinitionSHA256: model.RewardDefinitionSHA256,
			TargetPolicySHA256:     plan.TargetPolicySHA256,
			BehaviorPolicySHA256:   plan.BehaviorPolicySHA256,
			OutcomeModelSHA256:     registration.Ref.ArtifactSHA256,
			PredictionSHA256:       model.PredictionSHA256,
			MinimumESS:             plan.MinimumESS, MinimumOverlap: plan.MinimumOverlap,
		},
		Artifacts: artifacts, Rows: rows,
	}
	result, err := EvaluateSelectedIntentionOPE(input)
	if err != nil {
		markOPEReportIneligible(report, "estimator_unavailable", report.RowCount)
		return
	}
	report.DR, report.SNIPS, report.ESS = &result.DR, &result.SNIPS, &result.ESS
	seed, err := opeBootstrapSeed(
		cohort.ID().String(), report.LookNumber, report.EndpointKind,
		ledger.SHA256, plan.TargetPolicySHA256, registration.Ref.ArtifactSHA256,
	)
	if err != nil {
		markOPEReportIneligible(report, "uncertainty_unavailable", 1)
		return
	}
	interval, err := ClusterBootstrapDR(
		context.Background(), seed, input, DefaultOPEUncertaintyCaps(),
	)
	if err != nil {
		markOPEReportIneligible(report, "uncertainty_unavailable", 1)
		return
	}
	lower, upper := outwardEight(interval.Lower, false), outwardEight(interval.Upper, true)
	report.DRLower, report.DRUpper = &lower, &upper
	report.Eligible = true
}
func endpointOPEArtifacts(cohort *FocalCohort, ledger CohortLedger, plan storedOPEPlanMetadata, registration EvidenceArtifactRegistration, model storedOutcomeModelMetadata, endpoint EvidenceEndpointKind) (SelectedIntentionOPEArtifacts, string) {
	frozen := cohort.PrimaryQualityEndpoint()
	missingRule := "quality-missing-challenger-0-baseline-1-v1"
	if endpoint == EvidenceEndpointHarm {
		frozen = cohort.PrimaryHarmEndpoint()
		missingRule = "harm-missing-challenger-1-baseline-0-v1"
	}
	action := ActionSchemaArtifact{
		SchemaID: "aura.adaptive.action-schema/v1", Revision: 1,
		Actions: slices.Clone(plan.SupportedActions),
	}
	actionHash, err := action.SHA256()
	if err != nil {
		return SelectedIntentionOPEArtifacts{}, "action_schema_invalid"
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
		return SelectedIntentionOPEArtifacts{}, "reward_definition_invalid"
	}
	emptyHash, err := (EmptyFeatureSchemaArtifact{
		SchemaID: "aura.adaptive.feature-schema/empty/v1", Revision: 1,
		Features: []struct{}{},
	}).SHA256()
	if err != nil {
		return SelectedIntentionOPEArtifacts{}, "feature_schema_invalid"
	}
	target := TargetPolicyManifest{
		SchemaID: "aura.adaptive.policy-manifest/target/v1", Revision: 1,
		PolicyID: plan.TargetPolicyID, PolicyRevision: plan.TargetPolicyRevision,
		ActionSchemaSHA256: actionHash, Rule: "snapshot_recommended_action",
	}
	behavior := BehaviorPolicyManifest{
		SchemaID: "aura.adaptive.policy-manifest/behavior/v1", Revision: 1,
		PolicyID: plan.BehaviorPolicyID, PolicyRevision: plan.BehaviorPolicyRevision,
		ActionSchemaSHA256:      actionHash,
		RandomizationPlanSHA256: cohort.v2.document.RandomizationPlanRef.ArtifactSHA256,
		Rule:                    "marginalize_deterministic_arm_actions",
	}
	prediction := PredictionVector{
		SchemaID: "aura.adaptive.prediction-vector/action-mean/v1", Revision: 1,
		EndpointKind: string(endpoint), ActionSchemaSHA256: actionHash,
		RewardDefinitionSHA256: rewardHash, Predictions: slices.Clone(model.Predictions),
	}
	predictionHash, predictionErr := prediction.SHA256()
	trainingPredatesMembers := true
	for _, member := range ledger.Members {
		trainingPredatesMembers = trainingPredatesMembers &&
			model.TrainingCutoff.Before(member.Claim.ClaimedAt)
	}
	if model.SchemaID != "aura.adaptive.outcome-model/action-mean/v1" ||
		model.Revision != 1 || model.EndpointKind != endpoint ||
		model.Evaluator != canonicalEvaluator(frozen.Evaluator) ||
		model.RewardDefinitionSHA256 != rewardHash ||
		!canonicalEvidenceTime(model.TrainingCutoff) ||
		!validSHA256(model.TrainingLedgerSHA256) ||
		model.TrainingLedgerSHA256 == ledger.SHA256 ||
		model.ActionSchemaSHA256 != actionHash ||
		model.FeatureSchemaSHA256 != emptyHash ||
		model.AlgorithmID != "aura/endpoint-action-mean" ||
		model.AlgorithmRevision != 1 ||
		model.TargetPolicySHA256 != plan.TargetPolicySHA256 ||
		model.ClipMin.Cmp(MustExactRational(0, 1)) != 0 ||
		model.ClipMax.Cmp(MustExactRational(1, 1)) != 0 ||
		model.NoSupportFallback.Cmp(MustExactRational(0, 1)) < 0 ||
		model.NoSupportFallback.Cmp(MustExactRational(1, 1)) > 0 ||
		predictionErr != nil || predictionHash != model.PredictionSHA256 ||
		!modelPredictionsMatchPlan(model, plan) || !trainingPredatesMembers {
		return SelectedIntentionOPEArtifacts{}, "outcome_model_invalid"
	}
	targetHash, targetErr := target.SHA256()
	behaviorHash, behaviorErr := behavior.SHA256()
	if targetErr != nil || behaviorErr != nil ||
		targetHash != plan.TargetPolicySHA256 ||
		behaviorHash != plan.BehaviorPolicySHA256 {
		return SelectedIntentionOPEArtifacts{}, "policy_model_leakage"
	}
	return SelectedIntentionOPEArtifacts{
		ActionSchema: action, RewardDefinition: reward,
		TargetPolicy: target, BehaviorPolicy: behavior, Prediction: prediction,
		OutcomeModel: EndpointOutcomeModelBinding{
			EndpointKind: endpoint, ArtifactSHA256: registration.Ref.ArtifactSHA256,
			PredictionSHA256:       model.PredictionSHA256,
			RewardDefinitionSHA256: rewardHash,
			TargetPolicySHA256:     plan.TargetPolicySHA256,
		},
	}, ""
}
func modelPredictionsMatchPlan(model storedOutcomeModelMetadata, plan storedOPEPlanMetadata) bool {
	if len(model.Predictions) != len(plan.SupportedActions) {
		return false
	}
	for index, action := range plan.SupportedActions {
		prediction := model.Predictions[index]
		if prediction.ActionID != action ||
			prediction.SupportCount == 0 &&
				prediction.QHat.Cmp(model.NoSupportFallback) != 0 {
			return false
		}
	}
	return true
}
func selectedIntentionRows(cohort *FocalCohort, members []CohortMember, actions []string, endpoint EvidenceEndpointKind) ([]OPERow, ExactRational, map[string]uint64) {
	rows := make([]OPERow, 0, len(members))
	failures := make(map[string]uint64)
	overlap := MustExactRational(1, 1)
	for _, member := range members {
		row, rowOverlap, reason := selectedIntentionRow(
			cohort, member, actions, endpoint,
			member.Assignment.RecommendedActionID,
		)
		if reason != "" {
			failures[reason]++
			continue
		}
		if rowOverlap.Cmp(overlap) < 0 {
			overlap = rowOverlap
		}
		rows = append(rows, row)
	}
	return rows, overlap, failures
}
func selectedIntentionRow(cohort *FocalCohort, member CohortMember, actions []string, endpoint EvidenceEndpointKind, target string) (OPERow, ExactRational, string) {
	assignment := member.Assignment
	if member.Claim.AssignmentID != assignment.AssignmentID ||
		member.Claim.RequestID != assignment.RequestID ||
		assignment.ChampionActionID != StaticActionID ||
		assignment.RecommendedActionID != target ||
		!slices.Equal(assignment.EligibleActions, actions) {
		return OPERow{}, ExactRational{}, "assignment_binding_invalid"
	}
	probabilities, err := MarginalActionProbabilities(actions, StaticActionID, target)
	if err != nil || len(probabilities) != len(assignment.ActionProbabilities) {
		return OPERow{}, ExactRational{}, "behavior_support_invalid"
	}
	behavior := ExactRational{}
	for index, expected := range probabilities {
		expectedFloat, floatErr := expected.Probability.Float64()
		actual := assignment.ActionProbabilities[index]
		if floatErr != nil || actual.ActionID != expected.ActionID ||
			actual.Probability != expectedFloat {
			return OPERow{}, ExactRational{}, "behavior_support_invalid"
		}
		if expected.ActionID == target {
			behavior = expected.Probability
		}
	}
	if behavior.Cmp(MustExactRational(1, 2)) < 0 {
		return OPERow{}, ExactRational{}, "overlap_below_minimum"
	}
	observation := member.Quality
	var value *float64
	if endpoint == EvidenceEndpointQuality && observation != nil {
		value = observation.Measures.Quality
	}
	if endpoint == EvidenceEndpointHarm {
		observation = member.Harm
		if observation != nil {
			value = observation.Measures.Harm
		}
	}
	if observation == nil || observation.Status != OutcomeObserved ||
		value == nil || (*value != 0 && *value != 1) {
		return OPERow{}, ExactRational{}, "endpoint_outcome_invalid"
	}
	return OPERow{
		Order: OPEMemberOrder{
			AnalysisStratumID:     member.Claim.AnalysisStratumID,
			InterferenceClusterID: member.Claim.InterferenceClusterID,
			ClaimID:               member.Claim.ID.String(), RequestID: member.Claim.RequestID.String(),
			AssignmentID: member.Assignment.AssignmentID.String(),
		},
		TargetActionID:   assignment.RecommendedActionID,
		IntendedActionID: assignment.IntendedActionID, Reward: *value == 1,
		DeliveryKnown:       member.Delivery != nil && member.Delivery.ExposureKnown,
		ActionProbabilities: probabilities,
	}, behavior, ""
}
func applyOPERowFailures(report *EndpointOPEReport, failures map[string]uint64) {
	reasons := make([]string, 0, len(failures))
	var rows uint64
	for reason, count := range failures {
		reasons = append(reasons, reason)
		rows += count
	}
	slices.Sort(reasons)
	if rows > report.RowCount {
		rows = report.RowCount
	}
	report.IneligibleRowCount = rows
	report.IneligibleReasons = reasons
	report.IneligibleReasonCounts = make([]OPEIneligibleReasonCount, len(reasons))
	for index, reason := range reasons {
		report.IneligibleReasonCounts[index] = OPEIneligibleReasonCount{
			SchemaID: "aura.adaptive.report/ineligible-reason-count/v1",
			Revision: 1, Reason: reason, Count: failures[reason],
		}
	}
}
func markOPEReportIneligible(report *EndpointOPEReport, reason string, count uint64) {
	report.Eligible = false
	report.IneligibleReasons = []string{reason}
	report.IneligibleRowCount = min(count, report.RowCount)
	report.IneligibleReasonCounts = []OPEIneligibleReasonCount{{
		SchemaID: "aura.adaptive.report/ineligible-reason-count/v1",
		Revision: 1, Reason: reason, Count: count,
	}}
}
func opeBootstrapSeed(cohortID string, lookNumber int, endpoint EvidenceEndpointKind, ledgerSHA256, targetSHA256, modelSHA256 string) ([32]byte, error) {
	canonical, err := json.Marshal(struct {
		CohortID           string               `json:"cohort_id"`
		LookNumber         int                  `json:"look_number"`
		EndpointKind       EvidenceEndpointKind `json:"endpoint_kind"`
		ClosedLedgerSHA256 string               `json:"closed_ledger_sha256"`
		TargetPolicySHA256 string               `json:"target_policy_sha256"`
		OutcomeModelSHA256 string               `json:"outcome_model_sha256"`
	}{
		cohortID, lookNumber, endpoint, ledgerSHA256, targetSHA256, modelSHA256,
	})
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(append([]byte("aura/ope-cluster-bootstrap-sha256-v1\x00"), canonical...)), nil
}
func outwardEight(value float64, upper bool) float64 {
	scaled := value * 100_000_000
	if upper {
		scaled = math.Ceil(scaled)
	} else {
		scaled = math.Floor(scaled)
	}
	result := scaled / 100_000_000
	if result == 0 {
		return 0
	}
	return result
}
func finalizeOPEReports(quality, harm EndpointOPEReport) (opeReportPair, error) {
	if err := ValidateDistinctEndpointOPEReports(quality, harm); err != nil {
		return opeReportPair{}, err
	}
	qualitySHA256, err := quality.SHA256()
	if err != nil {
		return opeReportPair{}, err
	}
	harmSHA256, err := harm.SHA256()
	if err != nil {
		return opeReportPair{}, err
	}
	return opeReportPair{
		Quality: quality, Harm: harm,
		QualitySHA256: qualitySHA256, HarmSHA256: harmSHA256,
	}, nil
}
func opeReportArtifacts(reports opeReportPair) ([]EvidenceArtifactRegistration, error) {
	values := []struct {
		kind, id, sha256 string
		report           EndpointOPEReport
	}{
		{"quality_ope_report", reports.Quality.ReportID, reports.QualitySHA256, reports.Quality},
		{"harm_ope_report", reports.Harm.ReportID, reports.HarmSHA256, reports.Harm},
	}
	artifacts := make([]EvidenceArtifactRegistration, len(values))
	for index, value := range values {
		canonical, err := value.report.CanonicalJSON()
		if err != nil || sha256Hex(canonical) != value.sha256 {
			return nil, errors.New("adaptive OPE report hash changed")
		}
		artifacts[index] = EvidenceArtifactRegistration{
			Ref: ChildArtifactRef{
				SchemaID: ChildArtifactRefSchemaID, Revision: 1,
				Kind: value.kind, ArtifactSchemaID: "aura.adaptive.report/ope/v1",
				ArtifactRevision: 1, ArtifactID: value.id,
				ArtifactSHA256: value.sha256,
			},
			Canonical: canonical,
		}
	}
	return artifacts, nil
}
