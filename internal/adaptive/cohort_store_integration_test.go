//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
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

func TestCohortStoreEnrollsAtomicallyAndReplaysExactClaim(t *testing.T) {
	store, _, cohort, request, ctx, pool := cohortStoreFixture(t)
	if err := store.Save(ctx, cohort); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	builds := 0
	build := func(cohort *FocalCohort, request FocalEnrollmentRequest) (Assignment, error) {
		builds++
		return focalEnrollmentAssignment(t, cohort, request), nil
	}

	first, err := store.Enroll(ctx, request, build)
	if err != nil {
		t.Fatalf("Enroll() error = %v", err)
	}
	if builds != 1 {
		t.Fatalf("fresh callback calls = %d, want 1", builds)
	}
	if first.Assignment.AssignmentID == uuid.Nil {
		t.Fatal("fresh enrollment returned a zero assignment")
	}
	assertFocalEnrollmentCounts(t, ctx, pool, request.OwnerID, cohort.ID(), 1, 1)

	replayed, err := store.Enroll(ctx, request, func(*FocalCohort, FocalEnrollmentRequest) (Assignment, error) {
		t.Fatal("exact retry invoked callback")
		return Assignment{}, nil
	})
	if err != nil {
		t.Fatalf("Enroll() exact retry error = %v", err)
	}
	assertAssignmentsEqual(t, replayed.Assignment, first.Assignment)
	assertFocalEnrollmentCounts(t, ctx, pool, request.OwnerID, cohort.ID(), 1, 1)
}

func TestCohortStoreEnrollRollsBackClaimWhenCallbackFails(t *testing.T) {
	store, _, cohort, request, ctx, pool := cohortStoreFixture(t)
	if err := store.Save(ctx, cohort); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	callbackErr := errors.New("draw failed")

	_, err := store.Enroll(ctx, request, func(*FocalCohort, FocalEnrollmentRequest) (Assignment, error) {
		return Assignment{}, callbackErr
	})
	if !errors.Is(err, callbackErr) {
		t.Fatalf("Enroll() error = %v, want callback error", err)
	}
	assertFocalEnrollmentCounts(t, ctx, pool, request.OwnerID, cohort.ID(), 0, 0)
}

func TestCohortStoreEnrollFailsClosedForConflictsAndUnboundAssignments(t *testing.T) {
	t.Run("changed immutable retry", func(t *testing.T) {
		store, _, cohort, request, ctx, _ := cohortStoreFixture(t)
		if err := store.Save(ctx, cohort); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		if _, err := store.Enroll(ctx, request, focalEnrollmentBuilder(t)); err != nil {
			t.Fatalf("Enroll() error = %v", err)
		}
		changed := request
		changed.SessionID = uuidPointer(uuid.Must(uuid.NewV7()))
		_, err := store.Enroll(ctx, changed, func(*FocalCohort, FocalEnrollmentRequest) (Assignment, error) {
			t.Fatal("changed retry invoked callback")
			return Assignment{}, nil
		})
		if !errors.Is(err, ErrFocalClaimConflict) {
			t.Fatalf("changed retry error = %v, want ErrFocalClaimConflict", err)
		}
	})

	t.Run("conversation uniqueness loser", func(t *testing.T) {
		store, _, cohort, request, ctx, _ := cohortStoreFixture(t)
		if err := store.Save(ctx, cohort); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		if _, err := store.Enroll(ctx, request, focalEnrollmentBuilder(t)); err != nil {
			t.Fatalf("Enroll() winner error = %v", err)
		}
		loser := request
		loser.RequestID = uuid.Must(uuid.NewV7())
		_, err := store.Enroll(ctx, loser, func(*FocalCohort, FocalEnrollmentRequest) (Assignment, error) {
			t.Fatal("uniqueness loser invoked callback")
			return Assignment{}, nil
		})
		if !errors.Is(err, ErrFocalSlotUnavailable) {
			t.Fatalf("uniqueness loser error = %v, want ErrFocalSlotUnavailable", err)
		}
	})

	t.Run("preexisting assignment without claim", func(t *testing.T) {
		store, ledger, cohort, request, ctx, pool := cohortStoreFixture(t)
		if err := store.Save(ctx, cohort); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		if _, err := ledger.RecordAssignment(ctx, focalEnrollmentAssignment(t, cohort, request)); err != nil {
			t.Fatalf("RecordAssignment() error = %v", err)
		}
		_, err := store.Enroll(ctx, request, func(*FocalCohort, FocalEnrollmentRequest) (Assignment, error) {
			t.Fatal("orphan assignment invoked callback")
			return Assignment{}, nil
		})
		if !errors.Is(err, ErrFocalClaimConflict) {
			t.Fatalf("orphan assignment error = %v, want ErrFocalClaimConflict", err)
		}
		assertFocalEnrollmentCounts(t, ctx, pool, request.OwnerID, cohort.ID(), 0, 1)
	})
}

