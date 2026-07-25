package adaptive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	maxEvaluatorIDLength      = 128
	maxEvaluatorVersionLength = 128
	maxRubricIDLength         = 128
	maxCalibrationIDLength    = 128
	maxOutcomeLatencyMS       = uint64(86_400_000)
	maxOutcomeTokens          = uint64(1_000_000_000)
	maxOutcomeCostMicrounits  = uint64(1_000_000_000_000)
)

var (
	// ErrEvaluatorRegistrationInvalid rejects malformed or duplicate trusted evaluator metadata.
	ErrEvaluatorRegistrationInvalid = errors.New("adaptive evaluator registration is invalid")
	// ErrEvaluatorNotRegistered rejects evidence whose provenance is absent from the frozen catalog.
	ErrEvaluatorNotRegistered = errors.New("adaptive evaluator is not registered")
	// ErrOutcomeIneligible rejects operational and model-self-report evaluator kinds.
	ErrOutcomeIneligible = errors.New("adaptive outcome is not eligible")
	// ErrOutcomeObservationInvalid rejects malformed or unbounded outcome facts.
	ErrOutcomeObservationInvalid = errors.New("adaptive outcome observation is invalid")
	// ErrOutcomeAssignmentMismatch rejects facts outside their persisted assignment scope.
	ErrOutcomeAssignmentMismatch = errors.New("adaptive outcome does not match assignment")
	// ErrCorrectionObservationInvalid rejects malformed correction facts.
	ErrCorrectionObservationInvalid = errors.New("adaptive correction observation is invalid")
)

// EvaluatorKind identifies the independent source of an observed outcome.
type EvaluatorKind string

// Eligible and diagnostic evaluator kinds remain stable in persisted facts.
const (
	EvaluatorDeterministic   EvaluatorKind = "deterministic"
	EvaluatorHuman           EvaluatorKind = "human"
	EvaluatorCalibratedJudge EvaluatorKind = "calibrated_judge"
	EvaluatorSelfReport      EvaluatorKind = "self_report"
	EvaluatorOperational     EvaluatorKind = "operational_proxy"
)

// OutcomeStatus distinguishes independently observed facts from explicit censoring.
type OutcomeStatus string

// Outcome statuses remain stable in persisted facts.
const (
	OutcomeObserved OutcomeStatus = "observed"
	OutcomeCensored OutcomeStatus = "censored"
)

// EvaluatorProvenance binds an outcome to one registered immutable evaluator artifact.
type EvaluatorProvenance struct {
	Kind             EvaluatorKind `json:"kind"`
	ID               string        `json:"id"`
	Version          string        `json:"version"`
	RubricID         string        `json:"rubric_id"`
	CalibrationID    string        `json:"calibration_id"`
	ProvenanceSHA256 string        `json:"provenance_sha256"`
}

// EvaluatorRegistration mints trusted provenance in an immutable evaluator catalog.
type EvaluatorRegistration struct {
	Kind             EvaluatorKind
	ID               string
	Version          string
	RubricID         string
	CalibrationID    string
	ProvenanceSHA256 string
}

// Provenance returns the persisted representation of a trusted registration.
func (registration EvaluatorRegistration) Provenance() EvaluatorProvenance {
	return EvaluatorProvenance(registration)
}

type evaluatorCatalogKey struct {
	kind EvaluatorKind
	id   string
}

// EvaluatorCatalog is the immutable set of trusted evaluator artifacts.
type EvaluatorCatalog struct {
	registrations map[evaluatorCatalogKey]EvaluatorRegistration
}

// NewEvaluatorCatalog freezes the exact evaluator provenance accepted by a recorder.
func NewEvaluatorCatalog(registrations ...EvaluatorRegistration) (EvaluatorCatalog, error) {
	catalog := EvaluatorCatalog{
		registrations: make(map[evaluatorCatalogKey]EvaluatorRegistration, len(registrations)),
	}
	for _, registration := range registrations {
		if err := validateEvaluatorRegistration(registration); err != nil {
			return EvaluatorCatalog{}, err
		}
		key := evaluatorCatalogKey{kind: registration.Kind, id: registration.ID}
		if _, exists := catalog.registrations[key]; exists {
			return EvaluatorCatalog{}, fmt.Errorf(
				"%w: duplicate %s/%s",
				ErrEvaluatorRegistrationInvalid,
				registration.Kind,
				registration.ID,
			)
		}
		catalog.registrations[key] = registration
	}
	return catalog, nil
}

