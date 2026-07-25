//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTypedLedgerOwnerCleanupRemovesPermanentTombstone(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	var owner uuid.UUID
	t.Run("deleted owner", func(t *testing.T) {
		owner = seedTypedLedgerOwner(t, pool)
		if _, err := pool.Exec(
			ctx,
			`DELETE FROM aura.identities WHERE id=$1`,
			owner,
		); err != nil {
			t.Fatalf("delete typed-ledger owner: %v", err)
		}
	})

	migratePool := typedLedgerMigrationPool(t, ctx)
	var identities, tombstones int
	if err := migratePool.QueryRow(
		ctx,
		`SELECT
		    (SELECT count(*) FROM aura.identities WHERE id=$1),
		    (SELECT count(*) FROM aura.adaptive_identity_tombstones WHERE owner_id=$1)`,
		owner,
	).Scan(&identities, &tombstones); err != nil {
		t.Fatalf("read typed-ledger cleanup state: %v", err)
	}
	if identities != 0 || tombstones != 0 {
		t.Fatalf(
			"typed-ledger cleanup left identities=%d tombstones=%d, want 0/0",
			identities,
			tombstones,
		)
	}
}

func TestStoreRecordDeliveryLosesToCommittedFenceWithoutSequenceGap(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	delivery := typedLedgerDelivery(t, assignment)
	store := NewStore(pool, StoreConfig{})
	if _, err := store.RecordAssignment(ctx, assignment); err != nil {
		t.Fatalf("RecordAssignment: %v", err)
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
	var fenced bool
	if err := fenceTx.QueryRow(
		ctx,
		`SELECT aura.fence_adaptive_identity($1)`,
		owner,
	).Scan(&fenced); err != nil {
		t.Fatalf("fence owner: %v", err)
	}
	if !fenced {
		t.Fatal("fence existing owner returned false")
	}

	recordDone := recordTypedDeliveryAsync(ctx, store, owner, assignment, delivery)
	waitForAdaptiveOwnerLock(
		t,
		ctx,
		typedLedgerMigrationPool(t, ctx),
		fencePID,
		recordDone,
	)
	if err := fenceTx.Commit(ctx); err != nil {
		t.Fatalf("commit owner fence: %v", err)
	}
	assertTypedDeliveryTombstoned(t, ctx, recordDone)
	if _, err := store.RecordDelivery(
		ctx,
		owner,
		assignment.AssignmentID,
		delivery,
	); !errors.Is(err, ErrOwnerTombstoned) {
		t.Fatalf("RecordDelivery retry after fence error = %v, want ErrOwnerTombstoned", err)
	}
	assertNoTypedDelivery(t, ctx, pool, owner)
	assertTypedAggregateSequence(t, ctx, pool, owner, assignment.RequestID.String(), 2)
}

func TestStoreRecordDeliveryLosesToDeleteWithStableFence(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	delivery := typedLedgerDelivery(t, assignment)
	store := NewStore(pool, StoreConfig{})
	if _, err := store.RecordAssignment(ctx, assignment); err != nil {
		t.Fatalf("RecordAssignment: %v", err)
	}

	deleteTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin identity delete: %v", err)
	}
	defer rollbackTypedTestTx(t, deleteTx)
	var deletePID int32
	if err := deleteTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&deletePID); err != nil {
		t.Fatalf("read delete transaction PID: %v", err)
	}
	if _, err := deleteTx.Exec(
		ctx,
		`DELETE FROM aura.identities WHERE id=$1`,
		owner,
	); err != nil {
		t.Fatalf("delete owner: %v", err)
	}

	recordDone := recordTypedDeliveryAsync(ctx, store, owner, assignment, delivery)
	waitForAdaptiveOwnerLock(
		t,
		ctx,
		typedLedgerMigrationPool(t, ctx),
		deletePID,
		recordDone,
	)
	if err := deleteTx.Commit(ctx); err != nil {
		t.Fatalf("commit owner delete: %v", err)
	}
	assertTypedDeliveryTombstoned(t, ctx, recordDone)
	recreateTypedLedgerOwner(t, ctx, pool, owner)
	if _, err := store.RecordDelivery(
		ctx,
		owner,
		assignment.AssignmentID,
		delivery,
	); !errors.Is(err, ErrOwnerTombstoned) {
		t.Fatalf("RecordDelivery after owner recreation error = %v, want ErrOwnerTombstoned", err)
	}
	assertNoTypedDelivery(t, ctx, pool, owner)
	assertNoTypedAggregateSequence(t, ctx, pool, owner, assignment.RequestID.String())
}

