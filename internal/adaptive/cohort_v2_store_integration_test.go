//go:build db_integration

package adaptive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
)

func TestCohortStoreEnrollRejectsLegacyV1BeforeAnySideEffect(t *testing.T) {
	store, _, randomized, request, ctx, pool := cohortStoreFixture(t)
	snapshot, err := NewSnapshotStore(pool).Load(
		ctx, request.OwnerID, randomized.Scope().SnapshotID,
	)
	if err != nil {
		t.Fatal(err)
	}
	legacy := focalCohortForSnapshot(
		t, snapshot, time.Now().Add(time.Hour),
		[]BlockKey{BlockEpisode, BlockOwner, BlockSession, BlockTimeBlock},
	)
	if err := store.Save(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	request.CohortID = legacy.ID()
	source := &countingReader{Reader: bytes.NewReader([]byte{0xff})}
	store.entropy = source
	builds := 0
	_, err = store.Enroll(
		ctx,
		request,
		func(*FocalCohort, FocalEnrollmentRequest) (AssignmentTemplate, error) {
			builds++
			return focalEnrollmentTemplate(), nil
		},
	)
	if !errors.Is(err, ErrCohortV1RandomizationUnsupported) {
		t.Fatalf("Enroll(v1) error = %v", err)
	}
	if builds != 0 || source.bytesRead != 0 {
		t.Fatalf("v1 rejection builds/entropy = %d/%d, want 0/0", builds, source.bytesRead)
	}
	assertFocalEnrollmentCounts(
		t, ctx, pool, request.OwnerID, legacy.ID(), 0, 0, 0,
	)
}

func TestCohortStoreEnrollRejectsTamperedV2ChildBeforeAnySideEffect(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	t.Cleanup(cancel)
	pool, migrateURL := schema2MigrationDatabase(
		t, ctx, "aura_cohort_v2_tamper", shippedMigrationSteps(t),
	)
	ownerID := seedTypedLedgerOwner(t, pool)
	snapshot := focalCohortSnapshotForOwner(t, ownerID)
	if err := NewSnapshotStore(pool).Save(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	cohort := randomizedFocalCohortForSnapshot(
		t, snapshot, time.Now().Add(time.Hour),
		[]BlockKey{BlockOwner, BlockTimeBlock},
	)
	store := NewCohortStore(pool, NewStore(pool, StoreConfig{}))
	if err := store.Save(ctx, cohort); err != nil {
		t.Fatal(err)
	}
	migrationPool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(migrationPool.Close)
	if _, err := migrationPool.Exec(
		ctx,
		`ALTER TABLE aura.adaptive_focal_cohorts
			DROP CONSTRAINT adaptive_focal_cohorts_artifact_scope_check`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := migrationPool.Exec(
		ctx, `ALTER TABLE aura.adaptive_focal_cohorts DISABLE TRIGGER USER`,
	); err != nil {
		t.Fatal(err)
	}
	tamperedPlan := cohort.v2.randomizationPlan
	tamperedPlan.BitIndex = 1
	tamperedArtifact, err := json.Marshal(tamperedPlan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := migrationPool.Exec(
		ctx,
		`UPDATE aura.adaptive_focal_cohorts
		 SET randomization_plan_artifact=$1,
		     randomization_plan_artifact_json=convert_from($1, 'UTF8')::jsonb
		 WHERE id=$2`,
		tamperedArtifact, cohort.ID(),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := migrationPool.Exec(
		ctx, `ALTER TABLE aura.adaptive_focal_cohorts ENABLE TRIGGER USER`,
	); err != nil {
		t.Fatal(err)
	}
	request := FocalEnrollmentRequest{
		OwnerID: ownerID, CohortID: cohort.ID(), RequestID: uuid.Must(uuid.NewV7()),
		EvaluationConversationID: evaluationConversation(t, ctx, pool, ownerID),
		Domain:                   cohort.Domain(), Point: cohort.Predicate().Point,
		PointOrdinal: cohort.Predicate().Ordinal,
	}
	source := &countingReader{Reader: bytes.NewReader([]byte{0xff})}
	store.entropy = source
	builds := 0
	_, err = store.Enroll(
		ctx, request,
		func(*FocalCohort, FocalEnrollmentRequest) (AssignmentTemplate, error) {
			builds++
			return focalEnrollmentTemplate(), nil
		},
	)
	if !errors.Is(err, ErrPersistedCohortInvalid) {
		t.Fatalf("Enroll(tampered v2 child) error = %v", err)
	}
	if builds != 0 || source.bytesRead != 0 {
		t.Fatalf(
			"tampered child rejection builds/entropy = %d/%d, want 0/0",
			builds, source.bytesRead,
		)
	}
	assertFocalEnrollmentCounts(
		t, ctx, pool, ownerID, cohort.ID(), 0, 0, 0,
	)
}
