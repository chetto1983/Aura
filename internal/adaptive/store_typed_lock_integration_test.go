//go:build db_integration

package adaptive

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestStoreRecordDeliveryHoldsAssignmentLockThroughInsert(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	owner := seedTypedLedgerOwner(t, pool)
	store := NewStore(pool, StoreConfig{})
	assignment := typedLedgerAssignment(t, owner)
	if _, err := store.RecordAssignment(ctx, assignment); err != nil {
		t.Fatalf("RecordAssignment: %v", err)
	}
	delivery := typedLedgerDelivery(t, assignment)
	deliveryEvent, err := NewDeliveryEvent(assignment, delivery)
	if err != nil {
		t.Fatal(err)
	}

	migratePool := typedLedgerMigrationPool(t, ctx)
	deliveryBlock := installTypedDeliveryBlocker(t, ctx, migratePool, deliveryEvent.ID)

	blocker, err := migratePool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackTypedTestTx(t, blocker)
	if _, err := blocker.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock($1, $2)`,
		deliveryBlock.advisoryClass,
		deliveryBlock.advisoryObject,
	); err != nil {
		t.Fatalf("hold delivery trigger lock: %v", err)
	}

	recordDone := recordTypedDeliveryAsync(ctx, store, owner, assignment, delivery)
	waitForTypedDeliveryInsertBlock(
		t,
		ctx,
		migratePool,
		deliveryBlock.advisoryClass,
		deliveryBlock.advisoryObject,
	)

	contender, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackTypedTestTx(t, contender)
	if _, err := contender.Exec(
		ctx,
		`SELECT set_config('app.current_identity', $1, true)`,
		owner.String(),
	); err != nil {
		t.Fatal(err)
	}
	_, err = contender.Exec(
		ctx,
		`SELECT id FROM aura.adaptive_outbox WHERE id=$1 FOR UPDATE NOWAIT`,
		mustEventID(assignment.AssignmentID, EventDecision, "assignment"),
	)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("contending assignment lock error = %v, want SQLSTATE 55P03", err)
	}
	if err := contender.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	fenceTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin fence transaction: %v", err)
	}
	defer rollbackTypedTestTx(t, fenceTx)
	var fencePID int32
	if err := fenceTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&fencePID); err != nil {
		t.Fatalf("read fence transaction PID: %v", err)
	}
	fenceDone := make(chan typedFenceResult, 1)
	fenceCompleted := make(chan struct{})
	go func() {
		defer close(fenceCompleted)
		var fenced bool
		fenceErr := fenceTx.QueryRow(
			ctx,
			`SELECT aura.fence_adaptive_identity($1)`,
			owner,
		).Scan(&fenced)
		select {
		case fenceDone <- typedFenceResult{fenced: fenced, err: fenceErr}:
		case <-ctx.Done():
		}
	}()
	waitForAdaptiveBackendBlock(
		t,
		ctx,
		migratePool,
		fencePID,
		fenceCompleted,
	)

	if err := blocker.Commit(ctx); err != nil {
		t.Fatalf("release delivery trigger lock: %v", err)
	}
	select {
	case result := <-recordDone:
		if result.err != nil || result.sequence != 2 {
			t.Fatalf("RecordDelivery after insert unblock = (%d, %v), want (2, nil)", result.sequence, result.err)
		}
	case <-ctx.Done():
		t.Fatalf("RecordDelivery did not finish after insert unblock: %v", ctx.Err())
	}
	select {
	case result := <-fenceDone:
		if result.err != nil || !result.fenced {
			t.Fatalf("fence after delivery = (%t, %v), want (true, nil)", result.fenced, result.err)
		}
	case <-ctx.Done():
		t.Fatalf("fence did not finish after delivery commit: %v", ctx.Err())
	}
	if err := fenceTx.Commit(ctx); err != nil {
		t.Fatalf("commit delivery-following fence: %v", err)
	}
	if _, err := store.RecordDelivery(
		ctx,
		owner,
		assignment.AssignmentID,
		delivery,
	); !errors.Is(err, ErrOwnerTombstoned) {
		t.Fatalf("RecordDelivery after committed fence error = %v, want ErrOwnerTombstoned", err)
	}
	assertOneTypedDeliveryAndNextSequence(t, ctx, store, assignment, 3)
}

type typedDeliveryBlock struct {
	advisoryClass  int32
	advisoryObject int32
}

func installTypedDeliveryBlocker(
	t *testing.T,
	ctx context.Context,
	migratePool interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	},
	deliveryEventID uuid.UUID,
) typedDeliveryBlock {
	t.Helper()
	suffix := strings.ReplaceAll(uuid.Must(uuid.NewV7()).String(), "-", "")
	functionName := "test_typed_delivery_block_" + suffix
	triggerName := "test_typed_delivery_block_" + suffix
	advisoryClass := int32(binary.BigEndian.Uint32(deliveryEventID[0:4]) & 0x7fffffff)
	advisoryObject := int32(binary.BigEndian.Uint32(deliveryEventID[4:8]) & 0x7fffffff)
	functionSQL := fmt.Sprintf(`
CREATE FUNCTION aura.%s() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.id = '%s'::uuid THEN
        PERFORM pg_advisory_xact_lock(%d, %d);
    END IF;
    RETURN NEW;
END;
$$`,
		functionName,
		deliveryEventID,
		advisoryClass,
		advisoryObject,
	)
	if _, err := migratePool.Exec(ctx, functionSQL); err != nil {
		t.Fatalf("create delivery blocker function: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := migratePool.Exec(
			cleanupCtx,
			fmt.Sprintf(`DROP FUNCTION IF EXISTS aura.%s()`, functionName),
		); err != nil {
			t.Errorf("drop delivery blocker function: %v", err)
		}
	})
	if _, err := migratePool.Exec(
		ctx,
		fmt.Sprintf(
			`CREATE TRIGGER %s BEFORE INSERT ON aura.adaptive_outbox
			 FOR EACH ROW EXECUTE FUNCTION aura.%s()`,
			triggerName,
			functionName,
		),
	); err != nil {
		t.Fatalf("create delivery blocker trigger: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := migratePool.Exec(
			cleanupCtx,
			fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON aura.adaptive_outbox`, triggerName),
		); err != nil {
			t.Errorf("drop delivery blocker trigger: %v", err)
		}
	})
	return typedDeliveryBlock{
		advisoryClass:  advisoryClass,
		advisoryObject: advisoryObject,
	}
}