func validateEvaluatorRegistration(registration EvaluatorRegistration) error {
	if !registration.Kind.eligible() {
		if registration.Kind == EvaluatorOperational || registration.Kind == EvaluatorSelfReport {
			return fmt.Errorf("%w: evaluator kind %q", ErrOutcomeIneligible, registration.Kind)
		}
		return fmt.Errorf("%w: evaluator kind %q", ErrEvaluatorRegistrationInvalid, registration.Kind)
	}
	switch {
	case !validASCIIID(registration.ID, maxEvaluatorIDLength):
		return fmt.Errorf("%w: evaluator id", ErrEvaluatorRegistrationInvalid)
	case !validASCIIID(registration.Version, maxEvaluatorVersionLength):
		return fmt.Errorf("%w: evaluator version", ErrEvaluatorRegistrationInvalid)
	case !validASCIIID(registration.RubricID, maxRubricIDLength):
		return fmt.Errorf("%w: evaluator rubric", ErrEvaluatorRegistrationInvalid)
	case !validASCIIID(registration.CalibrationID, maxCalibrationIDLength):
		return fmt.Errorf("%w: evaluator calibration", ErrEvaluatorRegistrationInvalid)
	case !validSHA256(registration.ProvenanceSHA256):
		return fmt.Errorf("%w: evaluator provenance hash", ErrEvaluatorRegistrationInvalid)
	}
	return nil
}

func (catalog EvaluatorCatalog) validate(provenance EvaluatorProvenance) error {
	key := evaluatorCatalogKey{kind: provenance.Kind, id: provenance.ID}
	registration, exists := catalog.registrations[key]
	if !exists || registration.Provenance() != provenance {
		return ErrEvaluatorNotRegistered
	}
	return nil
}

// OutcomeMeasures contains bounded quality, harm, and optional operational covariates.
type OutcomeMeasures struct {
	Quality        *float64 `json:"quality"`
	Harm           *float64 `json:"harm"`
	LatencyMS      *uint64  `json:"latency_ms"`
	Tokens         *uint64  `json:"tokens"`
	CostMicrounits *uint64  `json:"cost_microunits"`
}

// OutcomeObservation is one typed terminal evaluator fact for an assignment.
type OutcomeObservation struct {
	SchemaVersion       string              `json:"schema_version"`
	AssignmentID        uuid.UUID           `json:"assignment_id"`
	OwnerID             uuid.UUID           `json:"owner_id"`
	Domain              Domain              `json:"domain"`
	ProviderID          string              `json:"provider_id"`
	ModelID             string              `json:"model_id"`
	Evaluator           EvaluatorProvenance `json:"evaluator"`
	SourceObservationID uuid.UUID           `json:"source_observation_id"`
	Status              OutcomeStatus       `json:"status"`
	Measures            OutcomeMeasures     `json:"measures"`
	ObservedAt          time.Time           `json:"observed_at"`
}

// CorrectionObservation replaces the effective leaf of one evaluator outcome chain.
type CorrectionObservation struct {
	SchemaVersion      string              `json:"schema_version"`
	AssignmentID       uuid.UUID           `json:"assignment_id"`
	OwnerID            uuid.UUID           `json:"owner_id"`
	Domain             Domain              `json:"domain"`
	ProviderID         string              `json:"provider_id"`
	ModelID            string              `json:"model_id"`
	Evaluator          EvaluatorProvenance `json:"evaluator"`
	CorrectionSourceID uuid.UUID           `json:"correction_source_id"`
	SupersedesEventID  uuid.UUID           `json:"supersedes_event_id"`
	Status             OutcomeStatus       `json:"status"`
	Measures           OutcomeMeasures     `json:"measures"`
	ObservedAt         time.Time           `json:"observed_at"`
}

// NewOutcomeEvent constructs a registered typed outcome bound to an assignment.
func NewOutcomeEvent(
	assignment Assignment,
	observation OutcomeObservation,
	catalog EvaluatorCatalog,
) (Event, error) {
	assignment = canonicalAssignment(assignment)
	observation = canonicalOutcomeObservation(observation)
	if err := validateAssignment(assignment); err != nil {
		return Event{}, fmt.Errorf("validate adaptive outcome assignment: %w", err)
	}
	if err := validateOutcomeObservation(observation, catalog); err != nil {
		return Event{}, err
	}
	if err := validateOutcomeAssignment(assignment, observation); err != nil {
		return Event{}, err
	}
	return newTypedOutcomeEvent(
		assignment,
		observation,
		EventOutcome,
		observation.Evaluator,
		observation.SourceObservationID,
		observation.ObservedAt,
	)
}

