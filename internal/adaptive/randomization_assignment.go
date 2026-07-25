package adaptive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/google/uuid"
)

// AssignmentTemplate contains only pre-draw context. It cannot select an arm.
type AssignmentTemplate struct {
	Features []SnapshotFeatureValue
}

// AssignmentTemplateBuilder computes bounded arm-neutral context before entropy.
type AssignmentTemplateBuilder func(
	*FocalCohort,
	FocalEnrollmentRequest,
) (AssignmentTemplate, error)

type randomizationAlternatives struct {
	baseline        Assignment
	challenger      Assignment
	receiptTemplate RandomizationReceipt
}

type canonicalActionSchema struct {
	SchemaID string   `json:"schema_id"`
	Revision int      `json:"revision"`
	Actions  []string `json:"actions"`
}

type canonicalFeatureSchema struct {
	SchemaID string                      `json:"schema_id"`
	Revision int                         `json:"revision"`
	Features []SnapshotFeatureDefinition `json:"features"`
}

type canonicalRandomizationPlan struct {
	SchemaID                    string                `json:"schema_id"`
	Revision                    int                   `json:"revision"`
	DesignID                    string                `json:"design_id"`
	MechanismID                 string                `json:"mechanism_id"`
	MechanismRevision           int                   `json:"mechanism_revision"`
	EntropySourceID             string                `json:"entropy_source_id"`
	DrawByteCount               int                   `json:"draw_byte_count"`
	BitIndex                    int                   `json:"bit_index"`
	CommonDenominator           int                   `json:"common_denominator"`
	AnalysisStratumSchemaID     string                `json:"analysis_stratum_schema_id"`
	AnalysisStratumRevision     int                   `json:"analysis_stratum_revision"`
	AnalysisStratumSchemaSHA256 string                `json:"analysis_stratum_schema_sha256"`
	BlockOrigin                 string                `json:"block_origin"`
	BlockWidthSeconds           uint64                `json:"block_width_seconds"`
	Arms                        []ReceiptArmPolicyRef `json:"arms"`
}

