//go:build db_integration

package adaptive

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCohortStorePersistsAndLoadsVerifiedCohort(t *testing.T) {
	store, _, cohort, _, ctx, _ := cohortStoreFixture(t)
	if err := store.Save(ctx, cohort); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.Save(ctx, cohort); err != nil {
		t.Fatalf("Save() exact replay error = %v", err)
	}
	loaded, err := store.Load(ctx, cohort.Scope().OwnerID, cohort.ID())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	assertFocalCohortsEqual(t, loaded, cohort)
}

func TestCohortStoreEnrollPersistsOneByteAssignmentAndReceiptTuple(t *testing.T) {
	store, _, cohort, request, ctx, pool := cohortStoreFixture(t)
	if err := store.Save(ctx, cohort); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	source := &countingReader{Reader: bytes.NewReader([]byte{0x00, 0x01, 0xfe, 0xff})}
	store.entropy = source
	vectors := []struct {
		draw byte
		arm  string
	}{
		{draw: 0x00, arm: "baseline"},
		{draw: 0x01, arm: "challenger"},
		{draw: 0xfe, arm: "baseline"},
		{draw: 0xff, arm: "challenger"},
	}
	for index, vector := range vectors {
		candidate := request
		if index > 0 {
			candidate.RequestID = uuid.Must(uuid.NewV7())
			candidate.EvaluationConversationID = evaluationConversation(
				t, ctx, pool, request.OwnerID,
			)
		}
		enrollment, err := store.Enroll(ctx, candidate, focalEnrollmentBuilder(t))
		if err != nil {
			t.Fatalf("Enroll(%02x) error = %v", vector.draw, err)
		}
		if enrollment.Assignment.ArmID != vector.arm ||
			enrollment.Receipt.DrawByte != hexByte(vector.draw) ||
			enrollment.Receipt.SelectedArmID != vector.arm {
			t.Fatalf("Enroll(%02x) tuple = %#v", vector.draw, enrollment)
		}
		if enrollment.Claim.AnalysisStratumID != enrollment.Receipt.AnalysisStratumID ||
			enrollment.Claim.AnalysisStratumSchemaSHA256 !=
				enrollment.Receipt.AnalysisStratumSchemaSHA256 {
			t.Fatal("claim and receipt stratum binding differ")
		}
	}
	if source.bytesRead != len(vectors) {
		t.Fatalf("entropy bytes read = %d, want %d", source.bytesRead, len(vectors))
	}
	assertFocalEnrollmentCounts(t, ctx, pool, request.OwnerID, cohort.ID(), 4, 4, 4)
}

func TestCohortStoreEnrollExactRetryReloadsWithoutBuilderOrEntropy(t *testing.T) {
	store, _, cohort, request, ctx, pool := cohortStoreFixture(t)
	if err := store.Save(ctx, cohort); err != nil {
		t.Fatal(err)
	}
	store.entropy = bytes.NewReader([]byte{0x01})
	first, err := store.Enroll(ctx, request, focalEnrollmentBuilder(t))
	if err != nil {
		t.Fatalf("Enroll() error = %v", err)
	}
	store.entropy = bytes.NewReader(nil)
	replayed, err := store.Enroll(
		ctx,
		request,
		func(*FocalCohort, FocalEnrollmentRequest) (AssignmentTemplate, error) {
			t.Fatal("exact retry invoked template builder")
			return AssignmentTemplate{}, nil
		},
	)
	if err != nil {
		t.Fatalf("Enroll() retry error = %v", err)
	}
	assertAssignmentsEqual(t, replayed.Assignment, first.Assignment)
	firstReceipt, _ := first.Receipt.CanonicalJSON()
	replayedReceipt, _ := replayed.Receipt.CanonicalJSON()
	if !bytes.Equal(firstReceipt, replayedReceipt) {
		t.Fatal("exact retry changed receipt bytes")
	}
	assertFocalEnrollmentCounts(t, ctx, pool, request.OwnerID, cohort.ID(), 1, 1, 1)
}

