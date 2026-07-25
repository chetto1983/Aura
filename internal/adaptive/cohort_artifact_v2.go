package adaptive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
)

const (
	// CohortSchemaIDV2 identifies the #93.7 cohort container.
	CohortSchemaIDV2 = "aura.adaptive.cohort/v2"
	// ChildArtifactRefSchemaID identifies immutable cohort child references.
	ChildArtifactRefSchemaID = "aura.adaptive.child-artifact-ref/v1"
)

var randomizationPlanIDNamespace = uuid.MustParse("11747d85-1a5f-4b4c-8308-ff9fb8bc0daf")

// ChildArtifactRef binds one immutable #93.7 child artifact.
type ChildArtifactRef struct {
	SchemaID         string `json:"schema_id"`
	Revision         int    `json:"revision"`
	Kind             string `json:"kind"`
	ArtifactSchemaID string `json:"artifact_schema_id"`
	ArtifactRevision int    `json:"artifact_revision"`
	ArtifactID       string `json:"artifact_id"`
	ArtifactSHA256   string `json:"artifact_sha256"`
}

// CohortV2ChildRefs supplies the five non-randomization child bindings.
// The randomization-plan reference is derived from its exact canonical bytes.
type CohortV2ChildRefs struct {
	InterferencePlan    ChildArtifactRef
	LookPlan            ChildArtifactRef
	QualityOutcomeModel ChildArtifactRef
	HarmOutcomeModel    ChildArtifactRef
	OPEPlan             ChildArtifactRef
}

type canonicalCohortPowerV2 struct {
	BaselineQualityRate   ExactRational `json:"baseline_quality_rate"`
	BaselineHarmRate      ExactRational `json:"baseline_harm_rate"`
	MinimumDetectableLift ExactRational `json:"minimum_detectable_lift"`
	ExpectedMembers       uint64        `json:"expected_members"`
	MinimumMembers        uint64        `json:"minimum_members"`
	MinimumArmSupport     ExactRational `json:"minimum_arm_support"`
	SimulationSHA256      string        `json:"simulation_sha256"`
}

type canonicalCohortMarginsV2 struct {
	MinimumQualityUplift ExactRational `json:"minimum_quality_uplift"`
	MaximumHarmIncrease  ExactRational `json:"maximum_harm_increase"`
}

type canonicalCohortCensoringV2 struct {
	MaxCensorRate      ExactRational `json:"max_censor_rate"`
	MaxCensorImbalance ExactRational `json:"max_censor_imbalance"`
}

type canonicalCohortV2 struct {
	SchemaID               string                     `json:"schema_id"`
	Revision               int                        `json:"revision"`
	OwnerID                string                     `json:"owner_id"`
	Domain                 Domain                     `json:"domain"`
	ProviderID             string                     `json:"provider_id"`
	ModelID                string                     `json:"model_id"`
	PolicyEpoch            uint64                     `json:"policy_epoch"`
	PolicyVersion          string                     `json:"policy_version"`
	SnapshotID             string                     `json:"snapshot_id"`
	SnapshotSHA256         string                     `json:"snapshot_sha256"`
	Environment            EvaluationEnvironment      `json:"environment"`
	ExperimentID           string                     `json:"experiment_id"`
	Predicate              FocalPredicate             `json:"predicate"`
	PredicateSHA256        string                     `json:"predicate_sha256"`
	SupportedActions       []string                   `json:"supported_actions"`
	Evaluators             []EvaluatorProvenance      `json:"evaluators"`
	PrimaryQuality         CohortEndpoint             `json:"primary_quality"`
	PrimaryHarm            CohortEndpoint             `json:"primary_harm"`
	Power                  canonicalCohortPowerV2     `json:"power"`
	Margins                canonicalCohortMarginsV2   `json:"margins"`
	Censoring              canonicalCohortCensoringV2 `json:"censoring"`
	RandomizationPlanRef   ChildArtifactRef           `json:"randomization_plan_ref"`
	InterferencePlanRef    ChildArtifactRef           `json:"interference_plan_ref"`
	LookPlanRef            ChildArtifactRef           `json:"look_plan_ref"`
	QualityOutcomeModelRef ChildArtifactRef           `json:"quality_outcome_model_ref"`
	HarmOutcomeModelRef    ChildArtifactRef           `json:"harm_outcome_model_ref"`
	OPEPlanRef             ChildArtifactRef           `json:"ope_plan_ref"`
	Cutoff                 time.Time                  `json:"cutoff"`
}

