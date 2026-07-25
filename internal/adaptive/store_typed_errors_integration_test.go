//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestStoreRecordDeliveryRejectsMissingForeignAndSchema1AssignmentsWithoutSequence(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := seedTypedLedgerOwner(t, pool)
	foreignOwner := seedTypedLedgerOwner(t, pool)
	store := NewStore(pool, StoreConfig{})

	if _, err := store.RecordDelivery(
		ctx,
		owner,
		uuid.Must(uuid.NewV7()),
		Delivery{},
	); !errors.Is(err, ErrAssignmentNotFound) {
		t.Fatalf("missing assignment error = %v, want ErrAssignmentNotFound", err)
	}
	var ownerSequences int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM aura.adaptive_aggregate_sequences WHERE owner_id=$1`,
		owner,
	).Scan(&ownerSequences); err != nil {
		t.Fatal(err)
	}
	if ownerSequences != 0 {
		t.Fatalf("missing assignment consumed %d sequence rows, want 0", ownerSequences)
	}

	assignment := typedLedgerAssignment(t, owner)
	if _, err := store.RecordAssignment(ctx, assignment); err != nil {
		t.Fatalf("RecordAssignment: %v", err)
	}
	if _, err := store.RecordDelivery(
		ctx,
		foreignOwner,
		assignment.AssignmentID,
		typedLedgerDelivery(t, assignment),
	); !errors.Is(err, ErrAssignmentNotFound) {
		t.Fatalf("foreign assignment error = %v, want ErrAssignmentNotFound", err)
	}

	schema1, err := NewEvent(EventParams{
		ID:          uuid.Must(uuid.NewV7()),
		OwnerID:     owner,
		AggregateID: "schema-1-history",
		DecisionID:  uuid.Must(uuid.NewV7()),
		Kind:        EventDecision,
		Payload:     []byte(`{"schema_version":"1.0","action":"static"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(ctx, schema1); err != nil {
		t.Fatalf("Record(schema1): %v", err)
	}
	if _, err := store.RecordDelivery(
		ctx,
		owner,
		schema1.DecisionID,
		Delivery{},
	); !errors.Is(err, ErrAssignmentNotFound) {
		t.Fatalf("schema-1 assignment error = %v, want ErrAssignmentNotFound", err)
	}
	schema1Rows, err := store.ListAggregate(ctx, owner, schema1.AggregateID)
	if err != nil {
		t.Fatalf("ListAggregate(schema1): %v", err)
	}
	if len(schema1Rows) != 1 || schema1Rows[0].ID != schema1.ID {
		t.Fatalf("schema-1 history rows = %+v, want readable original event", schema1Rows)
	}

	assertTypedAggregateSequence(t, ctx, pool, owner, assignment.RequestID.String(), 2)
	assertTypedAggregateSequence(t, ctx, pool, owner, schema1.AggregateID, 2)
	var foreignSequences int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM aura.adaptive_aggregate_sequences WHERE owner_id=$1`,
		foreignOwner,
	).Scan(&foreignSequences); err != nil {
		t.Fatal(err)
	}
	if foreignSequences != 0 {
		t.Fatalf("foreign owner sequence rows = %d, want 0", foreignSequences)
	}
}

func TestStoreRecordDeliveryRejectsUncommittedAssignment(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	event, err := NewAssignmentEvent(assignment)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(
		ctx,
		`SELECT set_config('app.current_identity', $1, true)`,
		owner.String(),
	); err != nil {
		t.Fatal(err)
	}
	if err := insertAdaptiveLedgerRow(
		ctx,
		tx,
		event.ID,
		event.OwnerID,
		event.AggregateID,
		1,
		event.DecisionID,
		event.Kind,
		event.Payload,
	); err != nil {
		t.Fatalf("insert uncommitted assignment: %v", err)
	}

	if _, err := NewStore(pool, StoreConfig{}).RecordDelivery(
		ctx,
		owner,
		assignment.AssignmentID,
		typedLedgerDelivery(t, assignment),
	); !errors.Is(err, ErrAssignmentNotFound) {
		t.Fatalf("uncommitted assignment error = %v, want ErrAssignmentNotFound", err)
	}
	var deliveryCount int
	if err := tx.QueryRow(
		ctx,
		`SELECT count(*) FROM aura.adaptive_outbox
		 WHERE owner_id=$1 AND event_kind='delivery'`,
		owner,
	).Scan(&deliveryCount); err != nil {
		t.Fatal(err)
	}
	if deliveryCount != 0 {
		t.Fatalf("uncommitted assignment bound %d deliveries, want 0", deliveryCount)
	}
}

func TestStoreRecordDeliveryRejectsStrictlyInvalidPersistedAssignment(t *testing.T) {
	ctx := context.Background()
	pool, _ := schema2MigrationDatabase(
		t,
		ctx,
		"aura_typed_store_invalid_assignment",
		60,
	)
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	if err := insertUnsafeSchema2Assignment(ctx, pool, assignment, 1); err != nil {
		t.Fatalf("insert pre-hardening assignment: %v", err)
	}

	if _, err := NewStore(pool, StoreConfig{}).RecordDelivery(
		ctx,
		owner,
		assignment.AssignmentID,
		typedLedgerDelivery(t, assignment),
	); !errors.Is(err, ErrPersistedAssignmentInvalid) {
		t.Fatalf(
			"invalid persisted assignment error = %v, want ErrPersistedAssignmentInvalid",
			err,
		)
	}
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
		t.Fatalf("invalid persisted assignment recorded %d deliveries, want 0", deliveries)
	}
	var sequenceRows int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM aura.adaptive_aggregate_sequences WHERE owner_id=$1`,
		owner,
	).Scan(&sequenceRows); err != nil {
		t.Fatal(err)
	}
	if sequenceRows != 0 {
		t.Fatalf("invalid persisted assignment consumed %d sequence rows, want 0", sequenceRows)
	}
}

