//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestStoreConcurrentExactDeliveriesShareOneRowAndSequence(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	owner := seedTypedLedgerOwner(t, pool)
	store := NewStore(pool, StoreConfig{})
	assignment := typedLedgerAssignment(t, owner)
	if _, err := store.RecordAssignment(ctx, assignment); err != nil {
		t.Fatalf("RecordAssignment: %v", err)
	}
	delivery := typedLedgerDelivery(t, assignment)

	const workers = 8
	deliveries := make([]Delivery, 0, workers)
	for range workers {
		deliveries = append(deliveries, delivery)
	}
	results := startConcurrentTypedDeliveries(
		t,
		ctx,
		store,
		owner,
		assignment.AssignmentID,
		deliveries,
	)
	for range workers {
		var result typedDeliveryRecordResult
		select {
		case result = <-results:
		case <-ctx.Done():
			t.Fatalf("concurrent exact deliveries did not finish: %v", ctx.Err())
		}
		if result.err != nil || result.sequence != 2 {
			t.Fatalf(
				"concurrent exact RecordDelivery = (%d, %v), want (2, nil)",
				result.sequence,
				result.err,
			)
		}
	}
	assertOneTypedDeliveryAndNextSequence(t, ctx, store, assignment, 3)
}

func TestStoreConcurrentConflictingDeliveriesHaveOneWinner(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	owner := seedTypedLedgerOwner(t, pool)
	store := NewStore(pool, StoreConfig{})
	assignment := typedLedgerAssignment(t, owner)
	if _, err := store.RecordAssignment(ctx, assignment); err != nil {
		t.Fatalf("RecordAssignment: %v", err)
	}
	first := typedLedgerDelivery(t, assignment)
	second := first
	second.EffectiveLimits = map[string]int{"top_k": 2}

	results := startConcurrentTypedDeliveries(
		t,
		ctx,
		store,
		owner,
		assignment.AssignmentID,
		[]Delivery{first, second},
	)

	var winners, conflicts int
	for range 2 {
		var result typedDeliveryRecordResult
		select {
		case result = <-results:
		case <-ctx.Done():
			t.Fatalf("concurrent conflicting deliveries did not finish: %v", ctx.Err())
		}
		switch {
		case result.err == nil && result.sequence == 2:
			winners++
		case errors.Is(result.err, ErrPayloadConflict):
			conflicts++
		default:
			t.Fatalf("concurrent conflicting RecordDelivery = (%d, %v)", result.sequence, result.err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("concurrent results = %d winners/%d conflicts, want 1/1", winners, conflicts)
	}
	assertOneTypedDeliveryAndNextSequence(t, ctx, store, assignment, 3)
}

type typedDeliveryRecordResult struct {
	sequence int64
	err      error
}

func startConcurrentTypedDeliveries(
	t *testing.T,
	ctx context.Context,
	store *Store,
	owner uuid.UUID,
	assignmentID uuid.UUID,
	deliveries []Delivery,
) <-chan typedDeliveryRecordResult {
	t.Helper()
	start := make(chan struct{})
	ready := make(chan struct{}, len(deliveries))
	results := make(chan typedDeliveryRecordResult, len(deliveries))
	for _, delivery := range deliveries {
		delivery := delivery
		go func() {
			select {
			case ready <- struct{}{}:
			case <-ctx.Done():
				return
			}
			select {
			case <-start:
			case <-ctx.Done():
				return
			}
			sequence, err := store.RecordDelivery(
				ctx,
				owner,
				assignmentID,
				delivery,
			)
			select {
			case results <- typedDeliveryRecordResult{sequence: sequence, err: err}:
			case <-ctx.Done():
			}
		}()
	}
	for range deliveries {
		select {
		case <-ready:
		case <-ctx.Done():
			t.Fatalf("concurrent deliveries did not become ready: %v", ctx.Err())
		}
	}
	close(start)
	return results
}

func assertOneTypedDeliveryAndNextSequence(
	t *testing.T,
	ctx context.Context,
	store *Store,
	assignment Assignment,
	wantNext int64,
) {
	t.Helper()
	owner := assignment.OwnerID
	rows, err := store.ListAggregate(ctx, owner, assignment.RequestID.String())
	if err != nil {
		t.Fatalf("ListAggregate: %v", err)
	}
	if len(rows) != 2 ||
		rows[0].Kind != EventDecision ||
		rows[1].Kind != EventDelivery {
		t.Fatalf("typed concurrent aggregate rows = %+v, want one assignment and one delivery", rows)
	}
	assertTypedAggregateSequence(
		t,
		ctx,
		store.pool,
		owner,
		assignment.RequestID.String(),
		wantNext,
	)
}