type cohortV2State struct {
	document          canonicalCohortV2
	randomizationPlan canonicalRandomizationPlan
}

// NewRandomizedFocalCohort constructs the exact #93.7 cohort required for enrollment.
func NewRandomizedFocalCohort(
	spec CohortSpec,
	snapshot *PolicySnapshot,
	refs CohortV2ChildRefs,
) (*FocalCohort, error) {
	spec = canonicalCohortSpec(spec)
	if err := validateCohortSpec(spec); err != nil {
		return nil, err
	}
	if snapshot == nil || !snapshotMatchesCohortSpec(snapshot, spec) {
		return nil, errors.New("adaptive cohort v2 snapshot binding is invalid")
	}
	if err := validateFrozenBernoulliArms(spec.Arms); err != nil {
		return nil, err
	}
	plan, err := canonicalRandomizationPlanFor(spec, snapshot)
	if err != nil {
		return nil, err
	}
	planSHA256, err := hashCanonical(plan)
	if err != nil {
		return nil, err
	}
	planDigest, _ := hex.DecodeString(planSHA256)
	randomizationRef := ChildArtifactRef{
		SchemaID: ChildArtifactRefSchemaID, Revision: 1, Kind: "randomization_plan",
		ArtifactSchemaID: "aura.adaptive.randomization-plan/v1", ArtifactRevision: 1,
		ArtifactID: uuid.NewHash(
			sha256.New(), randomizationPlanIDNamespace, planDigest, 5,
		).String(),
		ArtifactSHA256: planSHA256,
	}
	document, err := canonicalCohortV2FromSpec(spec, randomizationRef, refs)
	if err != nil {
		return nil, err
	}
	return newFocalCohortV2(document, plan)
}

func canonicalCohortV2FromSpec(
	spec CohortSpec,
	randomizationRef ChildArtifactRef,
	refs CohortV2ChildRefs,
) (canonicalCohortV2, error) {
	predicateSHA256, err := canonicalCohortHash(spec.Predicate)
	if err != nil {
		return canonicalCohortV2{}, err
	}
	power, margins, censoring, err := canonicalCohortMetricsV2(spec)
	if err != nil {
		return canonicalCohortV2{}, err
	}
	evaluators := make([]EvaluatorProvenance, len(spec.Evaluators))
	for index, evaluator := range spec.Evaluators {
		evaluators[index] = evaluator.Provenance()
	}
	return canonicalCohortV2{
		SchemaID: CohortSchemaIDV2, Revision: 1,
		OwnerID: spec.Scope.OwnerID.String(), Domain: spec.Predicate.Domain,
		ProviderID: spec.Scope.ProviderID, ModelID: spec.Scope.ModelID,
		PolicyEpoch: spec.Scope.PolicyEpoch, PolicyVersion: spec.Scope.PolicyVersion,
		SnapshotID: spec.Scope.SnapshotID.String(), SnapshotSHA256: spec.Scope.SnapshotSHA256,
		Environment: spec.Scope.Environment, ExperimentID: spec.ExperimentID,
		Predicate: spec.Predicate, PredicateSHA256: predicateSHA256,
		SupportedActions: cohortActionIDs(spec.Actions), Evaluators: evaluators,
		PrimaryQuality: spec.PrimaryQuality, PrimaryHarm: spec.PrimaryHarm,
		Power: power, Margins: margins, Censoring: censoring,
		RandomizationPlanRef: randomizationRef,
		InterferencePlanRef:  refs.InterferencePlan, LookPlanRef: refs.LookPlan,
		QualityOutcomeModelRef: refs.QualityOutcomeModel,
		HarmOutcomeModelRef:    refs.HarmOutcomeModel, OPEPlanRef: refs.OPEPlan,
		Cutoff: spec.Cutoff,
	}, nil
}

func newFocalCohortV2(
	document canonicalCohortV2,
	plan canonicalRandomizationPlan,
) (*FocalCohort, error) {
	spec, err := validateCanonicalCohortV2(document, plan)
	if err != nil {
		return nil, err
	}
	artifact, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("marshal adaptive cohort v2: %w", err)
	}
	contentSum := sha256.Sum256(artifact)
	contentSHA256 := hex.EncodeToString(contentSum[:])
	return &FocalCohort{
		id:     uuid.NewHash(sha256.New(), cohortIDNamespace, contentSum[:], 5),
		sha256: contentSHA256, predicateSHA256: document.PredicateSHA256,
		spec: spec, v2: &cohortV2State{document: document, randomizationPlan: plan},
	}, nil
}

