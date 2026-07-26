//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestStoreDeletionFenceClosesRecordWaitAndUUIDRecreationRace(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	t.Run("writer commits before waiting delete", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		owner := seedGenericRaceOwner(t, ctx, pool)
		event := genericRaceEvent(t, owner, "writer-first")
		store := NewStore(pool, StoreConfig{})
		migratePool := typedLedgerMigrationPool(t, ctx)

		eventBlocker, err := migratePool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin event blocker: %v", err)
		}
		defer rollbackGenericRaceTx(t, eventBlocker)
		var blockerPID int32
		if err := eventBlocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
			t.Fatalf("read event blocker PID: %v", err)
		}
		if _, err := eventBlocker.Exec(
			ctx,
			`SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`,
			event.ID.String(),
		); err != nil {
			t.Fatalf("hold event advisory lock: %v", err)
		}

		recordDone := recordGenericEventAsync(ctx, store, event)
		waitForAdaptiveOwnerLock(t, ctx, migratePool, blockerPID, recordDone)
		deleteTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin waiting delete: %v", err)
		}
		defer rollbackGenericRaceTx(t, deleteTx)
		var deletePID int32
		if err := deleteTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&deletePID); err != nil {
			t.Fatalf("read delete PID: %v", err)
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

		if err := eventBlocker.Commit(ctx); err != nil {
			t.Fatalf("release event advisory lock: %v", err)
		}
		select {
		case result := <-recordDone:
			if result.err != nil || result.sequence != 1 {
				t.Fatalf("writer-first Record = (%d, %v), want (1, nil)", result.sequence, result.err)
			}
		case <-ctx.Done():
			t.Fatalf("writer-first Record did not finish: %v", ctx.Err())
		}
		select {
		case err := <-deleteDone:
			if err != nil {
				t.Fatalf("delete after writer: %v", err)
			}
		case <-ctx.Done():
			t.Fatalf("delete did not finish after writer: %v", ctx.Err())
		}
		if err := deleteTx.Commit(ctx); err != nil {
			t.Fatalf("commit delete after writer: %v", err)
		}
		assertGenericRaceFencedAndEmpty(t, ctx, pool, owner, event)
	})

	t.Run("delete fence rejects waiting writer after recreation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		owner := seedGenericRaceOwner(t, ctx, pool)
		event := genericRaceEvent(t, owner, "delete-first")
		store := NewStore(pool, StoreConfig{})
		migratePool := typedLedgerMigrationPool(t, ctx)

		deleteTx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin delete fence: %v", err)
		}
		defer rollbackGenericRaceTx(t, deleteTx)
		var deletePID int32
		if err := deleteTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&deletePID); err != nil {
			t.Fatalf("read delete PID: %v", err)
		}
		if _, err := deleteTx.Exec(
			ctx,
			`DELETE FROM aura.identities WHERE id=$1`,
			owner,
		); err != nil {
			t.Fatalf("delete owner: %v", err)
		}

		recordDone := recordGenericEventAsync(ctx, store, event)
		waitForAdaptiveOwnerLock(t, ctx, migratePool, deletePID, recordDone)
		if err := deleteTx.Commit(ctx); err != nil {
			t.Fatalf("commit delete fence: %v", err)
		}
		select {
		case result := <-recordDone:
			if !errors.Is(result.err, ErrOwnerTombstoned) {
				t.Fatalf(
					"delete-first Record = (%d, %v), want ErrOwnerTombstoned",
					result.sequence,
					result.err,
				)
			}
		case <-ctx.Done():
			t.Fatalf("delete-first Record did not finish: %v", ctx.Err())
		}
		assertGenericRaceFencedAndEmpty(t, ctx, pool, owner, event)

		if _, err := pool.Exec(
			ctx,
			`INSERT INTO aura.identities (id, name, kind) VALUES ($1,$2,'user')`,
			owner,
			"generic-race-recreated-"+owner.String(),
		); err != nil {
			t.Fatalf("recreate owner: %v", err)
		}
		if _, err := store.recordLegacyEvent(ctx, event); !errors.Is(err, ErrOwnerTombstoned) {
			t.Fatalf("Record after owner recreation error = %v, want ErrOwnerTombstoned", err)
		}
		assertGenericRaceFencedAndEmpty(t, ctx, pool, owner, event)
	})
}

