package adaptive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// CohortPlannedLooks fixes the preregistered sequential analysis schedule.
	CohortPlannedLooks = 5
	// CohortTotalAlpha caps total sequential false-positive allocation.
	CohortTotalAlpha        = 0.05
	maxCohortCatalogEntries = 128
	maxCohortBlockKeys      = 4
)

var cohortIDNamespace = uuid.MustParse("37e07806-1464-47f3-8d89-f922fe7a1392")

// BlockKey identifies a frozen unit-interference blocking dimension.
type BlockKey string

const (
	// BlockEpisode scopes a unit to its evaluation episode.
	BlockEpisode BlockKey = "episode"
	// BlockOwner scopes a unit to its owner.
	BlockOwner BlockKey = "owner"
	// BlockSession scopes a unit to its session.
	BlockSession BlockKey = "session"
	// BlockTimeBlock scopes a unit to a frozen time interval.
	BlockTimeBlock BlockKey = "time_block"
)

// Interference specifies the preregistered cross-unit interference assumption.
type Interference string

const (
	// InterferenceNoneBetweenUnits declares the required no-interference assumption.
	InterferenceNoneBetweenUnits Interference = "none_between_units"
	// InterferenceUnmodelled marks an ineligible unmodelled interference assumption.
	InterferenceUnmodelled Interference = "unmodelled"
)

// CohortScope freezes the owner and serving artifacts eligible for a cohort.
type CohortScope struct {
	OwnerID        uuid.UUID             `json:"owner_id"`
	ProviderID     string                `json:"provider_id"`
	ModelID        string                `json:"model_id"`
	PolicyVersion  string                `json:"policy_version"`
	PolicyEpoch    uint64                `json:"policy_epoch"`
	SnapshotID     uuid.UUID             `json:"snapshot_id"`
	SnapshotSHA256 string                `json:"snapshot_sha256"`
	Environment    EvaluationEnvironment `json:"environment"`
}

// FocalPredicate identifies the one decision point eligible for randomization.
type FocalPredicate struct {
	Domain  Domain        `json:"domain"`
	Point   DecisionPoint `json:"point"`
	Ordinal uint32        `json:"ordinal"`
}

// CohortArm records a randomized experimental arm and its true probability.
type CohortArm struct {
	ArmID       string  `json:"arm_id"`
	Probability float64 `json:"probability"`
}

// CohortEndpointKind identifies an exact binary primary endpoint role.
type CohortEndpointKind string

const (
	// CohortBinaryQuality identifies the frozen binary primary quality endpoint.
	CohortBinaryQuality CohortEndpointKind = "binary_quality"
	// CohortBinaryHarm identifies the frozen binary primary harm endpoint.
	CohortBinaryHarm CohortEndpointKind = "binary_harm"
)

// CohortEndpoint freezes one safe binary endpoint and its evaluator artifact.
type CohortEndpoint struct {
	ID        string              `json:"id"`
	Kind      CohortEndpointKind  `json:"kind"`
	Evaluator EvaluatorProvenance `json:"evaluator"`
}

// CohortPowerPlan freezes the preregistered power and support assumptions.
type CohortPowerPlan struct {
	BaselineQualityRate   float64 `json:"baseline_quality_rate"`
	BaselineHarmRate      float64 `json:"baseline_harm_rate"`
	MinimumDetectableLift float64 `json:"minimum_detectable_lift"`
	ExpectedMembers       uint64  `json:"expected_members"`
	MinimumMembers        uint64  `json:"minimum_members"`
	MinimumArmSupport     float64 `json:"minimum_arm_support"`
	SimulationSHA256      string  `json:"simulation_sha256"`
}

// CohortMargins freezes the primary quality and harm decision margins.
type CohortMargins struct {
	MinimumQualityUplift float64 `json:"minimum_quality_uplift"`
	MaximumHarmIncrease  float64 `json:"maximum_harm_increase"`
}

// CohortCensoring freezes permitted censoring limits.
type CohortCensoring struct {
	MaxCensorRate      float64 `json:"max_censor_rate"`
	MaxCensorImbalance float64 `json:"max_censor_imbalance"`
}

// CohortAdmission freezes the unit blocking and interference plan.
type CohortAdmission struct {
	BlockKeys        []BlockKey   `json:"block_keys"`
	TimeBlockSeconds uint64       `json:"time_block_seconds"`
	Interference     Interference `json:"interference"`
}

// CohortLook allocates alpha to one planned sequential look.
type CohortLook struct {
	Look  uint8   `json:"look"`
	Alpha float64 `json:"alpha"`
}

