//go:build db_integration

package adaptive

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSchema2LockedAuditRejectsConcurrentNoncanonicalWriter(t *testing.T) {
	runSchema2ConcurrentUpgrade(t, "aura_schema2_locked_audit_invalid", false)
}

func TestSchema2LockedAuditAcceptsConcurrentCanonicalWriter(t *testing.T) {
	runSchema2ConcurrentUpgrade(t, "aura_schema2_locked_audit_canonical", true)
}

func TestSchema2AuditShareLockBlocksLedgerWrites(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, migrateURL := schema2MigrationDatabase(
		t, ctx, "aura_schema2_share_lock_writes", 64,
	)
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerEvent(ctx, pool, event, 1); err != nil {
		t.Fatalf("seed lock fixture: %v", err)
	}
	insertAssignment := typedLedgerAssignment(t, owner)
	insertEvent, err := NewAssignmentEvent(insertAssignment)
	if err != nil {
		t.Fatal(err)
	}
	migrationPool, err := db.Open(ctx, &db.Config{URL: migrateURL})
	if err != nil {
		t.Fatal(err)
	}
	defer migrationPool.Close()
	before := schema2LedgerSnapshotForOwner(t, ctx, pool, owner)
	tests := []struct {
		name  string
		write func(context.Context, pgx.Tx) error
	}{
		{
			name: "insert",
			write: func(ctx context.Context, tx pgx.Tx) error {
				return insertAdaptiveLedgerRow(
					ctx, tx, insertEvent.ID, owner,
					insertAssignment.RequestID.String(), 1,
					insertAssignment.AssignmentID,
					EventDecision, insertEvent.Payload,
				)
			},
		},
		{
			name: "update",
			write: func(ctx context.Context, tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `
UPDATE aura.adaptive_outbox
SET available_at = available_at
WHERE id = $1`, event.ID)
				return err
			},
		},
		{
			name: "delete",
			write: func(ctx context.Context, tx pgx.Tx) error {
				_, err := tx.Exec(
					ctx,
					`DELETE FROM aura.adaptive_outbox WHERE id = $1`,
					event.ID,
				)
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			locker, err := migrationPool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = locker.Rollback(context.Background()) }()
			if _, err := locker.Exec(
				ctx,
				`LOCK TABLE aura.adaptive_outbox IN SHARE MODE`,
			); err != nil {
				t.Fatalf("hold schema-2 audit lock: %v", err)
			}
			writerConnection, err := migrationPool.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer writerConnection.Release()
			writer, err := writerConnection.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = writer.Rollback(context.Background()) }()
			writeDone := make(chan error, 1)
			go func() {
				writeDone <- tt.write(ctx, writer)
			}()
			waitForSchema2WriterTableLock(
				t, ctx, pool, writerConnection.Conn().PgConn().PID(), writeDone,
			)
			if err := locker.Rollback(ctx); err != nil {
				t.Fatalf("release schema-2 audit lock: %v", err)
			}
			if err := waitForSchema2Operation(t, ctx, writeDone); err != nil {
				t.Fatalf("%s after audit lock release: %v", tt.name, err)
			}
			if err := writer.Rollback(ctx); err != nil {
				t.Fatalf("roll back %s fixture: %v", tt.name, err)
			}
		})
	}
	after := schema2LedgerSnapshotForOwner(t, ctx, pool, owner)
	if after != before {
		t.Fatalf(
			"lock compatibility test mutated ledger: before=%+v after=%+v",
			before, after,
		)
	}
}

func runSchema2ConcurrentUpgrade(
	t *testing.T,
	database string,
	canonical bool,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool, migrateURL := schema2MigrationDatabase(t, ctx, database, 62)
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	rowID := uuid.Must(uuid.NewV7())
	if canonical {
		rowID = event.ID
	}
	writer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Rollback(context.Background()) }()
	if err := insertAdaptiveLedgerRow(
		ctx, writer, rowID, owner, assignment.RequestID.String(),
		1, assignment.AssignmentID, EventDecision, event.Payload,
	); err != nil {
		t.Fatalf("seed uncommitted assignment: %v", err)
	}
	before := schema2LedgerSnapshotForOwner(t, ctx, writer, owner)

	migrationDone := make(chan error, 1)
	migrationStarted := make(chan struct{})
	go func() {
		close(migrationStarted)
		migrationDone <- db.MigrateSteps(ctx, migrateURL, 2)
	}()
	<-migrationStarted
	waitForSchema2MigrationTableLock(t, ctx, pool, migrationDone)
	if err := writer.Commit(ctx); err != nil {
		t.Fatalf("commit concurrent writer: %v", err)
	}
	err = waitForSchema2Operation(t, ctx, migrationDone)
	after := schema2LedgerSnapshotForOwner(t, ctx, pool, owner)
	if after != before {
		t.Fatalf(
			"locked audit mutated ledger: before=%+v after=%+v",
			before, after,
		)
	}
	if canonical {
		if err != nil {
			t.Fatalf("canonical concurrent upgrade: %v", err)
		}
		assertSchema2MigrationVersion(t, ctx, migrateURL, 64)
		return
	}
	assertSchema2AuditFailure(
		t, err, "23514",
		"schema-2 locked identity audit found an invalid fact",
	)
	version, dirty := schema2MigrationState(t, ctx, migrateURL)
	if version != 64 || !dirty {
		t.Fatalf(
			"concurrent unsafe upgrade state = version %d dirty %t, want version 64 dirty",
			version, dirty,
		)
	}
}

func waitForSchema2MigrationTableLock(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	migrationDone <-chan error,
) {
	t.Helper()
	waitForSchema2BlockedOperation(
		t, ctx, "migration", migrationDone,
		func() (bool, error) {
			var waiting bool
			if err := pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM pg_locks AS lock
    JOIN pg_stat_activity AS activity ON activity.pid = lock.pid
    WHERE activity.datname = current_database()
      AND activity.usename = 'aura_migrate'
      AND lock.relation = 'aura.adaptive_outbox'::regclass
      AND NOT lock.granted
)`).Scan(&waiting); err != nil {
				return false, err
			}
			return waiting, nil
		},
	)
}

func waitForSchema2BlockedOperation(
	t *testing.T,
	ctx context.Context,
	name string,
	done <-chan error,
	observe func() (bool, error),
) {
	t.Helper()
	for {
		select {
		case err := <-done:
			t.Fatalf("%s finished before reaching table lock: %v", name, err)
		default:
		}
		waiting, err := observe()
		if err != nil {
			t.Fatalf("observe blocked %s: %v", name, err)
		}
		if waiting {
			return
		}
		if ctx.Err() != nil {
			t.Fatalf("%s did not wait on adaptive_outbox: %v", name, ctx.Err())
		}
		runtime.Gosched()
	}
}

func waitForSchema2WriterTableLock(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	pid uint32,
	writeDone <-chan error,
) {
	t.Helper()
	waitForSchema2BlockedOperation(
		t, ctx, "writer", writeDone,
		func() (bool, error) {
			var waiting bool
			if err := pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM pg_locks
    WHERE pid = $1
      AND relation = 'aura.adaptive_outbox'::regclass
      AND mode = 'RowExclusiveLock'
      AND NOT granted
)`, pid).Scan(&waiting); err != nil {
				return false, err
			}
			return waiting, nil
		},
	)
}

func waitForSchema2Operation(
	t *testing.T,
	ctx context.Context,
	migrationDone <-chan error,
) error {
	t.Helper()
	select {
	case err := <-migrationDone:
		return err
	case <-ctx.Done():
		t.Fatalf("migration did not finish: %v", ctx.Err())
		return nil
	}
}
