//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOutcomeRecorderCorrectionDoesNotWaitForUnrelatedOutcomeLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	pool := adaptiveIntegrationPool(t)
	assignment, recorder, registration, catalog := liveOutcomeRecorderFixture(
		t,
		ctx,
		pool,
	)
	target := recordLiveOutcome(
		t,
		ctx,
		recorder,
		assignment,
		registration,
		catalog,
		uuid.Must(uuid.NewV7()),
	)
	unrelated := recordLiveOutcome(
		t,
		ctx,
		recorder,
		assignment,
		registration,
		catalog,
		uuid.Must(uuid.NewV7()),
	)

	blockerPool, err := db.Open(
		ctx,
		&db.Config{URL: pool.Config().ConnString()},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(blockerPool.Close)
	blocker, err := blockerPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		rollbackCtx, rollbackCancel := context.WithTimeout(
			context.WithoutCancel(t.Context()),
			5*time.Second,
		)
		defer rollbackCancel()
		if err := blocker.Rollback(rollbackCtx); err != nil &&
			!errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback unrelated outcome lock: %v", err)
		}
	})
	if _, err := blocker.Exec(
		ctx,
		`SELECT set_config('app.current_identity', $1, true)`,
		assignment.OwnerID.String(),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.Exec(
		ctx,
		`SELECT id FROM aura.adaptive_outbox WHERE id=$1 FOR UPDATE`,
		unrelated.ID,
	); err != nil {
		t.Fatalf("lock unrelated outcome: %v", err)
	}

	correction := validCorrectionObservation(assignment, registration, target.ID)
	correction.CorrectionSourceID = uuid.Must(uuid.NewV7())
	recordCtx, recordCancel := context.WithTimeout(ctx, 2*time.Second)
	defer recordCancel()
	if sequence, err := recorder.RecordCorrection(
		recordCtx,
		correction,
	); err != nil || sequence != 4 {
		t.Fatalf(
			"RecordCorrection while unrelated outcome locked = (%d, %v), want (4, nil)",
			sequence,
			err,
		)
	}
}

func TestOutcomeRecorderConcurrentSameTargetHasOneStableFork(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	pool := adaptiveIntegrationPool(t)
	assignment, recorder, registration, catalog := liveOutcomeRecorderFixture(
		t,
		ctx,
		pool,
	)
	target := recordLiveOutcome(
		t,
		ctx,
		recorder,
		assignment,
		registration,
		catalog,
		uuid.Must(uuid.NewV7()),
	)
	corrections := []CorrectionObservation{
		validCorrectionObservation(assignment, registration, target.ID),
		validCorrectionObservation(assignment, registration, target.ID),
	}
	corrections[0].CorrectionSourceID = uuid.Must(uuid.NewV7())
	corrections[1].CorrectionSourceID = uuid.Must(uuid.NewV7())

	start := make(chan struct{})
	ready := make(chan struct{}, len(corrections))
	results := make(chan outcomeCorrectionResult, len(corrections))
	for _, correction := range corrections {
		correction := correction
		go func() {
			select {
			case ready <- struct{}{}:
			case <-ctx.Done():
				return
			}
			select {
			case <-start:
			case <-ctx.Done():
				return
			}
			sequence, err := recorder.RecordCorrection(ctx, correction)
			select {
			case results <- outcomeCorrectionResult{sequence: sequence, err: err}:
			case <-ctx.Done():
			}
		}()
	}
	for range corrections {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatalf("concurrent corrections did not become ready: %v", ctx.Err())
		}
	}
	close(start)

	var winners, forks int
	for range corrections {
		select {
		case result := <-results:
			switch {
			case result.err == nil && result.sequence == 3:
				winners++
			case errors.Is(result.err, ErrCorrectionFork):
				forks++
			default:
				t.Fatalf(
					"concurrent correction = (%d, %v), want winner or ErrCorrectionFork",
					result.sequence,
					result.err,
				)
			}
		case <-ctx.Done():
			t.Fatalf("concurrent corrections did not finish: %v", ctx.Err())
		}
	}
	if winners != 1 || forks != 1 {
		t.Fatalf("concurrent corrections = %d winners/%d forks, want 1/1", winners, forks)
	}
}

type outcomeCorrectionResult struct {
	sequence int64
	err      error
}

func liveOutcomeRecorderFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) (Assignment, *OutcomeRecorder, EvaluatorRegistration, EvaluatorCatalog) {
	t.Helper()
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
	return assignment, recorder, registration, catalog
}

func recordLiveOutcome(
	t *testing.T,
	ctx context.Context,
	recorder *OutcomeRecorder,
	assignment Assignment,
	registration EvaluatorRegistration,
	catalog EvaluatorCatalog,
	sourceID uuid.UUID,
) Event {
	t.Helper()
	observation := validOutcomeObservation(assignment, registration)
	observation.SourceObservationID = sourceID
	event, err := NewOutcomeEvent(assignment, observation, catalog)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.RecordOutcome(ctx, observation); err != nil {
		t.Fatal(err)
	}
	return event
}