func validateCanonicalCohortV2(
	document canonicalCohortV2,
	plan canonicalRandomizationPlan,
) (CohortSpec, error) {
	if document.SchemaID != CohortSchemaIDV2 || document.Revision != 1 {
		return CohortSpec{}, errors.New("adaptive cohort v2 schema is invalid")
	}
	ownerID, ownerErr := uuid.Parse(document.OwnerID)
	snapshotID, snapshotErr := uuid.Parse(document.SnapshotID)
	if ownerErr != nil || ownerID.String() != document.OwnerID ||
		snapshotErr != nil || snapshotID.String() != document.SnapshotID {
		return CohortSpec{}, errors.New("adaptive cohort v2 identity is invalid")
	}
	predicateSHA256, err := canonicalCohortHash(document.Predicate)
	if err != nil || predicateSHA256 != document.PredicateSHA256 {
		return CohortSpec{}, errors.New("adaptive cohort v2 predicate hash is invalid")
	}
	refRules := []struct {
		ref    ChildArtifactRef
		kind   string
		schema string
	}{
		{document.RandomizationPlanRef, "randomization_plan", "aura.adaptive.randomization-plan/v1"},
		{document.InterferencePlanRef, "interference_plan", "aura.adaptive.interference-plan/v1"},
		{document.LookPlanRef, "look_plan", "aura.adaptive.look-plan/utc-cutoff/v1"},
		{document.QualityOutcomeModelRef, "quality_outcome_model", "aura.adaptive.outcome-model/action-mean/v1"},
		{document.HarmOutcomeModelRef, "harm_outcome_model", "aura.adaptive.outcome-model/action-mean/v1"},
		{document.OPEPlanRef, "ope_plan", "aura.adaptive.ope-plan/v1"},
	}
	for _, rule := range refRules {
		if err := validateChildArtifactRef(rule.ref, rule.kind, rule.schema); err != nil {
			return CohortSpec{}, err
		}
	}
	planSHA256, err := hashCanonical(plan)
	if err != nil || planSHA256 != document.RandomizationPlanRef.ArtifactSHA256 {
		return CohortSpec{}, errors.New("adaptive cohort v2 randomization plan hash is invalid")
	}
	if err := validateCanonicalRandomizationPlan(plan, document); err != nil {
		return CohortSpec{}, err
	}
	if len(document.SupportedActions) < 2 ||
		len(document.SupportedActions) > maxCohortCatalogEntries ||
		!slices.IsSorted(document.SupportedActions) {
		return CohortSpec{}, errors.New("adaptive cohort v2 supported actions are invalid")
	}
	actions := make([]ActionProbability, len(document.SupportedActions))
	remaining := 0.5 / float64(len(actions)-1)
	for index, actionID := range document.SupportedActions {
		if !safeCohortID(actionID, maxActionIDLength) ||
			(index > 0 && actionID == document.SupportedActions[index-1]) {
			return CohortSpec{}, errors.New("adaptive cohort v2 supported action is invalid")
		}
		probability := remaining
		if actionID == StaticActionID {
			probability = 0.5
		}
		actions[index] = ActionProbability{ActionID: actionID, Probability: probability}
	}
	registrations := make([]EvaluatorRegistration, len(document.Evaluators))
	for index, evaluator := range document.Evaluators {
		registrations[index] = EvaluatorRegistration(evaluator)
	}
	spec, err := cohortSpecFromCanonicalV2(
		document, ownerID, snapshotID, actions, registrations, plan.BlockWidthSeconds,
	)
	if err != nil {
		return CohortSpec{}, err
	}
	return spec, nil
}