func TestStoreRecordDeliveryWinsBeforeDeleteWithoutDeadlock(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	delivery := typedLedgerDelivery(t, assignment)
	store := NewStore(pool, StoreConfig{})
	if _, err := store.RecordAssignment(ctx, assignment); err != nil {
		t.Fatalf("RecordAssignment: %v", err)
	}
	deliveryEvent, err := NewDeliveryEvent(assignment, delivery)
	if err != nil {
		t.Fatal(err)
	}
	migratePool := typedLedgerMigrationPool(t, ctx)
	deliveryBlock := installTypedDeliveryBlocker(
		t,
		ctx,
		migratePool,
		deliveryEvent.ID,
	)
	insertBlocker, err := migratePool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin delivery insert blocker: %v", err)
	}
	defer rollbackTypedTestTx(t, insertBlocker)
	if _, err := insertBlocker.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock($1, $2)`,
		deliveryBlock.advisoryClass,
		deliveryBlock.advisoryObject,
	); err != nil {
		t.Fatalf("hold delivery insert blocker: %v", err)
	}

	recordDone := recordTypedDeliveryAsync(ctx, store, owner, assignment, delivery)
	waitForTypedDeliveryInsertBlock(
		t,
		ctx,
		migratePool,
		deliveryBlock.advisoryClass,
		deliveryBlock.advisoryObject,
	)
	deleteTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin identity delete: %v", err)
	}
	defer rollbackTypedTestTx(t, deleteTx)
	var deletePID int32
	if err := deleteTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&deletePID); err != nil {
		t.Fatalf("read delete transaction PID: %v", err)
	}
	deleteDone := make(chan error, 1)
	deleteCompleted := make(chan struct{})
	go func() {
		defer close(deleteCompleted)
		_, deleteErr := deleteTx.Exec(
			ctx,
			`DELETE FROM aura.identities WHERE id=$1`,
			owner,
		)
		select {
		case deleteDone <- deleteErr:
		case <-ctx.Done():
		}
	}()
	waitForAdaptiveBackendBlock(
		t,
		ctx,
		migratePool,
		deletePID,
		deleteCompleted,
	)
	if err := insertBlocker.Commit(ctx); err != nil {
		t.Fatalf("release delivery insert blocker: %v", err)
	}
	select {
	case result := <-recordDone:
		if result.err != nil || result.sequence != 2 {
			t.Fatalf("delivery winner = (%d, %v), want (2, nil)", result.sequence, result.err)
		}
	case <-ctx.Done():
		t.Fatalf("delivery winner did not finish: %v", ctx.Err())
	}
	select {
	case err := <-deleteDone:
		if err != nil {
			t.Fatalf("delete after delivery: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("delete did not finish after delivery: %v", ctx.Err())
	}
	if err := deleteTx.Commit(ctx); err != nil {
		t.Fatalf("commit delivery-following delete: %v", err)
	}
	recreateTypedLedgerOwner(t, ctx, pool, owner)
	if _, err := store.RecordDelivery(
		ctx,
		owner,
		assignment.AssignmentID,
		delivery,
	); !errors.Is(err, ErrOwnerTombstoned) {
		t.Fatalf("RecordDelivery after delete error = %v, want ErrOwnerTombstoned", err)
	}
	assertNoTypedDelivery(t, ctx, pool, owner)
	assertNoTypedAggregateSequence(t, ctx, pool, owner, assignment.RequestID.String())
}

func recordTypedDeliveryAsync(
	ctx context.Context,
	store *Store,
	owner uuid.UUID,
	assignment Assignment,
	delivery Delivery,
) <-chan typedDeliveryRecordResult {
	done := make(chan typedDeliveryRecordResult, 1)
	go func() {
		sequence, err := store.RecordDelivery(
			ctx,
			owner,
			assignment.AssignmentID,
			delivery,
		)
		select {
		case done <- typedDeliveryRecordResult{sequence: sequence, err: err}:
		case <-ctx.Done():
		}
	}()
	return done
}

func assertTypedDeliveryTombstoned(
	t *testing.T,
	ctx context.Context,
	done <-chan typedDeliveryRecordResult,
) {
	t.Helper()
	select {
	case result := <-done:
		if !errors.Is(result.err, ErrOwnerTombstoned) {
			t.Fatalf(
				"RecordDelivery = (%d, %v), want ErrOwnerTombstoned",
				result.sequence,
				result.err,
			)
		}
	case <-ctx.Done():
		t.Fatalf("RecordDelivery did not finish after owner fence: %v", ctx.Err())
	}
}

func recreateTypedLedgerOwner(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner uuid.UUID,
) {
	t.Helper()
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1,$2,'user')`,
		owner,
		"typed-owner-recreated-"+owner.String(),
	); err != nil {
		t.Fatalf("recreate typed-ledger owner: %v", err)
	}
}

func assertNoTypedDelivery(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner uuid.UUID,
) {
	t.Helper()
	var deliveries int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM aura.adaptive_outbox
		 WHERE owner_id=$1 AND event_kind='delivery'`,
		owner,
	).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	if deliveries != 0 {
		t.Fatalf("delivery event count = %d, want 0", deliveries)
	}
}

func assertNoTypedAggregateSequence(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner uuid.UUID,
	aggregate string,
) {
	t.Helper()
	var sequenceRows int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM aura.adaptive_aggregate_sequences
		 WHERE owner_id=$1 AND aggregate_id=$2`,
		owner,
		aggregate,
	).Scan(&sequenceRows); err != nil {
		t.Fatal(err)
	}
	if sequenceRows != 0 {
		t.Fatalf("aggregate sequence row count = %d, want 0", sequenceRows)
	}
}