func seedGenericRaceOwner(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) uuid.UUID {
	t.Helper()
	owner := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1,$2,'user')`,
		owner,
		"generic-race-"+owner.String(),
	); err != nil {
		t.Fatalf("seed generic race owner: %v", err)
	}
	registerAdaptiveOwnerCleanup(t, owner)
	return owner
}

func registerAdaptiveOwnerCleanup(t *testing.T, owner uuid.UUID) {
	t.Helper()
	setupCtx, setupCancel := context.WithTimeout(t.Context(), 5*time.Second)
	cleanupPool := typedLedgerMigrationPool(t, setupCtx)
	setupCancel()
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.WithoutCancel(t.Context()),
			5*time.Second,
		)
		defer cleanupCancel()
		if _, err := cleanupPool.Exec(
			cleanupCtx,
			`DELETE FROM aura.identities WHERE id=$1`,
			owner,
		); err != nil {
			t.Errorf("delete adaptive test owner: %v", err)
		}
		if _, err := cleanupPool.Exec(
			cleanupCtx,
			`DELETE FROM aura.adaptive_identity_tombstones WHERE owner_id=$1`,
			owner,
		); err != nil {
			t.Errorf("delete adaptive test owner tombstone: %v", err)
		}
	})
}

func genericRaceEvent(t *testing.T, owner uuid.UUID, aggregate string) Event {
	t.Helper()
	event, err := newLegacyEvent(eventParams{
		ID:          uuid.Must(uuid.NewV7()),
		OwnerID:     owner,
		AggregateID: "generic-race-" + aggregate,
		DecisionID:  uuid.Must(uuid.NewV7()),
		Kind:        EventDecision,
		Payload:     []byte(`{"schema_version":"1.0","action":"shadow"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return event
}

func recordGenericEventAsync(
	ctx context.Context,
	store *Store,
	event Event,
) <-chan typedDeliveryRecordResult {
	done := make(chan typedDeliveryRecordResult, 1)
	go func() {
		sequence, err := store.recordLegacyEvent(ctx, event)
		select {
		case done <- typedDeliveryRecordResult{sequence: sequence, err: err}:
		case <-ctx.Done():
		}
	}()
	return done
}

func assertGenericRaceFencedAndEmpty(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	owner uuid.UUID,
	event Event,
) {
	t.Helper()
	var events, sequences int
	var fenced bool
	if err := pool.QueryRow(
		ctx,
		`SELECT
		    (SELECT count(*) FROM aura.adaptive_outbox WHERE id=$1),
		    (SELECT count(*) FROM aura.adaptive_aggregate_sequences
		     WHERE owner_id=$2 AND aggregate_id=$3),
		    EXISTS (
		        SELECT 1 FROM aura.adaptive_identity_tombstones
		        WHERE owner_id=$2
		    )`,
		event.ID,
		owner,
		event.AggregateID,
	).Scan(&events, &sequences, &fenced); err != nil {
		t.Fatalf("read generic race state: %v", err)
	}
	if events != 0 || sequences != 0 || !fenced {
		t.Fatalf(
			"generic race state = events:%d sequences:%d fenced:%t, want 0/0/true",
			events,
			sequences,
			fenced,
		)
	}
}

func rollbackGenericRaceTx(t *testing.T, tx pgx.Tx) {
	t.Helper()
	cleanupCtx, cleanupCancel := context.WithTimeout(
		context.WithoutCancel(t.Context()),
		5*time.Second,
	)
	defer cleanupCancel()
	if err := tx.Rollback(cleanupCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Errorf("rollback generic race transaction: %v", err)
	}
}