// NewCorrectionEvent constructs a registered typed correction bound to an assignment.
func NewCorrectionEvent(
	assignment Assignment,
	correction CorrectionObservation,
	catalog EvaluatorCatalog,
) (Event, error) {
	assignment = canonicalAssignment(assignment)
	correction = canonicalCorrectionObservation(correction)
	if err := validateAssignment(assignment); err != nil {
		return Event{}, fmt.Errorf("validate adaptive correction assignment: %w", err)
	}
	if err := validateCorrectionObservation(correction, catalog); err != nil {
		return Event{}, err
	}
	if err := validateCorrectionAssignment(assignment, correction); err != nil {
		return Event{}, err
	}
	return newTypedOutcomeEvent(
		assignment,
		correction,
		EventCorrection,
		correction.Evaluator,
		correction.CorrectionSourceID,
		correction.ObservedAt,
	)
}

func newTypedOutcomeEvent(
	assignment Assignment,
	fact any,
	kind EventKind,
	evaluator EvaluatorProvenance,
	sourceID uuid.UUID,
	observedAt time.Time,
) (Event, error) {
	payload, err := json.Marshal(fact)
	if err != nil {
		return Event{}, fmt.Errorf("marshal adaptive %s: %w", kind, err)
	}
	return newEvent(EventParams{
		ID: eventIDForSource(
			assignment.AssignmentID,
			kind,
			evaluatorSourceIdentity(evaluator, sourceID),
		),
		OwnerID:     assignment.OwnerID,
		AggregateID: assignment.RequestID.String(),
		DecisionID:  assignment.AssignmentID,
		Kind:        kind,
		Payload:     payload,
		CreatedAt:   observedAt,
	})
}

func validateOutcomeObservation(
	observation OutcomeObservation,
	catalog EvaluatorCatalog,
) error {
	switch {
	case observation.SchemaVersion != SchemaVersion2:
		return fmt.Errorf("%w: schema_version", ErrOutcomeObservationInvalid)
	case observation.AssignmentID == uuid.Nil:
		return fmt.Errorf("%w: assignment_id", ErrOutcomeObservationInvalid)
	case observation.OwnerID == uuid.Nil:
		return fmt.Errorf("%w: owner_id", ErrOutcomeObservationInvalid)
	case !observation.Domain.valid():
		return fmt.Errorf("%w: domain", ErrOutcomeObservationInvalid)
	case !validASCIIID(observation.ProviderID, maxProviderIDLength):
		return fmt.Errorf("%w: provider_id", ErrOutcomeObservationInvalid)
	case !validASCIIID(observation.ModelID, maxModelIDLength):
		return fmt.Errorf("%w: model_id", ErrOutcomeObservationInvalid)
	case observation.SourceObservationID == uuid.Nil:
		return fmt.Errorf("%w: source_observation_id", ErrOutcomeObservationInvalid)
	case observation.ObservedAt.IsZero():
		return fmt.Errorf("%w: observed_at", ErrOutcomeObservationInvalid)
	}
	if err := catalog.validate(observation.Evaluator); err != nil {
		return err
	}
	return validateOutcomeMeasures(observation.Status, observation.Measures, ErrOutcomeObservationInvalid)
}

func validateCorrectionObservation(
	correction CorrectionObservation,
	catalog EvaluatorCatalog,
) error {
	observation := OutcomeObservation{
		SchemaVersion: correction.SchemaVersion,
		AssignmentID:  correction.AssignmentID,
		OwnerID:       correction.OwnerID,
		Domain:        correction.Domain,
		ProviderID:    correction.ProviderID,
		ModelID:       correction.ModelID,
		Evaluator:     correction.Evaluator,
		Status:        correction.Status,
		Measures:      correction.Measures,
		ObservedAt:    correction.ObservedAt,
	}
	switch {
	case correction.CorrectionSourceID == uuid.Nil:
		return fmt.Errorf("%w: correction_source_id", ErrCorrectionObservationInvalid)
	case correction.SupersedesEventID == uuid.Nil:
		return fmt.Errorf("%w: supersedes_event_id", ErrCorrectionObservationInvalid)
	}
	if err := validateOutcomeObservationWithoutSource(observation, catalog); err != nil {
		if errors.Is(err, ErrOutcomeObservationInvalid) {
			return fmt.Errorf("%w: %v", ErrCorrectionObservationInvalid, err)
		}
		return err
	}
	return nil
}

