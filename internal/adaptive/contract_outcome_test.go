package adaptive

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEvaluatorCatalogRejectsUntrustedAndOperationalRegistrations(t *testing.T) {
	t.Parallel()
	valid := validEvaluatorRegistration(EvaluatorDeterministic)

	if _, err := NewEvaluatorCatalog(valid); err != nil {
		t.Fatalf("NewEvaluatorCatalog(valid): %v", err)
	}
	if _, err := NewEvaluatorCatalog(valid, valid); !errors.Is(err, ErrEvaluatorRegistrationInvalid) {
		t.Fatalf("duplicate registration error = %v, want ErrEvaluatorRegistrationInvalid", err)
	}
	for _, kind := range []EvaluatorKind{EvaluatorOperational, EvaluatorSelfReport} {
		registration := valid
		registration.Kind = kind
		if _, err := NewEvaluatorCatalog(registration); !errors.Is(err, ErrOutcomeIneligible) {
			t.Fatalf("NewEvaluatorCatalog(%s) error = %v, want ErrOutcomeIneligible", kind, err)
		}
	}
}

func TestNewOutcomeEventBindsRegisteredEvaluatorAndAssignment(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	registration := validEvaluatorRegistration(EvaluatorDeterministic)
	catalog, err := NewEvaluatorCatalog(registration)
	if err != nil {
		t.Fatal(err)
	}
	observation := validOutcomeObservation(assignment, registration)

	first, err := NewOutcomeEvent(assignment, observation, catalog)
	if err != nil {
		t.Fatalf("NewOutcomeEvent: %v", err)
	}
	second, err := NewOutcomeEvent(assignment, observation, catalog)
	if err != nil {
		t.Fatalf("NewOutcomeEvent retry: %v", err)
	}
	if first.ID != second.ID || first.Kind != EventOutcome ||
		first.OwnerID != assignment.OwnerID ||
		first.DecisionID != assignment.AssignmentID ||
		first.AggregateID != assignment.RequestID.String() {
		t.Fatalf("outcome event binding = %+v, retry = %+v", first, second)
	}
	if !first.CreatedAt.Equal(observation.ObservedAt) {
		t.Fatalf("event created_at = %s, want observed_at %s", first.CreatedAt, observation.ObservedAt)
	}
	var payload map[string]any
	if err := json.Unmarshal(first.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"prompt", "query", "content", "arguments", "reasoning"} {
		if _, exists := payload[forbidden]; exists {
			t.Fatalf("outcome payload exposed forbidden key %q", forbidden)
		}
	}

	unregistered := observation
	unregistered.Evaluator.Version = "checker-v2"
	if _, err := NewOutcomeEvent(
		assignment,
		unregistered,
		catalog,
	); !errors.Is(err, ErrEvaluatorNotRegistered) {
		t.Fatalf("unregistered evaluator error = %v, want ErrEvaluatorNotRegistered", err)
	}
}

func TestNewOutcomeEventRejectsInvalidStatusMetricsAndScope(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	registration := validEvaluatorRegistration(EvaluatorCalibratedJudge)
	catalog, err := NewEvaluatorCatalog(registration)
	if err != nil {
		t.Fatal(err)
	}
	valid := validOutcomeObservation(assignment, registration)

	tests := []struct {
		name   string
		mutate func(*OutcomeObservation)
		target error
	}{
		{
			name: "owner",
			mutate: func(observation *OutcomeObservation) {
				observation.OwnerID = uuid.Must(uuid.NewV7())
			},
			target: ErrOutcomeAssignmentMismatch,
		},
		{
			name: "provider",
			mutate: func(observation *OutcomeObservation) {
				observation.ProviderID = "other-provider"
			},
			target: ErrOutcomeAssignmentMismatch,
		},
		{
			name: "quality",
			mutate: func(observation *OutcomeObservation) {
				quality := 1.01
				observation.Measures.Quality = &quality
			},
			target: ErrOutcomeObservationInvalid,
		},
		{
			name: "censored metrics",
			mutate: func(observation *OutcomeObservation) {
				observation.Status = OutcomeCensored
			},
			target: ErrOutcomeObservationInvalid,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			observation := valid
			test.mutate(&observation)
			if _, err := NewOutcomeEvent(
				assignment,
				observation,
				catalog,
			); !errors.Is(err, test.target) {
				t.Fatalf("NewOutcomeEvent error = %v, want %v", err, test.target)
			}
		})
	}
}