func TestCohortStoreEnrollmentRequiresOwnerScopedConversation(t *testing.T) {
	store, _, cohort, request, ctx, pool := cohortStoreFixture(t)
	if err := store.Save(ctx, cohort); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	for _, conversationID := range []uuid.UUID{
		uuid.Must(uuid.NewV7()),
		foreignEvaluationConversation(t, ctx, pool),
	} {
		invalid := request
		invalid.EvaluationConversationID = conversationID
		_, err := store.Enroll(ctx, invalid, func(*FocalCohort, FocalEnrollmentRequest) (Assignment, error) {
			t.Fatal("invalid conversation invoked callback")
			return Assignment{}, nil
		})
		if err == nil {
			t.Fatal("Enroll() fabricated or foreign conversation error = nil")
		}
	}
}

func TestCohortStoreEnrollRejectsChangedCohortAssignmentBinding(t *testing.T) {
	store, _, cohort, request, ctx, pool := cohortStoreFixture(t)
	if err := store.Save(ctx, cohort); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*Assignment)
	}{
		{
			name: "eligibility hash",
			mutate: func(assignment *Assignment) {
				assignment.EligibilitySHA256 = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
			},
		},
		{
			name: "catalog hash",
			mutate: func(assignment *Assignment) {
				assignment.CatalogSHA256 = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
			},
		},
		{
			name: "static champion",
			mutate: func(assignment *Assignment) {
				assignment.ChampionActionID = "graph_expansion"
			},
		},
		{
			name: "arm propensity",
			mutate: func(assignment *Assignment) {
				probability := 0.25
				assignment.ArmProbability = &probability
			},
		},
		{
			name: "provider",
			mutate: func(assignment *Assignment) {
				assignment.ProviderID = "other-provider"
			},
		},
		{
			name: "cohort identity",
			mutate: func(assignment *Assignment) {
				cohortID := uuid.Must(uuid.NewV7())
				assignment.CohortID = &cohortID
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := request
			candidate.RequestID = uuid.Must(uuid.NewV7())
			_, err := store.Enroll(ctx, candidate, func(cohort *FocalCohort, request FocalEnrollmentRequest) (Assignment, error) {
				assignment := focalEnrollmentAssignment(t, cohort, request)
				test.mutate(&assignment)
				return assignment, nil
			})
			if !errors.Is(err, ErrFocalClaimConflict) {
				t.Fatalf("Enroll() error = %v, want ErrFocalClaimConflict", err)
			}
			assertFocalEnrollmentCounts(t, ctx, pool, request.OwnerID, cohort.ID(), 0, 0)
		})
	}
}

func TestCohortStoreEnrollSerializesSameConversationRace(t *testing.T) {
	store, _, cohort, winner, ctx, pool := cohortStoreFixture(t)
	if err := store.Save(ctx, cohort); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	loser := winner
	loser.RequestID = uuid.Must(uuid.NewV7())
	start := make(chan struct{})
	errs := make(chan error, 2)
	var builds atomic.Int32
	var wait sync.WaitGroup
	for _, request := range []FocalEnrollmentRequest{winner, loser} {
		wait.Add(1)
		go func(request FocalEnrollmentRequest) {
			defer wait.Done()
			<-start
			_, err := store.Enroll(ctx, request, func(cohort *FocalCohort, request FocalEnrollmentRequest) (Assignment, error) {
				builds.Add(1)
				return focalEnrollmentAssignment(t, cohort, request), nil
			})
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
			t.Fatalf("race enrollment error = %v", err)
		}
	}
	if successes != 1 || unavailable != 1 || builds.Load() != 1 {
		t.Fatalf("successes/unavailable/builds = %d/%d/%d, want 1/1/1", successes, unavailable, builds.Load())
	}
	assertFocalEnrollmentCounts(t, ctx, pool, winner.OwnerID, cohort.ID(), 1, 1)
}

