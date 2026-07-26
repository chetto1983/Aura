//go:build db_integration

package adaptive

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestStoreConcurrentRecordsAreIdempotentAndGapFree(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user')`,
		owner, "adaptive-concurrent-"+owner.String()); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, owner)
	})
	store := NewStore(pool, StoreConfig{})
	decision := uuid.Must(uuid.NewV7())
	duplicate, err := newLegacyEvent(eventParams{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "same-event",
		DecisionID: decision, Kind: EventDecision,
		Payload: []byte(`{"schema_version":"1.0","action":"shadow"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	const writers = 24
	var wg sync.WaitGroup
	seqs := make(chan int64, writers)
	errs := make(chan error, writers)
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			seq, err := store.recordLegacyEvent(ctx, duplicate)
			seqs <- seq
			errs <- err
		}()
	}
	wg.Wait()
	close(seqs)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent duplicate Record: %v", err)
		}
	}
	for seq := range seqs {
		if seq != 1 {
			t.Fatalf("duplicate sequence = %d, want 1", seq)
		}
	}
	rows, err := store.ListAggregate(ctx, owner, "same-event")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("duplicate rows = %d, want 1", len(rows))
	}

	const distinct = 40
	distinctSeqs := make(chan int64, distinct)
	distinctErrs := make(chan error, distinct)
	for i := range distinct {
		event, err := newLegacyEvent(eventParams{
			ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "distinct-events",
			DecisionID: decision, Kind: EventOutcome,
			Payload: []byte(fmt.Sprintf(`{"schema_version":"1.0","sample":%d}`, i)),
		})
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func(event Event) {
			defer wg.Done()
			seq, err := store.recordLegacyEvent(ctx, event)
			distinctSeqs <- seq
			distinctErrs <- err
		}(event)
	}
	wg.Wait()
	close(distinctSeqs)
	close(distinctErrs)
	for err := range distinctErrs {
		if err != nil {
			t.Fatalf("concurrent distinct Record: %v", err)
		}
	}
	got := make([]int, 0, distinct)
	for seq := range distinctSeqs {
		got = append(got, int(seq))
	}
	sort.Ints(got)
	for i, seq := range got {
		if want := i + 1; seq != want {
			t.Fatalf("sorted sequence[%d] = %d, want gap-free %d", i, seq, want)
		}
	}
}