// CohortSpec is caller input for a canonical immutable focal cohort.
type CohortSpec struct {
	Scope          CohortScope
	Predicate      FocalPredicate
	ExperimentID   string
	Arms           []CohortArm
	Actions        []ActionProbability
	Evaluators     []EvaluatorRegistration
	PrimaryQuality CohortEndpoint
	PrimaryHarm    CohortEndpoint
	Power          CohortPowerPlan
	Margins        CohortMargins
	Censoring      CohortCensoring
	Admission      CohortAdmission
	Looks          []CohortLook
	Cutoff         time.Time
}

// FocalCohort is the immutable, preregistered cohort contract for one focal point.
type FocalCohort struct {
	id                      uuid.UUID
	sha256, predicateSHA256 string
	spec                    CohortSpec
}

// NewFocalCohort validates and freezes a preregistered focal cohort specification.
func NewFocalCohort(spec CohortSpec) (*FocalCohort, error) {
	if len(spec.Arms) > maxCohortCatalogEntries ||
		len(spec.Actions) > maxCohortCatalogEntries ||
		len(spec.Evaluators) > maxCohortCatalogEntries ||
		len(spec.Admission.BlockKeys) > maxCohortBlockKeys ||
		len(spec.Looks) > CohortPlannedLooks {
		return nil, errors.New("adaptive cohort collections exceed limits")
	}
	spec = canonicalCohortSpec(spec)
	if err := validateCohortSpec(spec); err != nil {
		return nil, err
	}
	predicateSHA256, err := canonicalCohortHash(spec.Predicate)
	if err != nil {
		return nil, err
	}
	artifact, err := marshalCanonicalCohort(spec)
	if err != nil {
		return nil, err
	}
	contentSum := sha256.Sum256(artifact)
	contentSHA256 := hex.EncodeToString(contentSum[:])
	digest, err := hex.DecodeString(contentSHA256)
	if err != nil {
		return nil, fmt.Errorf("decode canonical cohort hash: %w", err)
	}
	return &FocalCohort{
		id:              uuid.NewHash(sha256.New(), cohortIDNamespace, digest, 5),
		sha256:          contentSHA256,
		predicateSHA256: predicateSHA256,
		spec:            spec,
	}, nil
}

// ID returns the deterministic cohort identifier.
func (cohort *FocalCohort) ID() uuid.UUID {
	if cohort == nil {
		return uuid.Nil
	}
	return cohort.id
}

// SHA256 returns the canonical cohort content hash.
func (cohort *FocalCohort) SHA256() string {
	if cohort == nil {
		return ""
	}
	return cohort.sha256
}

// PredicateSHA256 returns the canonical focal predicate hash.
func (cohort *FocalCohort) PredicateSHA256() string {
	if cohort == nil {
		return ""
	}
	return cohort.predicateSHA256
}

// Scope returns the frozen scope.
func (cohort *FocalCohort) Scope() CohortScope {
	if cohort == nil {
		return CohortScope{}
	}
	return cohort.spec.Scope
}

// Predicate returns the frozen focal predicate.
func (cohort *FocalCohort) Predicate() FocalPredicate {
	if cohort == nil {
		return FocalPredicate{}
	}
	return cohort.spec.Predicate
}

// Domain returns the focal domain.
func (cohort *FocalCohort) Domain() Domain { return cohort.Predicate().Domain }

// ExperimentID returns the frozen experiment identifier.
func (cohort *FocalCohort) ExperimentID() string {
	if cohort == nil {
		return ""
	}
	return cohort.spec.ExperimentID
}

// Arms returns a defensive copy of the frozen arm catalog.
func (cohort *FocalCohort) Arms() []CohortArm {
	if cohort == nil {
		return nil
	}
	return slices.Clone(cohort.spec.Arms)
}

// Actions returns a defensive copy of the frozen action catalog.
func (cohort *FocalCohort) Actions() []ActionProbability {
	if cohort == nil {
		return nil
	}
	return slices.Clone(cohort.spec.Actions)
}

// Evaluators returns a defensive copy of the frozen evaluator catalog.
func (cohort *FocalCohort) Evaluators() []EvaluatorRegistration {
	if cohort == nil {
		return nil
	}
	return slices.Clone(cohort.spec.Evaluators)
}

// PrimaryQualityEndpoint returns the frozen primary binary quality endpoint.
func (cohort *FocalCohort) PrimaryQualityEndpoint() CohortEndpoint {
	if cohort == nil {
		return CohortEndpoint{}
	}
	return cohort.spec.PrimaryQuality
}

