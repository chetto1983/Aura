//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"testing"
)

func TestStoreTypedPersistenceIsExactlyIdempotentAndConflictSafe(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := seedTypedLedgerOwner(t, pool)
	store := NewStore(pool, StoreConfig{})
	assignment := typedLedgerAssignment(t, owner)

	assignmentSequence, err := store.RecordAssignment(ctx, assignment)
	if err != nil || assignmentSequence != 1 {
		t.Fatalf("RecordAssignment = (%d, %v), want (1, nil)", assignmentSequence, err)
	}
	retrySequence, err := store.RecordAssignment(ctx, assignment)
	if err != nil || retrySequence != assignmentSequence {
		t.Fatalf(
			"RecordAssignment exact retry = (%d, %v), want (%d, nil)",
			retrySequence,
			err,
			assignmentSequence,
		)
	}
	conflictingAssignment := assignment
	conflictingAssignment.PolicyVersion = "typed-ledger-v2"
	if _, err := store.RecordAssignment(ctx, conflictingAssignment); !errors.Is(err, ErrPayloadConflict) {
		t.Fatalf("RecordAssignment conflict error = %v, want ErrPayloadConflict", err)
	}

	delivery := typedLedgerDelivery(t, assignment)
	deliverySequence, err := store.RecordDelivery(
		ctx,
		owner,
		assignment.AssignmentID,
		delivery,
	)
	if err != nil || deliverySequence != 2 {
		t.Fatalf("RecordDelivery = (%d, %v), want (2, nil)", deliverySequence, err)
	}
	retrySequence, err = store.RecordDelivery(
		ctx,
		owner,
		assignment.AssignmentID,
		delivery,
	)
	if err != nil || retrySequence != deliverySequence {
		t.Fatalf(
			"RecordDelivery exact retry = (%d, %v), want (%d, nil)",
			retrySequence,
			err,
			deliverySequence,
		)
	}
	conflictingDelivery := delivery
	conflictingDelivery.EffectiveLimits = map[string]int{"top_k": 2}
	if _, err := store.RecordDelivery(
		ctx,
		owner,
		assignment.AssignmentID,
		conflictingDelivery,
	); !errors.Is(err, ErrPayloadConflict) {
		t.Fatalf("RecordDelivery conflict error = %v, want ErrPayloadConflict", err)
	}

	rows, err := store.ListAggregate(ctx, owner, assignment.RequestID.String())
	if err != nil {
		t.Fatalf("ListAggregate: %v", err)
	}
	if len(rows) != 2 ||
		rows[0].Kind != EventDecision ||
		rows[0].Sequence != 1 ||
		rows[1].Kind != EventDelivery ||
		rows[1].Sequence != 2 {
		t.Fatalf("typed aggregate rows = %+v, want assignment/delivery sequences 1/2", rows)
	}

	var nextSequence int64
	if err := pool.QueryRow(
		ctx,
		`SELECT next_sequence
		 FROM aura.adaptive_aggregate_sequences
		 WHERE owner_id=$1 AND aggregate_id=$2`,
		owner,
		assignment.RequestID.String(),
	).Scan(&nextSequence); err != nil {
		t.Fatal(err)
	}
	if nextSequence != 3 {
		t.Fatalf("next aggregate sequence = %d, want 3 without retry/conflict gaps", nextSequence)
	}
}