func TestCohortStoreEnrollRollsBackEveryPartialTupleFault(t *testing.T) {
	fault := errors.New("injected enrollment fault")
	tests := []struct {
		name   string
		inject func(*CohortStore) AssignmentTemplateBuilder
	}{
		{
			name: "template",
			inject: func(*CohortStore) AssignmentTemplateBuilder {
				return func(*FocalCohort, FocalEnrollmentRequest) (AssignmentTemplate, error) {
					return AssignmentTemplate{}, fault
				}
			},
		},
		{
			name: "entropy",
			inject: func(store *CohortStore) AssignmentTemplateBuilder {
				store.entropy = bytes.NewReader(nil)
				return focalEnrollmentBuilder(t)
			},
		},
		{
			name: "assignment",
			inject: func(store *CohortStore) AssignmentTemplateBuilder {
				store.writeAssignment = func(
					context.Context, *sqlc.Queries, Event,
				) (bool, error) {
					return false, fault
				}
				return focalEnrollmentBuilder(t)
			},
		},
		{
			name: "receipt",
			inject: func(store *CohortStore) AssignmentTemplateBuilder {
				store.writeReceipt = func(
					context.Context, *sqlc.Queries, RandomizationReceipt,
				) error {
					return fault
				}
				return focalEnrollmentBuilder(t)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, _, cohort, request, ctx, pool := cohortStoreFixture(t)
			if err := store.Save(ctx, cohort); err != nil {
				t.Fatal(err)
			}
			_, err := store.Enroll(ctx, request, test.inject(store))
			if err == nil {
				t.Fatal("Enroll() error = nil")
			}
			assertFocalEnrollmentCounts(
				t, ctx, pool, request.OwnerID, cohort.ID(), 0, 0, 0,
			)
		})
	}
}

func TestCohortStoreEnrollRecoversCommittedTupleBeforeReturningCommitError(t *testing.T) {
	store, _, cohort, request, ctx, pool := cohortStoreFixture(t)
	if err := store.Save(ctx, cohort); err != nil {
		t.Fatal(err)
	}
	commitUnknown := errors.New("commit result unavailable")
	store.entropy = bytes.NewReader([]byte{0xff})
	store.transact = func(
		ctx context.Context,
		pool *pgxpool.Pool,
		identity string,
		fn func(*sqlc.Queries) error,
	) error {
		if err := db.WithIdentityTx(ctx, pool, identity, fn); err != nil {
			return err
		}
		return commitUnknown
	}
	enrollment, err := store.Enroll(ctx, request, focalEnrollmentBuilder(t))
	if err != nil {
		t.Fatalf("Enroll() commit-unknown recovery error = %v", err)
	}
	if enrollment.Assignment.ArmID != "challenger" || enrollment.Receipt.DrawByte != "ff" {
		t.Fatalf("recovered tuple = %#v", enrollment)
	}
	assertFocalEnrollmentCounts(t, ctx, pool, request.OwnerID, cohort.ID(), 1, 1, 1)
}

func TestCohortStoreEnrollUniquenessLoserNeverBuildsOrReadsEntropy(t *testing.T) {
	store, _, cohort, winner, ctx, pool := cohortStoreFixture(t)
	if err := store.Save(ctx, cohort); err != nil {
		t.Fatal(err)
	}
	source := &countingReader{Reader: bytes.NewReader([]byte{0x01, 0xff})}
	store.entropy = source
	loser := winner
	loser.RequestID = uuid.Must(uuid.NewV7())
	start := make(chan struct{})
	errs := make(chan error, 2)
	var builds atomic.Int32
	var wait sync.WaitGroup
	for _, request := range []FocalEnrollmentRequest{winner, loser} {
		wait.Add(1)
		go func(candidate FocalEnrollmentRequest) {
			defer wait.Done()
			<-start
			_, err := store.Enroll(
				ctx,
				candidate,
				func(*FocalCohort, FocalEnrollmentRequest) (AssignmentTemplate, error) {
					builds.Add(1)
					return focalEnrollmentTemplate(), nil
				},
			)
			errs <- err
		}(request)
	}
	close(start)
	wait.Wait()
	close(errs)
	var successes, unavailable int
	for err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrFocalSlotUnavailable):
			unavailable++
		default:
			t.Fatalf("race error = %v", err)
		}
	}
	if successes != 1 || unavailable != 1 || builds.Load() != 1 || source.bytesRead != 1 {
		t.Fatalf(
			"success/unavailable/builds/bytes = %d/%d/%d/%d",
			successes, unavailable, builds.Load(), source.bytesRead,
		)
	}
	assertFocalEnrollmentCounts(t, ctx, pool, winner.OwnerID, cohort.ID(), 1, 1, 1)
}