// PrimaryHarmEndpoint returns the frozen primary binary harm endpoint.
func (cohort *FocalCohort) PrimaryHarmEndpoint() CohortEndpoint {
	if cohort == nil {
		return CohortEndpoint{}
	}
	return cohort.spec.PrimaryHarm
}

// ValidatePrimaryQualityOutcome checks persisted quality evidence against the frozen cohort endpoint.
func (cohort *FocalCohort) ValidatePrimaryQualityOutcome(observation OutcomeObservation) error {
	return validatePrimaryOutcome(cohort, observation, cohort.PrimaryQualityEndpoint(), observation.Measures.Quality)
}

// ValidatePrimaryHarmOutcome checks persisted harm evidence against the frozen cohort endpoint.
func (cohort *FocalCohort) ValidatePrimaryHarmOutcome(observation OutcomeObservation) error {
	return validatePrimaryOutcome(cohort, observation, cohort.PrimaryHarmEndpoint(), observation.Measures.Harm)
}

func validatePrimaryOutcome(cohort *FocalCohort, observation OutcomeObservation, endpoint CohortEndpoint, value *float64) error {
	if !cohort.initialized() ||
		observation.OwnerID != cohort.spec.Scope.OwnerID ||
		observation.Domain != cohort.spec.Predicate.Domain ||
		observation.ProviderID != cohort.spec.Scope.ProviderID ||
		observation.ModelID != cohort.spec.Scope.ModelID ||
		observation.Status != OutcomeObserved || observation.Evaluator != endpoint.Evaluator ||
		value == nil || (*value != 0 && *value != 1) {
		return errors.New("adaptive cohort primary outcome evidence is invalid")
	}
	return nil
}

// Power returns the frozen power plan.
func (cohort *FocalCohort) Power() CohortPowerPlan {
	if cohort == nil {
		return CohortPowerPlan{}
	}
	return cohort.spec.Power
}

// Margins returns the frozen decision margins.
func (cohort *FocalCohort) Margins() CohortMargins {
	if cohort == nil {
		return CohortMargins{}
	}
	return cohort.spec.Margins
}

// Censoring returns the frozen censoring limits.
func (cohort *FocalCohort) Censoring() CohortCensoring {
	if cohort == nil {
		return CohortCensoring{}
	}
	return cohort.spec.Censoring
}

// Admission returns the frozen admission plan with copied block keys.
func (cohort *FocalCohort) Admission() CohortAdmission {
	if cohort == nil {
		return CohortAdmission{}
	}
	admission := cohort.spec.Admission
	admission.BlockKeys = slices.Clone(admission.BlockKeys)
	return admission
}

// Looks returns a defensive copy of the planned alpha schedule.
func (cohort *FocalCohort) Looks() []CohortLook {
	if cohort == nil {
		return nil
	}
	return slices.Clone(cohort.spec.Looks)
}

// Cutoff returns the canonical PostgreSQL-precision evaluation cutoff.
func (cohort *FocalCohort) Cutoff() time.Time {
	if cohort == nil {
		return time.Time{}
	}
	return cohort.spec.Cutoff
}

// MatchesFocalPoint reports whether a decision is the cohort's sole focal point.
func (cohort *FocalCohort) MatchesFocalPoint(domain Domain, point DecisionPoint, ordinal uint32) bool {
	return cohort.initialized() && cohort.spec.Predicate == (FocalPredicate{Domain: domain, Point: point, Ordinal: ordinal})
}

func (cohort *FocalCohort) initialized() bool {
	return cohort != nil && cohort.id != uuid.Nil && cohort.sha256 != "" && cohort.predicateSHA256 != ""
}

func canonicalCohortSpec(spec CohortSpec) CohortSpec {
	spec.Arms = slices.Clone(spec.Arms)
	spec.Actions = slices.Clone(spec.Actions)
	spec.Evaluators = slices.Clone(spec.Evaluators)
	spec.Admission.BlockKeys = slices.Clone(spec.Admission.BlockKeys)
	spec.Looks = slices.Clone(spec.Looks)
	spec.Cutoff = spec.Cutoff.UTC().Truncate(time.Microsecond)
	for i := range spec.Arms {
		spec.Arms[i].Probability = canonicalZero(spec.Arms[i].Probability)
	}
	for i := range spec.Actions {
		spec.Actions[i].Probability = canonicalZero(spec.Actions[i].Probability)
	}
	for i := range spec.Looks {
		spec.Looks[i].Alpha = canonicalZero(spec.Looks[i].Alpha)
	}
	spec.Power.BaselineQualityRate = canonicalZero(spec.Power.BaselineQualityRate)
	spec.Power.BaselineHarmRate = canonicalZero(spec.Power.BaselineHarmRate)
	spec.Power.MinimumDetectableLift = canonicalZero(spec.Power.MinimumDetectableLift)
	spec.Power.MinimumArmSupport = canonicalZero(spec.Power.MinimumArmSupport)
	spec.Margins.MinimumQualityUplift = canonicalZero(spec.Margins.MinimumQualityUplift)
	spec.Margins.MaximumHarmIncrease = canonicalZero(spec.Margins.MaximumHarmIncrease)
	spec.Censoring.MaxCensorRate = canonicalZero(spec.Censoring.MaxCensorRate)
	spec.Censoring.MaxCensorImbalance = canonicalZero(spec.Censoring.MaxCensorImbalance)
	return spec
}