func buildRandomizationAlternatives(
	cohort *FocalCohort,
	snapshot *PolicySnapshot,
	request FocalEnrollmentRequest,
	claim FocalClaim,
	template AssignmentTemplate,
) (randomizationAlternatives, error) {
	if cohort == nil || snapshot == nil || !claim.matches(request, claim.AssignmentID) {
		return randomizationAlternatives{}, ErrFocalClaimConflict
	}
	if err := validateFrozenBernoulliArms(cohort.Arms()); err != nil {
		return randomizationAlternatives{}, err
	}
	scope := cohort.Scope()
	snapshotScope := snapshot.Scope()
	if snapshot.ID() != scope.SnapshotID || snapshot.SHA256() != scope.SnapshotSHA256 ||
		snapshotScope.OwnerID != scope.OwnerID || snapshotScope.Domain != request.Domain ||
		snapshotScope.Point != request.Point || snapshotScope.ProviderID != scope.ProviderID ||
		snapshotScope.ModelID != scope.ModelID || snapshotScope.PolicyVersion != scope.PolicyVersion {
		return randomizationAlternatives{}, ErrFocalClaimConflict
	}
	features, featureMap, err := canonicalTemplateFeatures(template.Features, snapshot.FeatureSchema())
	if err != nil {
		return randomizationAlternatives{}, fmt.Errorf("validate assignment template: %w", err)
	}
	decision := snapshot.Score(SnapshotQuery{
		OwnerID: scope.OwnerID, Domain: request.Domain, Point: request.Point,
		ProviderID: scope.ProviderID, ModelID: scope.ModelID,
		PolicyVersion: scope.PolicyVersion, Features: features,
	})
	if decision.Reason == SnapshotStaticInvalid ||
		decision.Reason == SnapshotStaticScopeMismatch ||
		decision.Reason == SnapshotStaticQueryInvalid {
		return randomizationAlternatives{}, ErrFocalClaimConflict
	}
	supportedActions := cohortActionIDs(cohort.Actions())
	if !slices.Equal(supportedActions, snapshotActionIDs(snapshot.Actions())) ||
		!slices.Contains(supportedActions, decision.ActionID) {
		return randomizationAlternatives{}, ErrFocalClaimConflict
	}
	exactProbabilities, err := MarginalActionProbabilities(
		supportedActions,
		StaticActionID,
		decision.ActionID,
	)
	if err != nil {
		return randomizationAlternatives{}, err
	}
	assignmentProbabilities := make([]ActionProbability, len(exactProbabilities))
	for index, probability := range exactProbabilities {
		value, err := probability.Probability.Float64()
		if err != nil {
			return randomizationAlternatives{}, errors.New("randomization probability is not finite binary64")
		}
		assignmentProbabilities[index] = ActionProbability{
			ActionID: probability.ActionID, Probability: value,
		}
	}
	eligibleActions, eligibilitySHA256, catalogSHA256, err := cohortAssignmentHashes(
		assignmentProbabilities,
	)
	if err != nil {
		return randomizationAlternatives{}, err
	}
	cohortID := cohort.ID()
	half := 0.5
	common := Assignment{
		SchemaVersion: SchemaVersion2, AssignmentID: claim.AssignmentID,
		OwnerID: request.OwnerID, RequestID: request.RequestID,
		Domain: request.Domain, Point: request.Point, PointOrdinal: request.PointOrdinal,
		PolicyEpoch: scope.PolicyEpoch, PolicyVersion: scope.PolicyVersion,
		PolicyMode: PolicyCanary, SnapshotID: scope.SnapshotID,
		SnapshotSHA256: scope.SnapshotSHA256, Environment: scope.Environment,
		ProviderID: scope.ProviderID, ModelID: scope.ModelID, CohortID: &cohortID,
		EligibleActions: eligibleActions, EligibilitySHA256: eligibilitySHA256,
		CatalogSHA256: catalogSHA256, ChampionActionID: StaticActionID,
		RecommendedActionID: decision.ActionID, ExperimentID: cohort.ExperimentID(),
		ArmProbability: &half, ActionProbabilities: assignmentProbabilities,
		SelectionReason: SelectionCanaryAssignment, Features: featureMap,
	}
	baseline := canonicalAssignment(common)
	baseline.ArmID = string(RandomizedArmBaseline)
	baseline.IntendedActionID = StaticActionID
	challenger := canonicalAssignment(common)
	challenger.ArmID = string(RandomizedArmChallenger)
	challenger.IntendedActionID = decision.ActionID
	if _, err := NewAssignmentEvent(baseline); err != nil {
		return randomizationAlternatives{}, fmt.Errorf("prevalidate baseline assignment: %w", err)
	}
	if _, err := NewAssignmentEvent(challenger); err != nil {
		return randomizationAlternatives{}, fmt.Errorf("prevalidate challenger assignment: %w", err)
	}
	stratum, err := NewAnalysisStratum(
		request.OwnerID.String(), claim.ClaimedAt, int64(cohort.Admission().TimeBlockSeconds),
	)
	if err != nil {
		return randomizationAlternatives{}, err
	}
	armRefs, planSHA256, err := randomizationPlanBindings(cohort, snapshot, stratum.SchemaSHA256)
	if err != nil {
		return randomizationAlternatives{}, err
	}
	return randomizationAlternatives{
		baseline: baseline, challenger: challenger,
		receiptTemplate: RandomizationReceipt{
			SchemaID: "aura.adaptive.randomization-receipt/v1", Revision: 1,
			AssignmentID: claim.AssignmentID.String(), OwnerID: request.OwnerID.String(),
			CohortID: request.CohortID.String(), RequestID: request.RequestID.String(),
			ClaimID: claim.ID.String(), RandomizationPlanSHA256: planSHA256,
			MechanismID: randomizationMechanismID, MechanismRevision: 1,
			EntropySourceID: randomizationEntropyID,
			ArmProbability:  MustExactRational(1, 2), AnalysisStratumID: stratum.ID,
			AnalysisStratumSchemaSHA256: stratum.SchemaSHA256,
			ArmPolicyRefs:               armRefs, ActionProbabilities: exactProbabilities,
		},
	}, nil
}

func validateFrozenBernoulliArms(arms []CohortArm) error {
	if len(arms) != 2 ||
		arms[0].ArmID != string(RandomizedArmBaseline) ||
		arms[1].ArmID != string(RandomizedArmChallenger) ||
		arms[0].Probability != 0.5 || arms[1].Probability != 0.5 {
		return errors.New("cohort does not freeze the equal baseline/challenger Bernoulli plan")
	}
	return nil
}

func canonicalTemplateFeatures(
	features []SnapshotFeatureValue,
	schema []SnapshotFeatureDefinition,
) ([]SnapshotFeatureValue, map[FeatureKey]float64, error) {
	canonical, err := canonicalSnapshotFeatureValues(features, schema)
	if err != nil {
		return nil, nil, err
	}
	asMap := make(map[FeatureKey]float64, len(canonical))
	for _, feature := range canonical {
		asMap[feature.Key] = feature.Value
	}
	return canonical, asMap, nil
}

func randomizationPlanBindings(
	cohort *FocalCohort,
	snapshot *PolicySnapshot,
	stratumSchemaSHA256 string,
) ([]ReceiptArmPolicyRef, string, error) {
	plan, err := canonicalRandomizationPlanFor(cohort.spec, snapshot)
	if err != nil {
		return nil, "", err
	}
	if plan.AnalysisStratumSchemaSHA256 != stratumSchemaSHA256 {
		return nil, "", errors.New("randomization stratum schema differs from frozen plan")
	}
	planSHA256, err := hashCanonical(plan)
	if err != nil {
		return nil, "", err
	}
	if cohort.randomizationEligible() &&
		(!reflect.DeepEqual(plan, cohort.v2.randomizationPlan) ||
			planSHA256 != cohort.v2.document.RandomizationPlanRef.ArtifactSHA256) {
		return nil, "", errors.New("randomization plan differs from cohort v2 binding")
	}
	return plan.Arms, planSHA256, nil
}

