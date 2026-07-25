//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

func TestCohortStoreReconstructsOpenCohort(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	t.Cleanup(cancel)
	pool, _ := schema2MigrationDatabase(t, ctx, "aura_cohort_loader", 74)
	ownerID := seedTypedLedgerOwner(t, pool)
	snapshot := focalCohortSnapshotForOwner(t, ownerID)
	if err := NewSnapshotStore(pool).Save(ctx, snapshot); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	cohort := focalCohortForSnapshot(t, snapshot, time.Now().UTC().Add(time.Minute), []BlockKey{BlockOwner, BlockTimeBlock})
	store := NewCohortStore(pool, NewStore(pool, StoreConfig{}))
	if err := store.Save(ctx, cohort); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	ledger, err := store.Reconstruct(ctx, ownerID, cohort.ID())
	if err != nil {
		t.Fatalf("Reconstruct() error = %v", err)
	}
	if ledger.State != CohortLedgerOpen || ledger.SHA256 != "" || ledger.Sealable {
		t.Fatalf("open ledger = %#v", ledger)
	}
}

func TestCohortStoreReconstructsClosedCensoredCohort(t *testing.T) {
	fixture := newCohortLoaderFixture(t, 2*time.Second)
	fixture.waitForClose(t)

	ledger, err := fixture.store.Reconstruct(fixture.ctx, fixture.ownerID, fixture.cohort.ID())
	if err != nil {
		t.Fatalf("Reconstruct() error = %v", err)
	}
	if ledger.State != CohortLedgerClosed || ledger.SHA256 == "" || len(ledger.Members) != 1 ||
		len(ledger.IncludedFacts) != 1 || ledger.Members[0].Claim.AssignmentID != fixture.enrollment.Assignment.AssignmentID {
		t.Fatalf("closed ledger = %#v", ledger)
	}
}

func TestCohortStoreReconstructsDeliveryOutcomesAndCorrection(t *testing.T) {
	fixture := newCohortLoaderFixture(t, 3*time.Second)
	assignment := fixture.enrollment.Assignment
	if _, err := fixture.ledger.RecordDelivery(
		fixture.ctx, fixture.ownerID, assignment.AssignmentID, cohortLoaderSuccessDelivery(t, assignment),
	); err != nil {
		t.Fatalf("record delivery: %v", err)
	}
	catalog, err := NewEvaluatorCatalog(fixture.cohort.Evaluators()...)
	if err != nil {
		t.Fatalf("new evaluator catalog: %v", err)
	}
	recorder, err := NewOutcomeRecorder(fixture.ledger, catalog)
	if err != nil {
		t.Fatalf("new outcome recorder: %v", err)
	}
	quality := cohortLoaderObservedOutcome(assignment, fixture.cohort.PrimaryQualityEndpoint().Evaluator, 1, 0)
	if _, err := recorder.RecordOutcome(fixture.ctx, quality); err != nil {
		t.Fatalf("record quality outcome: %v", err)
	}
	qualityEvent, err := NewOutcomeEvent(assignment, quality, catalog)
	if err != nil {
		t.Fatalf("construct quality event: %v", err)
	}
	harm := cohortLoaderObservedOutcome(assignment, fixture.cohort.PrimaryHarmEndpoint().Evaluator, 1, 0)
	if _, err := recorder.RecordOutcome(fixture.ctx, harm); err != nil {
		t.Fatalf("record harm outcome: %v", err)
	}
	correction := cohortLoaderCorrection(assignment, quality.Evaluator, qualityEvent.ID, 1, 0)
	if _, err := recorder.RecordCorrection(fixture.ctx, correction); err != nil {
		t.Fatalf("record quality correction: %v", err)
	}
	correctionEvent, err := NewCorrectionEvent(assignment, correction, catalog)
	if err != nil {
		t.Fatalf("construct quality correction event: %v", err)
	}
	fixture.waitForClose(t)

	ledger, err := fixture.store.Reconstruct(fixture.ctx, fixture.ownerID, fixture.cohort.ID())
	if err != nil {
		t.Fatalf("Reconstruct() error = %v", err)
	}
	member := ledger.Members[0]
	if ledger.State != CohortLedgerClosed || len(ledger.Members) != 1 || len(ledger.IncludedFacts) != 5 ||
		!member.Observed || !member.ITTEligible || !member.OPEEligible || member.Quality == nil || member.Harm == nil ||
		member.Quality.EventID != correctionEvent.ID || *member.Quality.Measures.Quality != 1 || len(member.ReasonCodes) != 0 {
		t.Fatalf("reconstructed full ledger = %#v", ledger)
	}
}