func TestCohortStoreEnrollRejectsSameConversationAcrossCohorts(t *testing.T) {
	store, _, first, request, ctx, pool := cohortStoreFixture(t)
	if err := store.Save(ctx, first); err != nil {
		t.Fatalf("Save(first) error = %v", err)
	}
	snapshot, err := NewSnapshotStore(pool).Load(ctx, request.OwnerID, first.Scope().SnapshotID)
	if err != nil {
		t.Fatalf("Load(snapshot) error = %v", err)
	}
	second := focalCohortForSnapshot(
		t,
		snapshot,
		time.Now().Add(2*time.Hour),
		[]BlockKey{BlockEpisode, BlockOwner, BlockSession, BlockTimeBlock},
	)
	if err := store.Save(ctx, second); err != nil {
		t.Fatalf("Save(second) error = %v", err)
	}
	var callbacks atomic.Int32
	if _, err := store.Enroll(ctx, request, func(cohort *FocalCohort, request FocalEnrollmentRequest) (Assignment, error) {
		callbacks.Add(1)
		return focalEnrollmentAssignment(t, cohort, request), nil
	}); err != nil {
		t.Fatalf("Enroll(first) error = %v", err)
	}
	secondRequest := request
	secondRequest.CohortID = second.ID()
	_, err = store.Enroll(ctx, secondRequest, func(*FocalCohort, FocalEnrollmentRequest) (Assignment, error) {
		callbacks.Add(1)
		t.Fatal("cross-cohort uniqueness loser invoked callback")
		return Assignment{}, nil
	})
	if !errors.Is(err, ErrFocalSlotUnavailable) {
		t.Fatalf("Enroll(second) error = %v, want ErrFocalSlotUnavailable", err)
	}
	if callbacks.Load() != 1 {
		t.Fatalf("callback calls = %d, want 1", callbacks.Load())
	}
	assertOwnerFocalEnrollmentCounts(t, ctx, pool, request.OwnerID, 1, 1)
}

func cohortStoreFixture(t *testing.T) (*CohortStore, *Store, *FocalCohort, FocalEnrollmentRequest, context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	t.Cleanup(cancel)
	pool, _ := schema2MigrationDatabase(t, ctx, "aura_cohort_store", 73)
	owner := seedTypedLedgerOwner(t, pool)
	snapshot := focalCohortSnapshotForOwner(t, owner)
	if err := NewSnapshotStore(pool).Save(ctx, snapshot); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}
	cohort := focalCohortForSnapshot(
		t,
		snapshot,
		time.Now().Add(time.Hour),
		[]BlockKey{BlockEpisode, BlockOwner, BlockSession, BlockTimeBlock},
	)
	conversationID := evaluationConversation(t, ctx, pool, owner)
	ledger := NewStore(pool, StoreConfig{})
	return NewCohortStore(pool, ledger), ledger, cohort, FocalEnrollmentRequest{
		OwnerID:                  owner,
		CohortID:                 cohort.ID(),
		RequestID:                uuid.Must(uuid.NewV7()),
		EvaluationConversationID: conversationID,
		SessionID:                uuidPointer(uuid.Must(uuid.NewV7())),
		EpisodeID:                uuidPointer(uuid.Must(uuid.NewV7())),
		Domain:                   cohort.Domain(),
		Point:                    cohort.Predicate().Point,
		PointOrdinal:             cohort.Predicate().Ordinal,
	}, ctx, pool
}

func focalEnrollmentBuilder(t *testing.T) AssignmentBuilder {
	t.Helper()
	return func(cohort *FocalCohort, request FocalEnrollmentRequest) (Assignment, error) {
		return focalEnrollmentAssignment(t, cohort, request), nil
	}
}