func canonicalRandomizationPlanFor(
	spec CohortSpec,
	snapshot *PolicySnapshot,
) (canonicalRandomizationPlan, error) {
	actionSchemaSHA256, err := hashCanonical(canonicalActionSchema{
		SchemaID: "aura.adaptive.action-schema/v1", Revision: 1,
		Actions: cohortActionIDs(spec.Actions),
	})
	if err != nil {
		return canonicalRandomizationPlan{}, err
	}
	featureSchemaSHA256, err := hashCanonical(canonicalFeatureSchema{
		SchemaID: "aura.adaptive.feature-schema/snapshot/v1", Revision: 1,
		Features: snapshot.FeatureSchema(),
	})
	if err != nil {
		return canonicalRandomizationPlan{}, err
	}
	armRef := func(armID, role, policyID string) ReceiptArmPolicyRef {
		return ReceiptArmPolicyRef{
			SchemaID: "aura.adaptive.arm-policy-ref/v1", Revision: 1,
			ArmID: armID, ArmRole: role, Weight: MustExactRational(1, 1),
			PolicyID: policyID, PolicyRevision: 1, SnapshotID: snapshot.ID().String(),
			SnapshotSHA256: snapshot.SHA256(), ActionSchemaSHA256: actionSchemaSHA256,
			FeatureSchemaSHA256: featureSchemaSHA256,
		}
	}
	refs := []ReceiptArmPolicyRef{
		armRef("baseline", "control", "aura/static-champion"),
		armRef("challenger", "treatment", "aura/graph-knn-snapshot"),
	}
	admission := spec.Admission
	stratum, err := NewAnalysisStratum(
		spec.Scope.OwnerID.String(), timeZeroUTC, int64(admission.TimeBlockSeconds),
	)
	if err != nil {
		return canonicalRandomizationPlan{}, err
	}
	return canonicalRandomizationPlan{
		SchemaID: "aura.adaptive.randomization-plan/v1", Revision: 1,
		DesignID: "independent-bernoulli", MechanismID: randomizationMechanismID,
		MechanismRevision: 1, EntropySourceID: randomizationEntropyID,
		DrawByteCount: 1, BitIndex: 0, CommonDenominator: 2,
		AnalysisStratumSchemaID: analysisStratumSchemaID, AnalysisStratumRevision: 1,
		AnalysisStratumSchemaSHA256: stratum.SchemaSHA256,
		BlockOrigin:                 "1970-01-01T00:00:00Z", BlockWidthSeconds: admission.TimeBlockSeconds,
		Arms: refs,
	}, nil
}

func cohortActionIDs(actions []ActionProbability) []string {
	ids := make([]string, len(actions))
	for index, action := range actions {
		ids[index] = action.ActionID
	}
	return ids
}

func snapshotActionIDs(actions []SnapshotAction) []string {
	ids := make([]string, len(actions))
	for index, action := range actions {
		ids[index] = action.ActionID
	}
	return ids
}

func hashCanonical(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

func selectedRandomization(
	alternatives randomizationAlternatives,
	selection RandomizedArmSelection,
) (Assignment, RandomizationReceipt, error) {
	assignment := alternatives.baseline
	role := "control"
	if selection.Arm == RandomizedArmChallenger {
		assignment = alternatives.challenger
		role = "treatment"
	}
	receipt := alternatives.receiptTemplate
	receipt.DrawByte = selection.DrawByte
	receipt.MappedBit = selection.MappedBit
	receipt.SelectedArmID = string(selection.Arm)
	receipt.SelectedArmRole = role
	if err := receipt.ValidateAssignment(randomizationAssignmentBinding(assignment, role)); err != nil {
		return Assignment{}, RandomizationReceipt{}, err
	}
	return assignment, receipt, nil
}

func randomizationAssignmentBinding(
	assignment Assignment,
	role string,
) RandomizationAssignmentBinding {
	exact := make([]ExactActionProbability, len(assignment.ActionProbabilities))
	for index, probability := range assignment.ActionProbabilities {
		exact[index] = ExactActionProbability{
			ActionID: probability.ActionID,
			Probability: MustExactRational(
				int64(probability.Probability*2),
				2,
			),
		}
	}
	return RandomizationAssignmentBinding{
		AssignmentID: assignment.AssignmentID.String(), OwnerID: assignment.OwnerID.String(),
		CohortID: assignment.CohortID.String(), RequestID: assignment.RequestID.String(),
		SelectedArmID: assignment.ArmID, SelectedArmRole: role,
		ArmProbability: MustExactRational(1, 2), IntendedActionID: assignment.IntendedActionID,
		ActionProbabilities: exact,
	}
}

func deterministicReceiptID(assignmentID uuid.UUID) uuid.UUID {
	return eventIDForSource(assignmentID, EventDecision, "randomization-receipt")
}
