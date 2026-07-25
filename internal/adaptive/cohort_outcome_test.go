package adaptive

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

func focalCohortOutcome(cohort *FocalCohort, evaluator EvaluatorProvenance) OutcomeObservation {
	scope := cohort.Scope()
	zero := 0.0
	return OutcomeObservation{
		SchemaVersion:       SchemaVersion2,
		AssignmentID:        uuid.New(),
		OwnerID:             scope.OwnerID,
		Domain:              cohort.Domain(),
		ProviderID:          scope.ProviderID,
		ModelID:             scope.ModelID,
		Evaluator:           evaluator,
		SourceObservationID: uuid.New(),
		Status:              OutcomeObserved,
		Measures:            OutcomeMeasures{Quality: &zero, Harm: &zero},
		ObservedAt:          time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC),
	}
}

func TestNewFocalCohort_RejectsEndpointWhoseIDDoesNotMatchEvaluatorRubric(t *testing.T) {
	spec := focalCohortTestSpec()
	spec.PrimaryQuality.ID = "another-rubric"

	if _, err := NewFocalCohort(spec); err == nil {
		t.Fatal("NewFocalCohort() accepted endpoint ID that differs from its evaluator rubric")
	}
}

func TestFocalCohort_RejectsPrimaryOutcomeForUninitializedCohort(t *testing.T) {
	var cohort FocalCohort
	value := 0.0
	observation := OutcomeObservation{
		Status:   OutcomeObserved,
		Measures: OutcomeMeasures{Quality: &value},
	}

	if err := cohort.ValidatePrimaryQualityOutcome(observation); err == nil {
		t.Fatal("zero-value cohort accepted primary outcome evidence")
	}
}

func TestFocalCohort_ValidatesPrimaryBinaryOutcomeEvidence(t *testing.T) {
	cohort := mustFocalCohort(t, focalCohortTestSpec())
	quality := focalCohortOutcome(cohort, cohort.PrimaryQualityEndpoint().Evaluator)
	harm := focalCohortOutcome(cohort, cohort.PrimaryHarmEndpoint().Evaluator)

	for _, value := range []float64{0, 1} {
		*quality.Measures.Quality = value
		if err := cohort.ValidatePrimaryQualityOutcome(quality); err != nil {
			t.Fatalf("ValidatePrimaryQualityOutcome(%g) error = %v", value, err)
		}
		*harm.Measures.Harm = value
		if err := cohort.ValidatePrimaryHarmOutcome(harm); err != nil {
			t.Fatalf("ValidatePrimaryHarmOutcome(%g) error = %v", value, err)
		}
	}
}

func TestFocalCohort_RejectsPrimaryOutcomeEvidenceOutsideFrozenBoundary(t *testing.T) {
	cohort := mustFocalCohort(t, focalCohortTestSpec())
	quality := focalCohortOutcome(cohort, cohort.PrimaryQualityEndpoint().Evaluator)
	harm := focalCohortOutcome(cohort, cohort.PrimaryHarmEndpoint().Evaluator)

	for _, testCase := range []struct {
		name     string
		outcome  OutcomeObservation
		validate func(OutcomeObservation) error
	}{
		{"quality owner", changedOutcome(quality, func(o *OutcomeObservation) { o.OwnerID = uuid.New() }), cohort.ValidatePrimaryQualityOutcome},
		{"quality domain", changedOutcome(quality, func(o *OutcomeObservation) { o.Domain = DomainReasoning }), cohort.ValidatePrimaryQualityOutcome},
		{"quality provider", changedOutcome(quality, func(o *OutcomeObservation) { o.ProviderID = "other" }), cohort.ValidatePrimaryQualityOutcome},
		{"quality model", changedOutcome(quality, func(o *OutcomeObservation) { o.ModelID = "other" }), cohort.ValidatePrimaryQualityOutcome},
		{"quality evaluator", changedOutcome(quality, func(o *OutcomeObservation) { o.Evaluator.Version = "other" }), cohort.ValidatePrimaryQualityOutcome},
		{"quality censored", changedOutcome(quality, func(o *OutcomeObservation) { o.Status = OutcomeCensored }), cohort.ValidatePrimaryQualityOutcome},
		{"quality missing", changedOutcome(quality, func(o *OutcomeObservation) { o.Measures.Quality = nil }), cohort.ValidatePrimaryQualityOutcome},
		{"quality nonbinary", changedOutcome(quality, func(o *OutcomeObservation) { value := 0.5; o.Measures.Quality = &value }), cohort.ValidatePrimaryQualityOutcome},
		{"quality nan", changedOutcome(quality, func(o *OutcomeObservation) { value := math.NaN(); o.Measures.Quality = &value }), cohort.ValidatePrimaryQualityOutcome},
		{"harm evaluator rubric", changedOutcome(harm, func(o *OutcomeObservation) { o.Evaluator.RubricID = "other" }), cohort.ValidatePrimaryHarmOutcome},
		{"harm missing", changedOutcome(harm, func(o *OutcomeObservation) { o.Measures.Harm = nil }), cohort.ValidatePrimaryHarmOutcome},
		{"harm nonbinary", changedOutcome(harm, func(o *OutcomeObservation) { value := 0.5; o.Measures.Harm = &value }), cohort.ValidatePrimaryHarmOutcome},
		{"harm nan", changedOutcome(harm, func(o *OutcomeObservation) { value := math.NaN(); o.Measures.Harm = &value }), cohort.ValidatePrimaryHarmOutcome},
		{"harm censored", changedOutcome(harm, func(o *OutcomeObservation) { o.Status = OutcomeCensored }), cohort.ValidatePrimaryHarmOutcome},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.validate(testCase.outcome); err == nil {
				t.Fatal("primary outcome evidence was accepted outside the frozen cohort boundary")
			}
		})
	}
}

func changedOutcome(outcome OutcomeObservation, change func(*OutcomeObservation)) OutcomeObservation {
	clone := outcome
	if outcome.Measures.Quality != nil {
		value := *outcome.Measures.Quality
		clone.Measures.Quality = &value
	}
	if outcome.Measures.Harm != nil {
		value := *outcome.Measures.Harm
		clone.Measures.Harm = &value
	}
	change(&clone)
	return clone
}
