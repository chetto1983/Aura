package adaptive

import (
	"context"
	"errors"
	"testing"
)

func TestOutcomeRecorderRejectsNilDependencies(t *testing.T) {
	t.Parallel()
	registration := validEvaluatorRegistration(EvaluatorDeterministic)
	catalog, err := NewEvaluatorCatalog(registration)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewOutcomeRecorder(nil, catalog); err == nil {
		t.Fatal("NewOutcomeRecorder accepted nil store")
	}

	var recorder *OutcomeRecorder
	if _, err := recorder.RecordOutcome(
		context.Background(),
		OutcomeObservation{},
	); !errors.Is(err, ErrOutcomeRecorderUnavailable) {
		t.Fatalf("nil RecordOutcome error = %v, want ErrOutcomeRecorderUnavailable", err)
	}
	if _, err := recorder.RecordCorrection(
		context.Background(),
		CorrectionObservation{},
	); !errors.Is(err, ErrOutcomeRecorderUnavailable) {
		t.Fatalf("nil RecordCorrection error = %v, want ErrOutcomeRecorderUnavailable", err)
	}
}