func validateCohortSpec(spec CohortSpec) error {
	if err := validateCohortScope(spec.Scope); err != nil {
		return err
	}
	if spec.Predicate.Domain.point() != spec.Predicate.Point || !spec.Predicate.Domain.valid() || !spec.Predicate.Point.valid() {
		return errors.New("adaptive cohort focal predicate is invalid")
	}
	if !safeCohortID(spec.ExperimentID, maxExperimentIDLength) {
		return errors.New("adaptive cohort experiment_id is invalid")
	}
	if err := validateCohortArms(spec.Arms); err != nil {
		return err
	}
	if err := validateCohortActions(spec.Actions); err != nil {
		return err
	}
	if err := validateCohortEvaluators(spec.Evaluators, spec.PrimaryQuality, spec.PrimaryHarm); err != nil {
		return err
	}
	if err := validateCohortNumbers(spec); err != nil {
		return err
	}
	return validateCohortAdmissionAndLooks(spec)
}

func validateCohortScope(scope CohortScope) error {
	if scope.OwnerID == uuid.Nil || !safeCohortID(scope.ProviderID, maxProviderIDLength) || !safeCohortID(scope.ModelID, maxModelIDLength) || !safeCohortID(scope.PolicyVersion, maxPolicyVersionIDLength) || scope.PolicyEpoch == 0 || scope.SnapshotID == uuid.Nil || !validSHA256(scope.SnapshotSHA256) || scope.Environment != EvaluationProductionCanary {
		return errors.New("adaptive cohort scope is invalid")
	}
	return nil
}

func validateCohortArms(arms []CohortArm) error {
	if len(arms) < 2 || !slices.IsSortedFunc(arms, func(a, b CohortArm) int { return strings.Compare(a.ArmID, b.ArmID) }) {
		return errors.New("adaptive cohort arms must be ordered and randomized")
	}
	total := 0.0
	for i, arm := range arms {
		if !safeCohortID(arm.ArmID, maxArmIDLength) || !finiteProbability(arm.Probability, false) || (i > 0 && arm.ArmID == arms[i-1].ArmID) {
			return errors.New("adaptive cohort arm is invalid")
		}
		total += arm.Probability
	}
	if math.Abs(total-1) > 1e-9 {
		return errors.New("adaptive cohort arm probabilities must total one")
	}
	return nil
}

func validateCohortActions(actions []ActionProbability) error {
	if len(actions) < 2 || !slices.IsSortedFunc(actions, func(a, b ActionProbability) int { return strings.Compare(a.ActionID, b.ActionID) }) {
		return errors.New("adaptive cohort actions must be ordered and supported")
	}
	total, static := 0.0, false
	for i, action := range actions {
		if action.ActionID == ActionNoneID || !safeCohortID(action.ActionID, maxActionIDLength) || !finiteProbability(action.Probability, false) || (i > 0 && action.ActionID == actions[i-1].ActionID) {
			return errors.New("adaptive cohort action is invalid")
		}
		static = static || action.ActionID == StaticActionID
		total += action.Probability
	}
	if !static || math.Abs(total-1) > 1e-9 {
		return errors.New("adaptive cohort action catalog is invalid")
	}
	return nil
}

