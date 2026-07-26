//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStoreRecordDeliveryLocksOwnerBeforeAssignment(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	store := NewStore(pool, StoreConfig{})
	if _, err := store.RecordAssignment(ctx, assignment); err != nil {
		t.Fatalf("RecordAssignment: %v", err)
	}

	migratePool := typedLedgerMigrationPool(t, ctx)
	ownerBlocker, err := migratePool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin owner blocker: %v", err)
	}
	defer rollbackTypedTestTx(t, ownerBlocker)
	var blockerPID int32
	if err := ownerBlocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatalf("read owner blocker PID: %v", err)
	}
	if _, err := ownerBlocker.Exec(
		ctx,
		`SELECT id FROM aura.identities WHERE id=$1 FOR UPDATE`,
		owner,
	); err != nil {
		t.Fatalf("lock owner row: %v", err)
	}

	delivery := typedLedgerDelivery(t, assignment)
	recordDone := recordTypedDeliveryAsync(ctx, store, owner, assignment, delivery)
	waitForAdaptiveOwnerLock(t, ctx, migratePool, blockerPID, recordDone)

	assignmentContender, err := migratePool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin assignment contender: %v", err)
	}
	defer rollbackTypedTestTx(t, assignmentContender)
	if _, err := assignmentContender.Exec(
		ctx,
		`SELECT id
		 FROM aura.adaptive_outbox
		 WHERE owner_id=$1 AND decision_id=$2 AND event_kind='decision'
		 FOR UPDATE NOWAIT`,
		owner,
		assignment.AssignmentID,
	); err != nil {
		t.Fatalf("assignment locked before owner lock completed: %v", err)
	}
	if err := assignmentContender.Rollback(ctx); err != nil {
		t.Fatalf("release assignment contender: %v", err)
	}
	if err := ownerBlocker.Commit(ctx); err != nil {
		t.Fatalf("release owner blocker: %v", err)
	}
	select {
	case result := <-recordDone:
		if result.err != nil || result.sequence != 2 {
			t.Fatalf("RecordDelivery after owner unblock = (%d, %v), want (2, nil)", result.sequence, result.err)
		}
	case <-ctx.Done():
		t.Fatalf("RecordDelivery did not finish after owner unblock: %v", ctx.Err())
	}
}