func cohortSpecFromCanonicalV2(
	document canonicalCohortV2,
	ownerID uuid.UUID,
	snapshotID uuid.UUID,
	actions []ActionProbability,
	registrations []EvaluatorRegistration,
	blockWidthSeconds uint64,
) (CohortSpec, error) {
	rationals := []ExactRational{
		document.Power.BaselineQualityRate, document.Power.BaselineHarmRate,
		document.Power.MinimumDetectableLift, document.Power.MinimumArmSupport,
		document.Margins.MinimumQualityUplift, document.Margins.MaximumHarmIncrease,
		document.Censoring.MaxCensorRate, document.Censoring.MaxCensorImbalance,
	}
	values := make([]float64, len(rationals))
	for index, rational := range rationals {
		var err error
		values[index], err = rational.Float64()
		if err != nil {
			return CohortSpec{}, err
		}
	}
	spec := CohortSpec{
		Scope: CohortScope{
			OwnerID: ownerID, ProviderID: document.ProviderID, ModelID: document.ModelID,
			PolicyVersion: document.PolicyVersion, PolicyEpoch: document.PolicyEpoch,
			SnapshotID: snapshotID, SnapshotSHA256: document.SnapshotSHA256,
			Environment: document.Environment,
		},
		Predicate: document.Predicate, ExperimentID: document.ExperimentID,
		Arms: []CohortArm{
			{ArmID: string(RandomizedArmBaseline), Probability: 0.5},
			{ArmID: string(RandomizedArmChallenger), Probability: 0.5},
		},
		Actions: actions, Evaluators: registrations,
		PrimaryQuality: document.PrimaryQuality, PrimaryHarm: document.PrimaryHarm,
		Power: CohortPowerPlan{
			BaselineQualityRate: values[0], BaselineHarmRate: values[1],
			MinimumDetectableLift: values[2], ExpectedMembers: document.Power.ExpectedMembers,
			MinimumMembers: document.Power.MinimumMembers, MinimumArmSupport: values[3],
			SimulationSHA256: document.Power.SimulationSHA256,
		},
		Margins: CohortMargins{
			MinimumQualityUplift: values[4], MaximumHarmIncrease: values[5],
		},
		Censoring: CohortCensoring{
			MaxCensorRate: values[6], MaxCensorImbalance: values[7],
		},
		Admission: CohortAdmission{
			BlockKeys:        []BlockKey{BlockOwner, BlockTimeBlock},
			TimeBlockSeconds: blockWidthSeconds, Interference: InterferenceNoneBetweenUnits,
		},
		Looks: []CohortLook{
			{Look: 1, Alpha: 0.005}, {Look: 2, Alpha: 0.005},
			{Look: 3, Alpha: 0.01}, {Look: 4, Alpha: 0.01}, {Look: 5, Alpha: 0.02},
		},
		Cutoff: document.Cutoff,
	}
	spec = canonicalCohortSpec(spec)
	if err := validateCohortSpec(spec); err != nil {
		return CohortSpec{}, fmt.Errorf("validate adaptive cohort v2 projection: %w", err)
	}
	return spec, nil
}

func canonicalCohortMetricsV2(
	spec CohortSpec,
) (canonicalCohortPowerV2, canonicalCohortMarginsV2, canonicalCohortCensoringV2, error) {
	values := []float64{
		spec.Power.BaselineQualityRate, spec.Power.BaselineHarmRate,
		spec.Power.MinimumDetectableLift, spec.Power.MinimumArmSupport,
		spec.Margins.MinimumQualityUplift, spec.Margins.MaximumHarmIncrease,
		spec.Censoring.MaxCensorRate, spec.Censoring.MaxCensorImbalance,
	}
	exact := make([]ExactRational, len(values))
	for index, value := range values {
		var err error
		exact[index], err = exactRationalFromFloat(value)
		if err != nil {
			return canonicalCohortPowerV2{}, canonicalCohortMarginsV2{},
				canonicalCohortCensoringV2{}, err
		}
	}
	return canonicalCohortPowerV2{
			BaselineQualityRate: exact[0], BaselineHarmRate: exact[1],
			MinimumDetectableLift: exact[2], ExpectedMembers: spec.Power.ExpectedMembers,
			MinimumMembers: spec.Power.MinimumMembers, MinimumArmSupport: exact[3],
			SimulationSHA256: spec.Power.SimulationSHA256,
		},
		canonicalCohortMarginsV2{
			MinimumQualityUplift: exact[4], MaximumHarmIncrease: exact[5],
		},
		canonicalCohortCensoringV2{
			MaxCensorRate: exact[6], MaxCensorImbalance: exact[7],
		}, nil
}

func exactRationalFromFloat(value float64) (ExactRational, error) {
	rational, ok := new(big.Rat).SetString(strconv.FormatFloat(value, 'f', -1, 64))
	if !ok {
		return ExactRational{}, errors.New("adaptive cohort v2 rational is invalid")
	}
	return NewExactRational(rational.Num(), rational.Denom())
}