func focalEnrollmentAssignment(t *testing.T, cohort *FocalCohort, request FocalEnrollmentRequest) Assignment {
	t.Helper()
	assignmentID, err := AssignmentIDForPoint(request.OwnerID, request.RequestID, request.Point, request.PointOrdinal)
	if err != nil {
		t.Fatal(err)
	}
	actions := cohort.Actions()
	eligibleActions, eligibilitySHA256, catalogSHA256 := mustCohortAssignmentHashes(t, actions)
	arms := cohort.Arms()
	cohortID := cohort.ID()
	return Assignment{
		SchemaVersion:       SchemaVersion2,
		AssignmentID:        assignmentID,
		OwnerID:             request.OwnerID,
		RequestID:           request.RequestID,
		Domain:              request.Domain,
		Point:               request.Point,
		PointOrdinal:        request.PointOrdinal,
		PolicyEpoch:         cohort.Scope().PolicyEpoch,
		PolicyVersion:       cohort.Scope().PolicyVersion,
		PolicyMode:          PolicyCanary,
		SnapshotID:          cohort.Scope().SnapshotID,
		SnapshotSHA256:      cohort.Scope().SnapshotSHA256,
		Environment:         EvaluationProductionCanary,
		ProviderID:          cohort.Scope().ProviderID,
		ModelID:             cohort.Scope().ModelID,
		CohortID:            &cohortID,
		EligibleActions:     eligibleActions,
		EligibilitySHA256:   eligibilitySHA256,
		CatalogSHA256:       catalogSHA256,
		ChampionActionID:    StaticActionID,
		RecommendedActionID: actions[0].ActionID,
		IntendedActionID:    actions[0].ActionID,
		ExperimentID:        cohort.ExperimentID(),
		ArmID:               arms[0].ArmID,
		ArmProbability:      &arms[0].Probability,
		ActionProbabilities: actions,
		SelectionReason:     SelectionCanaryAssignment,
		Features:            map[FeatureKey]float64{},
	}
}

func evaluationConversation(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx, `INSERT INTO aura.conversations (id, identity_id) VALUES ($1,$2)`, id, ownerID); err != nil {
		t.Fatal(err)
	}
	return id
}

func foreignEvaluationConversation(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ownerID := seedTypedLedgerOwner(t, pool)
	return evaluationConversation(t, ctx, pool, ownerID)
}

func assertFocalEnrollmentCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerID, cohortID uuid.UUID, wantClaims, wantAssignments int) {
	t.Helper()
	var claims, assignments int
	err := db.WithIdentityTxRaw(ctx, pool, ownerID.String(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM aura.adaptive_focal_cohort_claims WHERE cohort_id=$1`, cohortID).Scan(&claims); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM aura.adaptive_outbox WHERE owner_id=$1 AND event_kind='decision' AND payload->>'schema_version'='2.0'`, ownerID).Scan(&assignments)
	})
	if err != nil {
		t.Fatal(err)
	}
	if claims != wantClaims || assignments != wantAssignments {
		t.Fatalf("claims/assignments = %d/%d, want %d/%d", claims, assignments, wantClaims, wantAssignments)
	}
}

func assertOwnerFocalEnrollmentCounts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ownerID uuid.UUID, wantClaims, wantAssignments int) {
	t.Helper()
	var claims, assignments int
	err := db.WithIdentityTxRaw(ctx, pool, ownerID.String(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM aura.adaptive_focal_cohort_claims WHERE owner_id=$1`, ownerID).Scan(&claims); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM aura.adaptive_outbox WHERE owner_id=$1 AND event_kind='decision' AND payload->>'schema_version'='2.0'`, ownerID).Scan(&assignments)
	})
	if err != nil {
		t.Fatal(err)
	}
	if claims != wantClaims || assignments != wantAssignments {
		t.Fatalf("owner claims/assignments = %d/%d, want %d/%d", claims, assignments, wantClaims, wantAssignments)
	}
}

func assertFocalCohortsEqual(t *testing.T, got, want *FocalCohort) {
	t.Helper()
	gotArtifact, gotErr := got.canonicalArtifact()
	wantArtifact, wantErr := want.canonicalArtifact()
	if gotErr != nil || wantErr != nil {
		t.Fatalf("canonical artifacts = (%v, %v)", gotErr, wantErr)
	}
	if got.ID() != want.ID() || got.SHA256() != want.SHA256() || string(gotArtifact) != string(wantArtifact) {
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
	if gotEvent.ID != wantEvent.ID || string(gotEvent.Payload) != string(wantEvent.Payload) {
		t.Fatalf("replayed assignment = %#v, want %#v", got, want)
	}
}

func mustCohortAssignmentHashes(t *testing.T, actions []ActionProbability) ([]string, string, string) {
	t.Helper()
	eligibleActions, eligibilitySHA256, catalogSHA256, err := cohortAssignmentHashes(actions)
	if err != nil {
		t.Fatal(err)
	}
	return eligibleActions, eligibilitySHA256, catalogSHA256
}

func uuidPointer(id uuid.UUID) *uuid.UUID { return &id }
