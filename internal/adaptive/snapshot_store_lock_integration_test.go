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
	"github.com/jackc/pgx/v5/pgconn"
)

func TestSnapshotStoreSerializesSaveBeforeOwnerFence(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	owner := seedTypedLedgerOwner(t, pool)
	snapshot := snapshotForOwner(t, owner)
	store := NewSnapshotStore(pool)
	migratePool := typedLedgerMigrationPool(t, ctx)
	block := installSnapshotInsertBlocker(
		t,
		ctx,
		migratePool,
		snapshot.ID(),
	)

	blocker, err := migratePool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackTypedTestTx(t, blocker)
	if _, err := blocker.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock($1, $2)`,
		block.advisoryClass,
		block.advisoryObject,
	); err != nil {
		t.Fatalf("hold snapshot insert blocker: %v", err)
	}

	saveDone := make(chan error, 1)
	saveCompleted := make(chan struct{})
	go func() {
		defer close(saveCompleted)
		err := store.Save(ctx, snapshot)
		select {
		case saveDone <- err:
		case <-ctx.Done():
		}
	}()
	waitForTypedDeliveryInsertBlock(
		t,
		ctx,
		migratePool,
		block.advisoryClass,
		block.advisoryObject,
	)

	fenceTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackTypedTestTx(t, fenceTx)
	var fencePID int32
	if err := fenceTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&fencePID); err != nil {
		t.Fatal(err)
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
		t.Fatalf("release snapshot insert blocker: %v", err)
	}
	select {
	case err := <-saveDone:
		if err != nil {
			t.Fatalf("Save() after blocker release error = %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("Save() did not finish after blocker release: %v", ctx.Err())
	}
	select {
	case result := <-fenceDone:
		if result.err != nil || !result.fenced {
			t.Fatalf("fence after Save() = (%t, %v), want (true, nil)", result.fenced, result.err)
		}
	case <-ctx.Done():
		t.Fatalf("fence did not finish after Save(): %v", ctx.Err())
	}
	if err := fenceTx.Commit(ctx); err != nil {
		t.Fatalf("commit snapshot-following fence: %v", err)
	}
	if err := store.Save(ctx, snapshot); !errors.Is(err, ErrOwnerTombstoned) {
		t.Fatalf("Save() after committed fence error = %v, want ErrOwnerTombstoned", err)
	}
}

type snapshotInsertBlock struct {
	advisoryClass  int32
	advisoryObject int32
}

func installSnapshotInsertBlocker(
	t *testing.T,
	ctx context.Context,
	migratePool interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	},
	snapshotID uuid.UUID,
) snapshotInsertBlock {
	t.Helper()
	suffix := strings.ReplaceAll(uuid.Must(uuid.NewV7()).String(), "-", "")
	functionName := "test_snapshot_insert_block_" + suffix
	triggerName := "test_snapshot_insert_block_" + suffix
	advisoryClass := int32(binary.BigEndian.Uint32(snapshotID[0:4]) & 0x7fffffff)
	advisoryObject := int32(binary.BigEndian.Uint32(snapshotID[4:8]) & 0x7fffffff)
	functionSQL := fmt.Sprintf(`
CREATE FUNCTION aura.%s() RETURNS trigger
LANGUAGE plpgsql
SET search_path = pg_catalog
AS $$
BEGIN
    IF NEW.id = '%s'::uuid THEN
        PERFORM pg_advisory_xact_lock(%d, %d);
    END IF;
    RETURN NEW;
END;
$$`,
		functionName,
		snapshotID,
		advisoryClass,
		advisoryObject,
	)
	if _, err := migratePool.Exec(ctx, functionSQL); err != nil {
		t.Fatalf("create snapshot blocker function: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cleanupCancel()
		if _, err := migratePool.Exec(
			cleanupCtx,
			fmt.Sprintf(`DROP FUNCTION IF EXISTS aura.%s()`, functionName),
		); err != nil {
			t.Errorf("drop snapshot blocker function: %v", err)
		}
	})
	if _, err := migratePool.Exec(
		ctx,
		fmt.Sprintf(
			`CREATE TRIGGER %s BEFORE INSERT
			 ON aura.adaptive_policy_snapshots
			 FOR EACH ROW EXECUTE FUNCTION aura.%s()`,
			triggerName,
			functionName,
		),
	); err != nil {
		t.Fatalf("create snapshot blocker trigger: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cleanupCancel()
		if _, err := migratePool.Exec(
			cleanupCtx,
			fmt.Sprintf(
				`DROP TRIGGER IF EXISTS %s
				 ON aura.adaptive_policy_snapshots`,
				triggerName,
			),
		); err != nil {
			t.Errorf("drop snapshot blocker trigger: %v", err)
		}
	})
	return snapshotInsertBlock{
		advisoryClass:  advisoryClass,
		advisoryObject: advisoryObject,
	}
}
