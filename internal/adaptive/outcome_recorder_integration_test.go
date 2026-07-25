//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOutcomeRecorderPersistsIdempotentOutcomeAndLinearCorrections(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	store := NewStore(pool, StoreConfig{})
	if sequence, err := store.RecordAssignment(ctx, assignment); err != nil || sequence != 1 {
		t.Fatalf("RecordAssignment = (%d, %v), want (1, nil)", sequence, err)
	}
	deterministic := validEvaluatorRegistration(EvaluatorDeterministic)
	judge := validEvaluatorRegistration(EvaluatorCalibratedJudge)
	catalog, err := NewEvaluatorCatalog(deterministic, judge)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := NewOutcomeRecorder(store, catalog)
	if err != nil {
		t.Fatal(err)
	}
	outcome := validOutcomeObservation(assignment, deterministic)
	outcomeSequence, err := recorder.RecordOutcome(ctx, outcome)
	if err != nil || outcomeSequence != 2 {
		t.Fatalf("RecordOutcome = (%d, %v), want (2, nil)", outcomeSequence, err)
	}
	retrySequence, err := recorder.RecordOutcome(ctx, outcome)
	if err != nil || retrySequence != outcomeSequence {
		t.Fatalf("RecordOutcome retry = (%d, %v), want (%d, nil)", retrySequence, err, outcomeSequence)
	}
	conflict := outcome
	quality := 0.5
	conflict.Measures.Quality = &quality
	if _, err := recorder.RecordOutcome(ctx, conflict); !errors.Is(err, ErrPayloadConflict) {
		t.Fatalf("RecordOutcome conflict error = %v, want ErrPayloadConflict", err)
	}
	outcomeEvent, err := NewOutcomeEvent(assignment, outcome, catalog)
	if err != nil {
		t.Fatal(err)
	}
	firstCorrection := validCorrectionObservation(assignment, deterministic, outcomeEvent.ID)
	correctionSequence, err := recorder.RecordCorrection(ctx, firstCorrection)
	if err != nil || correctionSequence != 3 {
		t.Fatalf("RecordCorrection = (%d, %v), want (3, nil)", correctionSequence, err)
	}
	retrySequence, err = recorder.RecordCorrection(ctx, firstCorrection)
	if err != nil || retrySequence != correctionSequence {
		t.Fatalf(
			"RecordCorrection retry = (%d, %v), want (%d, nil)",
			retrySequence,
			err,
			correctionSequence,
		)
	}
	fork := firstCorrection
	fork.CorrectionSourceID = uuid.Must(uuid.NewV7())
	if _, err := recorder.RecordCorrection(ctx, fork); !errors.Is(err, ErrCorrectionFork) {
		t.Fatalf("fork correction error = %v, want ErrCorrectionFork", err)
	}
	crossEvaluator := fork
	crossEvaluator.CorrectionSourceID = uuid.Must(uuid.NewV7())
	crossEvaluator.Evaluator = judge.Provenance()
	if _, err := recorder.RecordCorrection(
		ctx,
		crossEvaluator,
	); !errors.Is(err, ErrCorrectionScopeMismatch) {
		t.Fatalf("cross-evaluator correction error = %v, want ErrCorrectionScopeMismatch", err)
	}
	missing := fork
	missing.CorrectionSourceID = uuid.Must(uuid.NewV7())
	missing.SupersedesEventID = uuid.Must(uuid.NewV7())
	if _, err := recorder.RecordCorrection(
		ctx,
		missing,
	); !errors.Is(err, ErrCorrectionTargetNotFound) {
		t.Fatalf("missing correction error = %v, want ErrCorrectionTargetNotFound", err)
	}
	firstCorrectionEvent, err := NewCorrectionEvent(assignment, firstCorrection, catalog)
	if err != nil {
		t.Fatal(err)
	}
	secondCorrection := firstCorrection
	secondCorrection.CorrectionSourceID = uuid.Must(uuid.NewV7())
	secondCorrection.SupersedesEventID = firstCorrectionEvent.ID
	if sequence, err := recorder.RecordCorrection(
		ctx,
		secondCorrection,
	); err != nil || sequence != 4 {
		t.Fatalf("second RecordCorrection = (%d, %v), want (4, nil)", sequence, err)
	}
	assertAdaptiveNextSequence(t, pool, owner, assignment.RequestID.String(), 5)
}

