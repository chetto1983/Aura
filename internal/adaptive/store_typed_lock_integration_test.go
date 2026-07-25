//go:build db_integration

package adaptive

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
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

	migratePool, err := db.Open(ctx, &db.Config{URL: os.Getenv("AURA_DB_MIGRATE_URL")})
	if err != nil {
		t.Fatalf("open migration pool: %v", err)
	}
	t.Cleanup(migratePool.Close)
	suffix := strings.ReplaceAll(uuid.Must(uuid.NewV7()).String(), "-", "")
	functionName := "test_typed_delivery_block_" + suffix
	triggerName := "test_typed_delivery_block_" + suffix
	advisoryClass := int32(binary.BigEndian.Uint32(deliveryEvent.ID[0:4]) & 0x7fffffff)
	advisoryObject := int32(binary.BigEndian.Uint32(deliveryEvent.ID[4:8]) & 0x7fffffff)
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
		deliveryEvent.ID,
		advisoryClass,
		advisoryObject,
	)
	if _, err := migratePool.Exec(ctx, functionSQL); err != nil {
		t.Fatalf("create delivery blocker function: %v", err)
	}
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
		_, _ = migratePool.Exec(
			cleanupCtx,
			fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON aura.adaptive_outbox`, triggerName),
		)
		_, _ = migratePool.Exec(
			cleanupCtx,
			fmt.Sprintf(`DROP FUNCTION IF EXISTS aura.%s()`, functionName),
		)
	})

	blocker, err := migratePool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback(context.Background()) }()
	if _, err := blocker.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock($1, $2)`,
		advisoryClass,
		advisoryObject,
	); err != nil {
		t.Fatalf("hold delivery trigger lock: %v", err)
	}

	recordDone := make(chan typedDeliveryRecordResult, 1)
	go func() {
		sequence, recordErr := store.RecordDelivery(
			ctx,
			owner,
			assignment.AssignmentID,
			delivery,
		)
		recordDone <- typedDeliveryRecordResult{sequence: sequence, err: recordErr}
	}()
	waitForTypedDeliveryInsertBlock(
		t,
		ctx,
		migratePool,
		advisoryClass,
		advisoryObject,
	)

	contender, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = contender.Rollback(context.Background()) }()
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
	if err := blocker.Commit(ctx); err != nil {
		t.Fatalf("release delivery trigger lock: %v", err)
	}
	result := <-recordDone
	if result.err != nil || result.sequence != 2 {
		t.Fatalf("RecordDelivery after insert unblock = (%d, %v), want (2, nil)", result.sequence, result.err)
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
