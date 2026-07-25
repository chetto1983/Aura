package adaptive

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
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
	catalog, err := NewEvaluatorCatalog(deterministic, judge)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := NewOutcomeRecorder(
		&Store{pool: new(pgxpool.Pool)},
		catalog,
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := NewDeterministicBenchmarkEvaluator(nil, deterministic); err == nil {
		t.Fatal("deterministic evaluator accepted nil recorder")
	}
	if _, err := NewCalibratedJudgeBenchmarkEvaluator(
		&OutcomeRecorder{},
		judge,
	); !errors.Is(err, ErrOutcomeRecorderUnavailable) {
		t.Fatalf("empty recorder error = %v, want ErrOutcomeRecorderUnavailable", err)
	}
	unregistered := deterministic
	unregistered.ID = "unregistered-checker"
	if _, err := NewDeterministicBenchmarkEvaluator(
		recorder,
		unregistered,
	); !errors.Is(err, ErrEvaluatorNotRegistered) {
		t.Fatalf("unregistered adapter error = %v, want ErrEvaluatorNotRegistered", err)
	}
	if _, err := NewDeterministicBenchmarkEvaluator(
		recorder,
		human,
	); !errors.Is(err, ErrEvaluatorRegistrationInvalid) {
		t.Fatalf("human deterministic adapter error = %v, want ErrEvaluatorRegistrationInvalid", err)
	}
	if _, err := NewDeterministicBenchmarkEvaluator(recorder, deterministic); err != nil {
		t.Fatalf("registered deterministic adapter: %v", err)
	}
	if _, err := NewCalibratedJudgeBenchmarkEvaluator(recorder, judge); err != nil {
		t.Fatalf("registered judge adapter: %v", err)
	}
}