func TestStoreTypedAssignmentPreservesOwnerTombstoneFence(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := seedTypedLedgerOwner(t, pool)
	assignment := typedLedgerAssignment(t, owner)
	delivery := typedLedgerDelivery(t, assignment)
	store := NewStore(pool, StoreConfig{})
	sequence, err := store.RecordAssignment(ctx, assignment)
	if err != nil {
		t.Fatalf("RecordAssignment: %v", err)
	}
	if sequence != 1 {
		t.Fatalf("RecordAssignment sequence = %d, want 1", sequence)
	}
	assertTypedAggregateSequence(t, ctx, pool, owner, assignment.RequestID.String(), 2)
	if _, err := pool.Exec(ctx, `DELETE FROM aura.identities WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1,$2,'user')`,
		owner,
		"typed-tombstone-recreated-"+owner.String(),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordDelivery(ctx, owner, assignment.AssignmentID, delivery); !errors.Is(err, ErrOwnerTombstoned) {
		t.Fatalf("RecordDelivery after owner recreation error = %v, want ErrOwnerTombstoned", err)
	}
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
	var sequenceRows int
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM aura.adaptive_aggregate_sequences
		 WHERE owner_id=$1 AND aggregate_id=$2`,
		owner,
		assignment.RequestID.String(),
	).Scan(&sequenceRows); err != nil {
		t.Fatal(err)
	}
	if sequenceRows != 0 {
		t.Fatalf("delivery after owner recreation allocated %d sequence rows, want 0", sequenceRows)
	}
	if _, err := store.RecordAssignment(ctx, assignment); !errors.Is(err, ErrOwnerTombstoned) {
		t.Fatalf("RecordAssignment after owner recreation error = %v, want ErrOwnerTombstoned", err)
	}
}

func assertTypedAggregateSequence(
	t *testing.T,
	ctx context.Context,
	querier interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	},
	owner uuid.UUID,
	aggregateID string,
	want int64,
) {
	t.Helper()
	var next int64
	if err := querier.QueryRow(
		ctx,
		`SELECT next_sequence
		 FROM aura.adaptive_aggregate_sequences
		 WHERE owner_id=$1 AND aggregate_id=$2`,
		owner,
		aggregateID,
	).Scan(&next); err != nil {
		t.Fatal(err)
	}
	if next != want {
		t.Fatalf("next sequence for %s = %d, want %d", aggregateID, next, want)
	}
}