type typedFenceResult struct {
	fenced bool
	err    error
}

func waitForAdaptiveBackendBlock(
	t *testing.T,
	ctx context.Context,
	querier interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	blockedPID int32,
	completed <-chan struct{},
) {
	t.Helper()
	for {
		var waiting bool
		if err := querier.QueryRow(
			ctx,
			`SELECT cardinality(pg_blocking_pids($1)) > 0`,
			blockedPID,
		).Scan(&waiting); err != nil {
			t.Fatalf("observe adaptive backend block: %v", err)
		}
		if waiting {
			return
		}
		select {
		case <-completed:
			t.Fatal("adaptive operation completed before the expected owner lock wait")
		case <-ctx.Done():
			t.Fatalf("adaptive operation did not reach owner lock wait: %v", ctx.Err())
		default:
		}
	}
}

func waitForTypedDeliveryInsertBlock(
	t *testing.T,
	ctx context.Context,
	querier interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	advisoryClass int32,
	advisoryObject int32,
) {
	t.Helper()
	for {
		var waiting bool
		if err := querier.QueryRow(
			ctx,
			`SELECT EXISTS (
			    SELECT 1
			    FROM pg_locks
			    WHERE locktype = 'advisory'
			      AND classid = $1::oid
			      AND objid = $2::oid
			      AND objsubid = 2
			      AND NOT granted
			)`,
			advisoryClass,
			advisoryObject,
		).Scan(&waiting); err != nil {
			t.Fatalf("observe blocked RecordDelivery: %v", err)
		}
		if waiting {
			return
		}
		if err := ctx.Err(); err != nil {
			t.Fatalf("RecordDelivery did not reach blocked INSERT: %v", err)
		}
	}
}
