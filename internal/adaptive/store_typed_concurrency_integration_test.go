//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestStoreConcurrentExactDeliveriesShareOneRowAndSequence(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := seedTypedLedgerOwner(t, pool)
	store := NewStore(pool, StoreConfig{})
	assignment := typedLedgerAssignment(t, owner)
	if _, err := store.RecordAssignment(ctx, assignment); err != nil {
		t.Fatalf("RecordAssignment: %v", err)
	}
	delivery := typedLedgerDelivery(t, assignment)

	const workers = 8
	start := make(chan struct{})
	results := make(chan typedDeliveryRecordResult, workers)
	var ready sync.WaitGroup
	ready.Add(workers)
	for range workers {
		go func() {
			ready.Done()
			<-start
			sequence, err := store.RecordDelivery(
				ctx,
				owner,
				assignment.AssignmentID,
				delivery,
			)
			results <- typedDeliveryRecordResult{sequence: sequence, err: err}
		}()
	}
	ready.Wait()
	close(start)
	for range workers {
		result := <-results
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
	ctx := context.Background()
	owner := seedTypedLedgerOwner(t, pool)
	store := NewStore(pool, StoreConfig{})
	assignment := typedLedgerAssignment(t, owner)
	if _, err := store.RecordAssignment(ctx, assignment); err != nil {
		t.Fatalf("RecordAssignment: %v", err)
	}
	first := typedLedgerDelivery(t, assignment)
	second := first
	second.EffectiveLimits = map[string]int{"top_k": 2}

	start := make(chan struct{})
	results := make(chan typedDeliveryRecordResult, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, delivery := range []Delivery{first, second} {
		delivery := delivery
		go func() {
			ready.Done()
			<-start
			sequence, err := store.RecordDelivery(
				ctx,
				owner,
				assignment.AssignmentID,
				delivery,
			)
			results <- typedDeliveryRecordResult{sequence: sequence, err: err}
		}()
	}
	ready.Wait()
	close(start)

	var winners, conflicts int
	for range 2 {
		result := <-results
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