func validateOutcomeObservationWithoutSource(
	observation OutcomeObservation,
	catalog EvaluatorCatalog,
) error {
	observation.SourceObservationID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	return validateOutcomeObservation(observation, catalog)
}

func validateOutcomeAssignment(assignment Assignment, observation OutcomeObservation) error {
	if assignment.AssignmentID != observation.AssignmentID ||
		assignment.OwnerID != observation.OwnerID ||
		assignment.Domain != observation.Domain ||
		assignment.ProviderID != observation.ProviderID ||
		assignment.ModelID != observation.ModelID {
		return ErrOutcomeAssignmentMismatch
	}
	return nil
}

func validateCorrectionAssignment(assignment Assignment, correction CorrectionObservation) error {
	return validateOutcomeAssignment(assignment, OutcomeObservation{
		AssignmentID: correction.AssignmentID,
		OwnerID:      correction.OwnerID,
		Domain:       correction.Domain,
		ProviderID:   correction.ProviderID,
		ModelID:      correction.ModelID,
	})
}

func validateOutcomeMeasures(status OutcomeStatus, measures OutcomeMeasures, target error) error {
	if status != OutcomeObserved && status != OutcomeCensored {
		return fmt.Errorf("%w: status", target)
	}
	if status == OutcomeCensored {
		if measures.Quality != nil || measures.Harm != nil || measures.LatencyMS != nil ||
			measures.Tokens != nil || measures.CostMicrounits != nil {
			return fmt.Errorf("%w: censored observation contains measures", target)
		}
		return nil
	}
	if measures.Quality == nil || measures.Harm == nil ||
		!finiteUnitInterval(*measures.Quality) || !finiteUnitInterval(*measures.Harm) {
		return fmt.Errorf("%w: observed quality and harm", target)
	}
	if measures.LatencyMS != nil && *measures.LatencyMS > maxOutcomeLatencyMS {
		return fmt.Errorf("%w: latency_ms", target)
	}
	if measures.Tokens != nil && *measures.Tokens > maxOutcomeTokens {
		return fmt.Errorf("%w: tokens", target)
	}
	if measures.CostMicrounits != nil && *measures.CostMicrounits > maxOutcomeCostMicrounits {
		return fmt.Errorf("%w: cost_microunits", target)
	}
	return nil
}

func canonicalOutcomeObservation(observation OutcomeObservation) OutcomeObservation {
	observation.ObservedAt = observation.ObservedAt.Round(0).UTC()
	observation.Measures = canonicalOutcomeMeasures(observation.Measures)
	return observation
}

func canonicalCorrectionObservation(correction CorrectionObservation) CorrectionObservation {
	correction.ObservedAt = correction.ObservedAt.Round(0).UTC()
	correction.Measures = canonicalOutcomeMeasures(correction.Measures)
	return correction
}

func canonicalOutcomeMeasures(measures OutcomeMeasures) OutcomeMeasures {
	if measures.Quality != nil {
		value := canonicalZero(*measures.Quality)
		measures.Quality = &value
	}
	if measures.Harm != nil {
		value := canonicalZero(*measures.Harm)
		measures.Harm = &value
	}
	return measures
}

func evaluatorSourceIdentity(provenance EvaluatorProvenance, sourceID uuid.UUID) string {
	identity := strings.Join([]string{
		string(provenance.Kind),
		provenance.ID,
		provenance.Version,
		provenance.RubricID,
		provenance.CalibrationID,
		provenance.ProvenanceSHA256,
		sourceID.String(),
	}, "\x00")
	sum := sha256.Sum256([]byte(identity))
	return "evaluator:" + hex.EncodeToString(sum[:])
}

func finiteUnitInterval(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func (kind EvaluatorKind) eligible() bool {
	return kind == EvaluatorDeterministic ||
		kind == EvaluatorHuman ||
		kind == EvaluatorCalibratedJudge
}