func TestCohortStoreEnrollRejectsCrossCohortConversationWithoutEntropy(t *testing.T) {
	store, _, first, request, ctx, pool := cohortStoreFixture(t)
	if err := store.Save(ctx, first); err != nil {
		t.Fatal(err)
	}
	snapshot, err := NewSnapshotStore(pool).Load(ctx, request.OwnerID, first.Scope().SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	second := randomizedFocalCohortForSnapshot(
		t, snapshot, time.Now().Add(2*time.Hour),
		[]BlockKey{BlockEpisode, BlockOwner, BlockSession, BlockTimeBlock},
	)
	if err := store.Save(ctx, second); err != nil {
		t.Fatal(err)
	}
	source := &countingReader{Reader: bytes.NewReader([]byte{0x00, 0xff})}
	store.entropy = source
	if _, err := store.Enroll(ctx, request, focalEnrollmentBuilder(t)); err != nil {
		t.Fatal(err)
	}
	loser := request
	loser.CohortID = second.ID()
	_, err = store.Enroll(
		ctx,
		loser,
		func(*FocalCohort, FocalEnrollmentRequest) (AssignmentTemplate, error) {
			t.Fatal("cross-cohort loser invoked template builder")
			return AssignmentTemplate{}, nil
		},
	)
	if !errors.Is(err, ErrFocalSlotUnavailable) {
		t.Fatalf("cross-cohort loser error = %v", err)
	}
	if source.bytesRead != 1 {
		t.Fatalf("entropy bytes read = %d, want 1", source.bytesRead)
	}
	assertOwnerFocalEnrollmentCounts(t, ctx, pool, request.OwnerID, 1, 1, 1)
}

func cohortStoreFixture(
	t *testing.T,
) (*CohortStore, *Store, *FocalCohort, FocalEnrollmentRequest, context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	t.Cleanup(cancel)
	pool, _ := schema2MigrationDatabase(t, ctx, "aura_cohort_store", shippedMigrationSteps(t))
	owner := seedTypedLedgerOwner(t, pool)
	snapshot := focalCohortSnapshotForOwner(t, owner)
	if err := NewSnapshotStore(pool).Save(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	cohort := randomizedFocalCohortForSnapshot(
		t, snapshot, time.Now().Add(time.Hour),
		[]BlockKey{BlockEpisode, BlockOwner, BlockSession, BlockTimeBlock},
	)
	ledger := NewStore(pool, StoreConfig{})
	return NewCohortStore(pool, ledger), ledger, cohort, FocalEnrollmentRequest{
		OwnerID: owner, CohortID: cohort.ID(), RequestID: uuid.Must(uuid.NewV7()),
		EvaluationConversationID: evaluationConversation(t, ctx, pool, owner),
		SessionID:                uuidPointer(uuid.Must(uuid.NewV7())),
		EpisodeID:                uuidPointer(uuid.Must(uuid.NewV7())),
		Domain:                   cohort.Domain(), Point: cohort.Predicate().Point,
		PointOrdinal: cohort.Predicate().Ordinal,
	}, ctx, pool
}

func randomizedFocalCohortForSnapshot(
	t *testing.T,
	snapshot *PolicySnapshot,
	cutoff time.Time,
	blockKeys []BlockKey,
) *FocalCohort {
	t.Helper()
	spec := focalCohortSpecForSnapshot(snapshot, cutoff, blockKeys)
	spec.Actions = make([]ActionProbability, len(snapshot.Actions()))
	for index, action := range snapshot.Actions() {
		probability := 0.25
		if action.ActionID == StaticActionID {
			probability = 0.5
		}
		spec.Actions[index] = ActionProbability{
			ActionID: action.ActionID, Probability: probability,
		}
	}
	cohort, err := NewRandomizedFocalCohort(
		spec, snapshot, validCohortV2ChildRefs(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return cohort
}

func focalEnrollmentBuilder(t *testing.T) AssignmentTemplateBuilder {
	t.Helper()
	return func(*FocalCohort, FocalEnrollmentRequest) (AssignmentTemplate, error) {
		return focalEnrollmentTemplate(), nil
	}
}

func focalEnrollmentTemplate() AssignmentTemplate {
	return AssignmentTemplate{Features: []SnapshotFeatureValue{
		{Key: FeatureCandidateCount, Value: 2},
		{Key: FeatureContextLength, Value: 10},
	}}
}

func evaluationConversation(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	ownerID uuid.UUID,
) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(
		ctx, `INSERT INTO aura.conversations (id, identity_id) VALUES ($1,$2)`,
		id, ownerID,
	); err != nil {
		t.Fatal(err)
	}
	return id
}

func assertFocalEnrollmentCounts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	ownerID uuid.UUID,
	cohortID uuid.UUID,
	wantClaims int,
	wantAssignments int,
	wantReceipts int,
) {
	t.Helper()
	var claims, assignments, receipts int
	err := db.WithIdentityTxRaw(ctx, pool, ownerID.String(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(
			ctx, `SELECT count(*) FROM aura.adaptive_focal_cohort_claims WHERE cohort_id=$1`,
			cohortID,
		).Scan(&claims); err != nil {
			return err
		}
		if err := tx.QueryRow(
			ctx, `SELECT count(*) FROM aura.adaptive_outbox
				WHERE owner_id=$1 AND event_kind='decision'
				AND payload->>'schema_version'='2.0'`,
			ownerID,
		).Scan(&assignments); err != nil {
			return err
		}
		return tx.QueryRow(
			ctx, `SELECT count(*) FROM aura.adaptive_randomization_receipts
				WHERE owner_id=$1 AND cohort_id=$2`,
			ownerID, cohortID,
		).Scan(&receipts)
	})
	if err != nil {
		t.Fatal(err)
	}
	if claims != wantClaims || assignments != wantAssignments || receipts != wantReceipts {
		t.Fatalf(
			"claims/assignments/receipts = %d/%d/%d, want %d/%d/%d",
			claims, assignments, receipts, wantClaims, wantAssignments, wantReceipts,
		)
	}
}

func assertOwnerFocalEnrollmentCounts(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	ownerID uuid.UUID,
	wantClaims int,
	wantAssignments int,
	wantReceipts int,
) {
	t.Helper()
	var claims, assignments, receipts int
	err := db.WithIdentityTxRaw(ctx, pool, ownerID.String(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(
			ctx, `SELECT count(*) FROM aura.adaptive_focal_cohort_claims WHERE owner_id=$1`,
			ownerID,
		).Scan(&claims); err != nil {
			return err
		}
		if err := tx.QueryRow(
			ctx, `SELECT count(*) FROM aura.adaptive_outbox
				WHERE owner_id=$1 AND event_kind='decision'
				AND payload->>'schema_version'='2.0'`,
			ownerID,
		).Scan(&assignments); err != nil {
			return err
		}
		return tx.QueryRow(
			ctx, `SELECT count(*) FROM aura.adaptive_randomization_receipts WHERE owner_id=$1`,
			ownerID,
		).Scan(&receipts)
	})
	if err != nil {
		t.Fatal(err)
	}
	if claims != wantClaims || assignments != wantAssignments || receipts != wantReceipts {
		t.Fatalf(
			"owner claims/assignments/receipts = %d/%d/%d, want %d/%d/%d",
			claims, assignments, receipts, wantClaims, wantAssignments, wantReceipts,
		)
	}
}

func assertFocalCohortsEqual(t *testing.T, got, want *FocalCohort) {
	t.Helper()
	gotArtifact, gotErr := got.canonicalArtifact()
	wantArtifact, wantErr := want.canonicalArtifact()
	if gotErr != nil || wantErr != nil {
		t.Fatalf("canonical artifacts = (%v, %v)", gotErr, wantErr)
	}
	if got.ID() != want.ID() || got.SHA256() != want.SHA256() ||
		!bytes.Equal(gotArtifact, wantArtifact) {
		t.Fatalf("loaded cohort = %#v, want %#v", got, want)
	}
}

func assertAssignmentsEqual(t *testing.T, got, want Assignment) {
	t.Helper()
	gotEvent, gotErr := NewAssignmentEvent(got)
	wantEvent, wantErr := NewAssignmentEvent(want)
	if gotErr != nil || wantErr != nil {
		t.Fatalf("assignment events = (%v, %v)", gotErr, wantErr)
	}
	if gotEvent.ID != wantEvent.ID || !bytes.Equal(gotEvent.Payload, wantEvent.Payload) {
		t.Fatalf("replayed assignment = %#v, want %#v", got, want)
	}
}

func uuidPointer(id uuid.UUID) *uuid.UUID { return &id }

func hexByte(value byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[value>>4], digits[value&0x0f]})
}