func TestStoreValidatedWritersLockOwnerBeforeEvent(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	tests := []struct {
		name  string
		build func(*testing.T, *pgxpool.Pool, uuid.UUID, *Store) (uuid.UUID, func(context.Context) (int64, error))
	}{
		{
			name: "recordValidated",
			build: func(t *testing.T, _ *pgxpool.Pool, owner uuid.UUID, store *Store) (uuid.UUID, func(context.Context) (int64, error)) {
				event := typedSchema1Event(t, owner)
				return event.ID, func(ctx context.Context) (int64, error) {
					return store.recordLegacyEvent(ctx, event)
				}
			},
		},
		{
			name: "recordValidatedTx",
			build: func(t *testing.T, pool *pgxpool.Pool, owner uuid.UUID, store *Store) (uuid.UUID, func(context.Context) (int64, error)) {
				event := typedSchema1Event(t, owner)
				return event.ID, func(ctx context.Context) (int64, error) {
					var sequence int64
					err := db.WithIdentityTx(ctx, pool, owner.String(), func(q *sqlc.Queries) error {
						var recordErr error
						sequence, _, recordErr = store.recordLegacyEventTx(ctx, q, event)
						return recordErr
					})
					return sequence, err
				}
			},
		},
		{
			name: "RecordAssignment",
			build: func(t *testing.T, _ *pgxpool.Pool, owner uuid.UUID, store *Store) (uuid.UUID, func(context.Context) (int64, error)) {
				assignment := typedLedgerAssignment(t, owner)
				event, err := NewAssignmentEvent(assignment)
				if err != nil {
					t.Fatal(err)
				}
				return event.ID, func(ctx context.Context) (int64, error) {
					return store.RecordAssignment(ctx, assignment)
				}
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			owner := seedTypedLedgerOwner(t, pool)
			store := NewStore(pool, StoreConfig{})
			eventID, write := test.build(t, pool, owner, store)
			migratePool := typedLedgerMigrationPool(t, ctx)
			ownerBlocker, err := migratePool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin owner blocker: %v", err)
			}
			defer rollbackTypedTestTx(t, ownerBlocker)
			var blockerPID int32
			if err := ownerBlocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
				t.Fatalf("read owner blocker PID: %v", err)
			}
			if _, err := ownerBlocker.Exec(
				ctx,
				`SELECT id FROM aura.identities WHERE id=$1 FOR UPDATE`,
				owner,
			); err != nil {
				t.Fatalf("lock owner row: %v", err)
			}

			writeDone := make(chan typedDeliveryRecordResult, 1)
			go func() {
				sequence, writeErr := write(ctx)
				select {
				case writeDone <- typedDeliveryRecordResult{sequence: sequence, err: writeErr}:
				case <-ctx.Done():
				}
			}()
			waitForAdaptiveOwnerLock(
				t,
				ctx,
				migratePool,
				blockerPID,
				writeDone,
			)

			eventContender, err := migratePool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin event contender: %v", err)
			}
			defer rollbackTypedTestTx(t, eventContender)
			var eventLockAvailable bool
			if err := eventContender.QueryRow(
				ctx,
				`SELECT pg_try_advisory_xact_lock(hashtextextended($1::text, 0))`,
				eventID.String(),
			).Scan(&eventLockAvailable); err != nil {
				t.Fatalf("try event advisory lock: %v", err)
			}
			if !eventLockAvailable {
				t.Fatal("writer locked event before completing the owner lock")
			}
			if err := eventContender.Rollback(ctx); err != nil {
				t.Fatalf("release event contender: %v", err)
			}
			if err := ownerBlocker.Commit(ctx); err != nil {
				t.Fatalf("release owner blocker: %v", err)
			}
			select {
			case result := <-writeDone:
				if result.err != nil || result.sequence != 1 {
					t.Fatalf("%s after owner unblock = (%d, %v), want (1, nil)", test.name, result.sequence, result.err)
				}
			case <-ctx.Done():
				t.Fatalf("%s did not finish after owner unblock: %v", test.name, ctx.Err())
			}
		})
	}
}

func typedSchema1Event(t *testing.T, owner uuid.UUID) Event {
	t.Helper()
	event, err := newLegacyEvent(eventParams{
		ID:          uuid.Must(uuid.NewV7()),
		OwnerID:     owner,
		AggregateID: "owner-lock-" + uuid.Must(uuid.NewV7()).String(),
		DecisionID:  uuid.Must(uuid.NewV7()),
		Kind:        EventDecision,
		Payload:     []byte(`{"schema_version":"1.0"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func waitForAdaptiveOwnerLock(
	t *testing.T,
	ctx context.Context,
	querier interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	blockerPID int32,
	recordDone <-chan typedDeliveryRecordResult,
) {
	t.Helper()
	for {
		var waiting bool
		if err := querier.QueryRow(
			ctx,
			`SELECT EXISTS (
			    SELECT 1
			    FROM pg_stat_activity
			    WHERE $1::integer = ANY(pg_blocking_pids(pid))
			)`,
			blockerPID,
		).Scan(&waiting); err != nil {
			t.Fatalf("observe owner lock waiter: %v", err)
		}
		if waiting {
			return
		}
		select {
		case result := <-recordDone:
			t.Fatalf(
				"adaptive writer bypassed owner lock = (%d, %v)",
				result.sequence,
				result.err,
			)
		case <-ctx.Done():
			t.Fatalf("RecordDelivery did not wait on owner lock: %v", ctx.Err())
		default:
		}
	}
}

func rollbackTypedTestTx(t *testing.T, tx pgx.Tx) {
	t.Helper()
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()
	if err := tx.Rollback(cleanupCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Errorf("rollback typed test transaction: %v", err)
	}
}