func TestCohortStoreReconstructRejectsNonbinaryPrimaryCorrection(t *testing.T) {
	fixture := newCohortLoaderFixture(t, 3*time.Second)
	assignment := fixture.enrollment.Assignment
	catalog, err := NewEvaluatorCatalog(fixture.cohort.Evaluators()...)
	if err != nil {
		t.Fatalf("new evaluator catalog: %v", err)
	}
	recorder, err := NewOutcomeRecorder(fixture.ledger, catalog)
	if err != nil {
		t.Fatalf("new outcome recorder: %v", err)
	}
	quality := cohortLoaderObservedOutcome(assignment, fixture.cohort.PrimaryQualityEndpoint().Evaluator, 1, 0)
	if _, err := recorder.RecordOutcome(fixture.ctx, quality); err != nil {
		t.Fatalf("record quality outcome: %v", err)
	}
	qualityEvent, err := NewOutcomeEvent(assignment, quality, catalog)
	if err != nil {
		t.Fatalf("construct quality event: %v", err)
	}
	harm := cohortLoaderObservedOutcome(assignment, fixture.cohort.PrimaryHarmEndpoint().Evaluator, 1, 0)
	if _, err := recorder.RecordOutcome(fixture.ctx, harm); err != nil {
		t.Fatalf("record harm outcome: %v", err)
	}
	if _, err := recorder.RecordCorrection(
		fixture.ctx,
		cohortLoaderCorrection(assignment, quality.Evaluator, qualityEvent.ID, 0.5, 0),
	); err != nil {
		t.Fatalf("record nonbinary quality correction: %v", err)
	}
	fixture.waitForClose(t)

	_, err = fixture.store.Reconstruct(fixture.ctx, fixture.ownerID, fixture.cohort.ID())
	if !errors.Is(err, ErrInvalidCohortLedger) {
		t.Fatalf("Reconstruct() error = %v, want ErrInvalidCohortLedger", err)
	}
}

func TestCohortStoreReconstructExcludesPostCutoffFactsFromHash(t *testing.T) {
	fixture := newCohortLoaderFixture(t, 2*time.Second)
	fixture.waitForClose(t)
	baseline, err := fixture.store.Reconstruct(fixture.ctx, fixture.ownerID, fixture.cohort.ID())
	if err != nil {
		t.Fatalf("reconstruct baseline: %v", err)
	}
	if _, err := fixture.ledger.RecordDelivery(
		fixture.ctx,
		fixture.ownerID,
		fixture.enrollment.Assignment.AssignmentID,
		cohortLoaderSuccessDelivery(t, fixture.enrollment.Assignment),
	); err != nil {
		t.Fatalf("record post-cutoff delivery: %v", err)
	}

	ledger, err := fixture.store.Reconstruct(fixture.ctx, fixture.ownerID, fixture.cohort.ID())
	if err != nil {
		t.Fatalf("reconstruct with post-cutoff fact: %v", err)
	}
	if ledger.SHA256 != baseline.SHA256 || len(ledger.IncludedFacts) != len(baseline.IncludedFacts) ||
		ledger.LateFactCount != baseline.LateFactCount+1 || ledger.Members[0].Delivery != nil {
		t.Fatalf("post-cutoff reconstruction = %#v, baseline = %#v", ledger, baseline)
	}
}

func TestCohortStoreReconstructRejectsCohortFactWithoutRecordedAt(t *testing.T) {
	fixture := newCohortLoaderFixture(t, 2*time.Second)
	assignmentEvent, err := NewAssignmentEvent(fixture.enrollment.Assignment)
	if err != nil {
		t.Fatalf("construct assignment event: %v", err)
	}
	fixture.clearFactRecordedAt(t, assignmentEvent.ID)
	fixture.waitForClose(t)

	_, err = fixture.store.Reconstruct(fixture.ctx, fixture.ownerID, fixture.cohort.ID())
	if !errors.Is(err, ErrInvalidCohortLedger) {
		t.Fatalf("Reconstruct() error = %v, want ErrInvalidCohortLedger", err)
	}
}

func TestCohortReconstructionOwnerFenceBlocksTrustedWriter(t *testing.T) {
	fixture := newCohortLoaderFixture(t, 5*time.Second)
	migratePool := fixture.migrationPool(t)
	fenceTx, err := migratePool.Begin(fixture.ctx)
	if err != nil {
		t.Fatalf("begin reconstruction fence: %v", err)
	}
	defer rollbackTypedTestTx(t, fenceTx)
	var fencePID int32
	if err := fenceTx.QueryRow(fixture.ctx, `SELECT pg_backend_pid()`).Scan(&fencePID); err != nil {
		t.Fatalf("read reconstruction fence PID: %v", err)
	}
	if _, err := sqlc.New(fenceTx).LockAdaptiveOwnerForCohortReconstruction(fixture.ctx, dbUUID(fixture.ownerID)); err != nil {
		t.Fatalf("lock reconstruction owner: %v", err)
	}

	done := recordTypedDeliveryAsync(
		fixture.ctx,
		fixture.ledger,
		fixture.ownerID,
		fixture.enrollment.Assignment,
		cohortLoaderSuccessDelivery(t, fixture.enrollment.Assignment),
	)
	waitForAdaptiveOwnerLock(t, fixture.ctx, migratePool, fencePID, done)
	if err := fenceTx.Commit(fixture.ctx); err != nil {
		t.Fatalf("release reconstruction fence: %v", err)
	}
	select {
	case result := <-done:
		if result.err != nil || result.sequence != 2 {
			t.Fatalf("RecordDelivery after reconstruction fence = (%d, %v), want (2, nil)", result.sequence, result.err)
		}
	case <-fixture.ctx.Done():
		t.Fatalf("RecordDelivery did not finish after reconstruction fence: %v", fixture.ctx.Err())
	}
}