func TestNewCorrectionEventUsesStableSourceAndCurrentFactIdentity(t *testing.T) {
	t.Parallel()
	assignment := validAssignment()
	registration := validEvaluatorRegistration(EvaluatorDeterministic)
	catalog, err := NewEvaluatorCatalog(registration)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := NewOutcomeEvent(
		assignment,
		validOutcomeObservation(assignment, registration),
		catalog,
	)
	if err != nil {
		t.Fatal(err)
	}
	correction := validCorrectionObservation(assignment, registration, outcome.ID)
	first, err := NewCorrectionEvent(assignment, correction, catalog)
	if err != nil {
		t.Fatalf("NewCorrectionEvent: %v", err)
	}
	second, err := NewCorrectionEvent(assignment, correction, catalog)
	if err != nil {
		t.Fatalf("NewCorrectionEvent retry: %v", err)
	}
	if first.ID != second.ID || first.Kind != EventCorrection {
		t.Fatalf("correction event = %+v, retry = %+v", first, second)
	}
	changedTarget := correction
	changedTarget.SupersedesEventID = uuid.Must(uuid.NewV7())
	conflict, err := NewCorrectionEvent(assignment, changedTarget, catalog)
	if err != nil {
		t.Fatalf("NewCorrectionEvent changed target: %v", err)
	}
	if conflict.ID != first.ID {
		t.Fatalf("correction source generated id %s after target change, want stable %s", conflict.ID, first.ID)
	}
}

func validEvaluatorRegistration(kind EvaluatorKind) EvaluatorRegistration {
	return EvaluatorRegistration{
		Kind:             kind,
		ID:               "reasoning-answer-checker",
		Version:          "checker-v1",
		RubricID:         "binary-correctness-v1",
		CalibrationID:    "heldout-2026-07-v1",
		ProvenanceSHA256: strings.Repeat("d", 64),
	}
}

func validOutcomeObservation(
	assignment Assignment,
	registration EvaluatorRegistration,
) OutcomeObservation {
	quality, harm := 1.0, 0.0
	latency, tokens, cost := uint64(125), uint64(42), uint64(7)
	return OutcomeObservation{
		SchemaVersion:       SchemaVersion2,
		AssignmentID:        assignment.AssignmentID,
		OwnerID:             assignment.OwnerID,
		Domain:              assignment.Domain,
		ProviderID:          assignment.ProviderID,
		ModelID:             assignment.ModelID,
		Evaluator:           registration.Provenance(),
		SourceObservationID: uuid.MustParse("4e440606-f1e1-47d4-9c5e-cd45228b2335"),
		Status:              OutcomeObserved,
		Measures: OutcomeMeasures{
			Quality:        &quality,
			Harm:           &harm,
			LatencyMS:      &latency,
			Tokens:         &tokens,
			CostMicrounits: &cost,
		},
		ObservedAt: time.Date(2026, 7, 24, 16, 30, 0, 123, time.UTC),
	}
}

func validCorrectionObservation(
	assignment Assignment,
	registration EvaluatorRegistration,
	supersedes uuid.UUID,
) CorrectionObservation {
	quality, harm := 0.0, 0.0
	return CorrectionObservation{
		SchemaVersion:      SchemaVersion2,
		AssignmentID:       assignment.AssignmentID,
		OwnerID:            assignment.OwnerID,
		Domain:             assignment.Domain,
		ProviderID:         assignment.ProviderID,
		ModelID:            assignment.ModelID,
		Evaluator:          registration.Provenance(),
		CorrectionSourceID: uuid.MustParse("14948bc8-f9ea-4380-a8ba-30544696def8"),
		SupersedesEventID:  supersedes,
		Status:             OutcomeObserved,
		Measures: OutcomeMeasures{
			Quality: &quality,
			Harm:    &harm,
		},
		ObservedAt: time.Date(2026, 7, 24, 17, 0, 0, 0, time.UTC),
	}
}