func TestOutcomeRecorderBindsPersistedAssignmentAndBenchmarkAdapters(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	store := NewStore(pool, StoreConfig{})
	if _, err := store.RecordAssignment(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	deterministic := validEvaluatorRegistration(EvaluatorDeterministic)
	judge := validEvaluatorRegistration(EvaluatorCalibratedJudge)
	catalog, err := NewEvaluatorCatalog(deterministic, judge)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := NewOutcomeRecorder(store, catalog)
	if err != nil {
		t.Fatal(err)
	}
	mismatched := validOutcomeObservation(assignment, deterministic)
	mismatched.ProviderID = "other-provider"
	if _, err := recorder.RecordOutcome(
		ctx,
		mismatched,
	); !errors.Is(err, ErrOutcomeAssignmentMismatch) {
		t.Fatalf("provider mismatch error = %v, want ErrOutcomeAssignmentMismatch", err)
	}
	deterministicAdapter, err := NewDeterministicBenchmarkEvaluator(recorder, deterministic)
	if err != nil {
		t.Fatal(err)
	}
	judgeAdapter, err := NewCalibratedJudgeBenchmarkEvaluator(recorder, judge)
	if err != nil {
		t.Fatal(err)
	}
	deterministicOutcome := validOutcomeObservation(assignment, judge)
	if sequence, err := deterministicAdapter.Record(
		ctx,
		deterministicOutcome,
	); err != nil || sequence != 2 {
		t.Fatalf("deterministic adapter = (%d, %v), want (2, nil)", sequence, err)
	}
	judgeOutcome := validOutcomeObservation(assignment, deterministic)
	judgeOutcome.SourceObservationID = uuid.Must(uuid.NewV7())
	if sequence, err := judgeAdapter.Record(
		ctx,
		judgeOutcome,
	); err != nil || sequence != 3 {
		t.Fatalf("judge adapter = (%d, %v), want (3, nil)", sequence, err)
	}
	assertAdaptiveNextSequence(t, pool, owner, assignment.RequestID.String(), 4)
}

func TestOutcomeRecorderAndDatabaseEnforceFoldableCorrectionLimit(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	store := NewStore(pool, StoreConfig{})
	if _, err := store.RecordAssignment(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	registration := validEvaluatorRegistration(EvaluatorDeterministic)
	catalog, err := NewEvaluatorCatalog(registration)
	if err != nil {
		t.Fatal(err)
	}
	recorder, err := NewOutcomeRecorder(store, catalog)
	if err != nil {
		t.Fatal(err)
	}
	outcome := validOutcomeObservation(assignment, registration)
	if _, err := recorder.RecordOutcome(ctx, outcome); err != nil {
		t.Fatal(err)
	}
	root, err := NewOutcomeEvent(assignment, outcome, catalog)
	if err != nil {
		t.Fatal(err)
	}
	leafID := root.ID
	for index := 0; index < maxCorrectionChainLength-1; index++ {
		correction := validCorrectionObservation(assignment, registration, leafID)
		correction.CorrectionSourceID = uuid.Must(uuid.NewV7())
		event, err := NewCorrectionEvent(assignment, correction, catalog)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := recorder.RecordCorrection(ctx, correction); err != nil {
			t.Fatalf("RecordCorrection exact-limit index %d: %v", index, err)
		}
		leafID = event.ID
	}
	records, err := store.ListAggregate(ctx, owner, assignment.RequestID.String())
	if err != nil {
		t.Fatal(err)
	}
	facts := make([]outcomeChainFact, 0, maxCorrectionChainLength)
	for _, record := range records {
		switch record.Kind {
		case EventOutcome:
			decoded, err := DecodeOutcome(record.Payload, assignment, catalog)
			if err != nil {
				t.Fatal(err)
			}
			facts = append(facts, outcomeChainFact{
				id: record.ID, kind: record.Kind, evaluator: decoded.Evaluator,
			})
		case EventCorrection:
			decoded, err := DecodeCorrection(record.Payload, assignment, catalog)
			if err != nil {
				t.Fatal(err)
			}
			facts = append(facts, outcomeChainFact{
				id:         record.ID,
				kind:       record.Kind,
				evaluator:  decoded.Evaluator,
				supersedes: decoded.SupersedesEventID,
			})
		}
	}
	folded, err := foldOutcomeChain(facts, root.ID)
	if err != nil {
		t.Fatalf("fold exact-limit persisted chain: %v", err)
	}
	if folded.id != leafID {
		t.Fatalf("persisted exact-limit leaf = %s, want %s", folded.id, leafID)
	}

	overLimit := validCorrectionObservation(assignment, registration, leafID)
	overLimit.CorrectionSourceID = uuid.Must(uuid.NewV7())
	if _, err := recorder.RecordCorrection(
		ctx,
		overLimit,
	); !errors.Is(err, ErrCorrectionChainTooLong) {
		t.Fatalf("over-limit recorder error = %v, want ErrCorrectionChainTooLong", err)
	}
	assertAdaptiveNextSequence(
		t,
		pool,
		owner,
		assignment.RequestID.String(),
		int64(maxCorrectionChainLength+2),
	)

	overLimitEvent, err := NewCorrectionEvent(assignment, overLimit, catalog)
	if err != nil {
		t.Fatal(err)
	}
	err = insertAdaptiveLedgerEvent(
		ctx,
		pool,
		overLimitEvent,
		int64(maxCorrectionChainLength+2),
	)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "54000" {
		t.Fatalf("over-limit direct insert error = %v, want SQLSTATE 54000", err)
	}
}

func assertAdaptiveNextSequence(
	t *testing.T,
	pool *pgxpool.Pool,
	owner uuid.UUID,
	aggregateID string,
	want int64,
) {
	t.Helper()
	var next int64
	if err := pool.QueryRow(
		t.Context(),
		`SELECT next_sequence
		 FROM aura.adaptive_aggregate_sequences
		 WHERE owner_id=$1 AND aggregate_id=$2`,
		owner,
		aggregateID,
	).Scan(&next); err != nil {
		t.Fatal(err)
	}
	if next != want {
		t.Fatalf("next aggregate sequence = %d, want %d", next, want)
	}
}
