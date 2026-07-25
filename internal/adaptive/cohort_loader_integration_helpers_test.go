//go:build db_integration

package adaptive

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type cohortLoaderFixture struct {
	ctx        context.Context
	pool       *pgxpool.Pool
	migrateURL string
	ownerID    uuid.UUID
	cohort     *FocalCohort
	store      *CohortStore
	ledger     *Store
	enrollment *FocalEnrollment
	cutoff     time.Time
}

func newCohortLoaderFixture(t *testing.T, cutoffAfter time.Duration) cohortLoaderFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	t.Cleanup(cancel)
	pool, migrateURL := schema2MigrationDatabase(t, ctx, "aura_cohort_loader", 74)
	ownerID := seedTypedLedgerOwner(t, pool)
	snapshot := focalCohortSnapshotForOwner(t, ownerID)
	if err := NewSnapshotStore(pool).Save(ctx, snapshot); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	cutoff := time.Now().UTC().Add(cutoffAfter)
	cohort := focalCohortForSnapshot(t, snapshot, cutoff, []BlockKey{BlockOwner, BlockTimeBlock})
	ledger := NewStore(pool, StoreConfig{})
	store := NewCohortStore(pool, ledger)
	if err := store.Save(ctx, cohort); err != nil {
		t.Fatalf("save cohort: %v", err)
	}
	request := FocalEnrollmentRequest{
		OwnerID: ownerID, CohortID: cohort.ID(), RequestID: uuid.Must(uuid.NewV7()),
		EvaluationConversationID: evaluationConversation(t, ctx, pool, ownerID),
		Domain:                   cohort.Domain(), Point: cohort.Predicate().Point, PointOrdinal: cohort.Predicate().Ordinal,
	}
	enrollment, err := store.Enroll(ctx, request, focalEnrollmentBuilder(t))
	if err != nil {
		t.Fatalf("enroll cohort member: %v", err)
	}
	return cohortLoaderFixture{
		ctx: ctx, pool: pool, migrateURL: migrateURL, ownerID: ownerID, cohort: cohort,
		store: store, ledger: ledger, enrollment: enrollment, cutoff: cutoff,
	}
}

func (fixture cohortLoaderFixture) waitForClose(t *testing.T) {
	t.Helper()
	wait := time.Until(fixture.cutoff) + 100*time.Millisecond
	if wait <= 0 {
		return
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-fixture.ctx.Done():
		t.Fatalf("wait for cohort cutoff: %v", fixture.ctx.Err())
	}
}

func (fixture cohortLoaderFixture) migrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := db.Open(fixture.ctx, &db.Config{URL: fixture.migrateURL})
	if err != nil {
		t.Fatalf("open cohort-loader migration pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func (fixture cohortLoaderFixture) clearFactRecordedAt(t *testing.T, eventID uuid.UUID) {
	t.Helper()
	pool := fixture.migrationPool(t)
	if _, err := pool.Exec(fixture.ctx, `ALTER TABLE aura.adaptive_outbox DISABLE TRIGGER USER`); err != nil {
		t.Fatalf("disable outbox fact triggers: %v", err)
	}
	defer func() {
		if _, err := pool.Exec(fixture.ctx, `ALTER TABLE aura.adaptive_outbox ENABLE TRIGGER USER`); err != nil {
			t.Errorf("enable outbox fact triggers: %v", err)
		}
	}()
	if _, err := pool.Exec(fixture.ctx, `UPDATE aura.adaptive_outbox SET recorded_at = NULL WHERE id = $1`, eventID); err != nil {
		t.Fatalf("clear fact recorded_at: %v", err)
	}
}

func cohortLoaderSuccessDelivery(t *testing.T, assignment Assignment) Delivery {
	t.Helper()
	var probability float64
	for _, action := range assignment.ActionProbabilities {
		if action.ActionID == assignment.IntendedActionID {
			probability = action.Probability
			break
		}
	}
	revisions, err := NewRevisionSet()
	if err != nil {
		t.Fatal(err)
	}
	return Delivery{
		SchemaVersion:       SchemaVersion2,
		AssignmentID:        assignment.AssignmentID,
		IntendedActionID:    assignment.IntendedActionID,
		ActualActionID:      assignment.IntendedActionID,
		Status:              DeliverySuccess,
		ExposureKnown:       true,
		ExposureProbability: &probability,
		ResultIDs:           []ResultID{},
		Revisions:           revisions,
		EffectiveLimits:     map[string]int{},
	}
}

func cohortLoaderObservedOutcome(
	assignment Assignment,
	evaluator EvaluatorProvenance,
	quality, harm float64,
) OutcomeObservation {
	return OutcomeObservation{
		SchemaVersion:       SchemaVersion2,
		AssignmentID:        assignment.AssignmentID,
		OwnerID:             assignment.OwnerID,
		Domain:              assignment.Domain,
		ProviderID:          assignment.ProviderID,
		ModelID:             assignment.ModelID,
		Evaluator:           evaluator,
		SourceObservationID: uuid.Must(uuid.NewV7()),
		Status:              OutcomeObserved,
		Measures:            OutcomeMeasures{Quality: &quality, Harm: &harm},
		ObservedAt:          time.Now().UTC(),
	}
}

func cohortLoaderCensoredOutcome(assignment Assignment, evaluator EvaluatorProvenance) OutcomeObservation {
	return OutcomeObservation{
		SchemaVersion:       SchemaVersion2,
		AssignmentID:        assignment.AssignmentID,
		OwnerID:             assignment.OwnerID,
		Domain:              assignment.Domain,
		ProviderID:          assignment.ProviderID,
		ModelID:             assignment.ModelID,
		Evaluator:           evaluator,
		SourceObservationID: uuid.Must(uuid.NewV7()),
		Status:              OutcomeCensored,
		ObservedAt:          time.Now().UTC(),
	}
}

func cohortLoaderCorrection(
	assignment Assignment,
	evaluator EvaluatorProvenance,
	supersedesEventID uuid.UUID,
	quality, harm float64,
) CorrectionObservation {
	return CorrectionObservation{
		SchemaVersion:      SchemaVersion2,
		AssignmentID:       assignment.AssignmentID,
		OwnerID:            assignment.OwnerID,
		Domain:             assignment.Domain,
		ProviderID:         assignment.ProviderID,
		ModelID:            assignment.ModelID,
		Evaluator:          evaluator,
		CorrectionSourceID: uuid.Must(uuid.NewV7()),
		SupersedesEventID:  supersedesEventID,
		Status:             OutcomeObserved,
		Measures:           OutcomeMeasures{Quality: &quality, Harm: &harm},
		ObservedAt:         time.Now().UTC(),
	}
}