func validateCohortEvaluators(registrations []EvaluatorRegistration, quality, harm CohortEndpoint) error {
	if !validCohortEndpoint(quality, CohortBinaryQuality) || !validCohortEndpoint(harm, CohortBinaryHarm) {
		return errors.New("adaptive cohort primary endpoint is invalid")
	}
	if len(registrations) == 0 || !slices.IsSortedFunc(registrations, func(a, b EvaluatorRegistration) int {
		if compare := strings.Compare(string(a.Kind), string(b.Kind)); compare != 0 {
			return compare
		}
		return strings.Compare(a.ID, b.ID)
	}) {
		return errors.New("adaptive cohort evaluator catalog is invalid")
	}
	qualityOK, harmOK := false, false
	for i, registration := range registrations {
		if err := validateEvaluatorRegistration(registration); err != nil {
			return err
		}
		if !safeCohortID(registration.ID, maxEvaluatorIDLength) ||
			!safeCohortID(registration.Version, maxEvaluatorVersionLength) ||
			!safeCohortID(registration.RubricID, maxRubricIDLength) ||
			!safeCohortID(registration.CalibrationID, maxCalibrationIDLength) {
			return errors.New("adaptive cohort evaluator catalog identifier is invalid")
		}
		if i > 0 && registration.Kind == registrations[i-1].Kind && registration.ID == registrations[i-1].ID {
			return errors.New("adaptive cohort evaluator is duplicated")
		}
		provenance := registration.Provenance()
		qualityOK = qualityOK || provenance == quality.Evaluator
		harmOK = harmOK || provenance == harm.Evaluator
	}
	if !qualityOK || !harmOK {
		return errors.New("adaptive cohort primary evaluator is not registered")
	}
	return nil
}

func validCohortEndpoint(endpoint CohortEndpoint, kind CohortEndpointKind) bool {
	return endpoint.Kind == kind && endpoint.ID == endpoint.Evaluator.RubricID && safeCohortID(endpoint.ID, maxEvaluatorIDLength)
}

func validateCohortNumbers(spec CohortSpec) error {
	power := spec.Power
	if !unitInterval(power.BaselineQualityRate) || !unitInterval(power.BaselineHarmRate) ||
		!positiveFinite(power.MinimumDetectableLift) ||
		power.BaselineQualityRate+power.MinimumDetectableLift > 1 ||
		power.ExpectedMembers == 0 || power.MinimumMembers == 0 ||
		power.MinimumMembers > power.ExpectedMembers ||
		!finiteProbability(power.MinimumArmSupport, false) ||
		!validSHA256(power.SimulationSHA256) {
		return errors.New("adaptive cohort power plan is invalid")
	}
	for _, arm := range spec.Arms {
		if arm.Probability < power.MinimumArmSupport {
			return errors.New("adaptive cohort arm support is insufficient")
		}
	}
	if !nonnegativeFinite(spec.Margins.MinimumQualityUplift) ||
		spec.Margins.MinimumQualityUplift >= 1 ||
		power.BaselineQualityRate+spec.Margins.MinimumQualityUplift >= 1 ||
		!finiteProbability(spec.Margins.MaximumHarmIncrease, true) ||
		spec.Margins.MaximumHarmIncrease >= 1 ||
		power.BaselineHarmRate+spec.Margins.MaximumHarmIncrease > 1 ||
		!finiteProbability(spec.Censoring.MaxCensorRate, true) ||
		spec.Censoring.MaxCensorRate >= 1 ||
		!nonnegativeFinite(spec.Censoring.MaxCensorImbalance) ||
		spec.Censoring.MaxCensorImbalance >= 1 || spec.Cutoff.IsZero() {
		return errors.New("adaptive cohort thresholds are invalid")
	}
	return nil
}

func validateCohortAdmissionAndLooks(spec CohortSpec) error {
	admission := spec.Admission
	if admission.Interference != InterferenceNoneBetweenUnits || len(admission.BlockKeys) == 0 || !slices.IsSorted(admission.BlockKeys) || admission.TimeBlockSeconds == 0 || !slices.Contains(admission.BlockKeys, BlockTimeBlock) {
		return errors.New("adaptive cohort admission plan is invalid")
	}
	for i, key := range admission.BlockKeys {
		if (key != BlockEpisode && key != BlockOwner && key != BlockSession && key != BlockTimeBlock) || (i > 0 && key == admission.BlockKeys[i-1]) {
			return errors.New("adaptive cohort block key is invalid")
		}
	}
	if len(spec.Looks) != CohortPlannedLooks {
		return errors.New("adaptive cohort must have five planned looks")
	}
	total := 0.0
	for i, look := range spec.Looks {
		if look.Look != uint8(i+1) || !finiteProbability(look.Alpha, false) {
			return errors.New("adaptive cohort alpha look is invalid")
		}
		total += look.Alpha
	}
	if total > CohortTotalAlpha+1e-12 {
		return errors.New("adaptive cohort alpha budget is exceeded")
	}
	return nil
}

func canonicalCohortHash(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal canonical cohort: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}
func safeCohortID(value string, maximum int) bool {
	if !validASCIIID(value, maximum) || value != strings.ToLower(value) {
		return false
	}
	return !sensitiveCatalogID(value)
}
func unitInterval(value float64) bool { return finiteProbability(value, true) }
func positiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
func nonnegativeFinite(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
