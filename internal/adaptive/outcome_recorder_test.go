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

func TestBenchmarkEvaluatorConstructorsOnlyAcceptTheirTrustedKinds(t *testing.T) {
	t.Parallel()
	deterministic := validEvaluatorRegistration(EvaluatorDeterministic)
	judge := validEvaluatorRegistration(EvaluatorCalibratedJudge)
	human := validEvaluatorRegistration(EvaluatorHuman)

	if _, err := NewDeterministicBenchmarkEvaluator(nil, deterministic); err == nil {
		t.Fatal("deterministic evaluator accepted nil recorder")
	}
	if _, err := NewCalibratedJudgeBenchmarkEvaluator(&OutcomeRecorder{}, judge); err != nil {
		t.Fatalf("NewCalibratedJudgeBenchmarkEvaluator: %v", err)
	}
	if _, err := NewDeterministicBenchmarkEvaluator(
		&OutcomeRecorder{},
		human,
	); !errors.Is(err, ErrEvaluatorRegistrationInvalid) {
		t.Fatalf("human deterministic adapter error = %v, want ErrEvaluatorRegistrationInvalid", err)
	}
}
