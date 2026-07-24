//go:build db_integration

package adaptive

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

var errLiveGraphUnavailable = errors.New("live graph unavailable")

type unavailableAdaptiveGraph struct{}

func (unavailableAdaptiveGraph) Project(context.Context, OutboxRecord) error {
	return errLiveGraphUnavailable
}

func (unavailableAdaptiveGraph) PurgeOwner(context.Context, uuid.UUID) error {
	return nil
}

type interleavingAdaptiveGraph struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	node    bool
	purges  int
}

func (g *interleavingAdaptiveGraph) Project(ctx context.Context, _ OutboxRecord) error {
	g.once.Do(func() { close(g.started) })
	select {
	case <-g.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	g.mu.Lock()
	g.node = true
	g.mu.Unlock()
	return nil
}

func (g *interleavingAdaptiveGraph) PurgeOwner(context.Context, uuid.UUID) error {
	g.mu.Lock()
	g.node = false
	g.purges++
	g.mu.Unlock()
	return nil
}

func (g *interleavingAdaptiveGraph) state() (bool, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.node, g.purges
}

func TestProjectorDeletionFenceClosesLiveTOCTOUInterleaving(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := uuid.Must(uuid.NewV7())
	name := "adaptive-projector-race-" + owner.String()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1,$2,'user')`,
		owner, name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, owner)
	})

	store := NewStore(pool, StoreConfig{})
	event, err := NewEvent(EventParams{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "delete-race",
		DecisionID: uuid.Must(uuid.NewV7()), Kind: EventDecision,
		Payload: []byte(`{"schema_version":"1.0","domain":"tool"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(ctx, event); err != nil {
		t.Fatalf("Record: %v", err)
	}
	graph := &interleavingAdaptiveGraph{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	projector := NewProjector(store, graph, ProjectorConfig{WorkerID: "delete-race"})
	done := make(chan error, 1)
	go func() {
		_, projectErr := projector.ProjectOne(ctx)
		done <- projectErr
	}()
	<-graph.started // projector passed the first tombstone check and entered graph write

	// Deprovision raises the permanent fence before its graph purge.
	var fenced bool
	if err := pool.QueryRow(ctx,
		`SELECT aura.fence_adaptive_identity($1)`, owner,
	).Scan(&fenced); err != nil {
		t.Fatalf("raise deletion fence: %v", err)
	}
	if !fenced {
		t.Fatal("raise deletion fence returned false")
	}
	if err := graph.PurgeOwner(ctx, owner); err != nil {
		t.Fatal(err)
	}
	close(graph.release) // the already-in-flight projector writes after the purge
	if err := <-done; err != nil {
		t.Fatalf("ProjectOne: %v", err)
	}

	node, purges := graph.state()
	if node || purges != 2 {
		t.Fatalf("post-interleaving graph state node=%t purges=%d, want false/2", node, purges)
	}
	var status string
	if err := pool.QueryRow(ctx,
		`SELECT status FROM aura.adaptive_outbox WHERE id=$1`, event.ID).Scan(&status); err != nil {
		t.Fatalf("read fenced event: %v", err)
	}
	if status == "projected" {
		t.Fatal("late projection was acknowledged after deletion fence")
	}
}

func TestProjectorGraphOutagePersistsRetryThenDeadLetterLive(t *testing.T) {
	pool := adaptiveIntegrationPool(t)
	ctx := context.Background()
	owner := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1,$2,'user')`,
		owner, "adaptive-projector-outage-"+owner.String()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, owner)
	})

	store := NewStore(pool, StoreConfig{MaxAttempts: 2})
	event, err := NewEvent(EventParams{
		ID: uuid.Must(uuid.NewV7()), OwnerID: owner, AggregateID: "graph-outage",
		DecisionID: uuid.Must(uuid.NewV7()), Kind: EventOutcome,
		Payload: []byte(`{"schema_version":"1.0","quality_observed":false}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(ctx, event); err != nil {
		t.Fatalf("Record: %v", err)
	}

	projector := NewProjector(store, unavailableAdaptiveGraph{}, ProjectorConfig{
		WorkerID: "graph-outage-worker", RetryBackoff: time.Nanosecond,
	})
	if processed, err := projector.ProjectOne(ctx); !processed || !errors.Is(err, errLiveGraphUnavailable) {
		t.Fatalf("first ProjectOne = processed %t err %v, want true/graph unavailable", processed, err)
	}
	var status string
	var attempts int
	var deadLetterAt *time.Time
	if err := pool.QueryRow(ctx,
		`SELECT status, attempts, dead_letter_at FROM aura.adaptive_outbox WHERE id=$1`,
		event.ID).Scan(&status, &attempts, &deadLetterAt); err != nil {
		t.Fatalf("read retry state: %v", err)
	}
	if status != "pending" || attempts != 1 || deadLetterAt != nil {
		t.Fatalf("first outage state = %s attempt %d dead_letter %v, want pending/1/nil",
			status, attempts, deadLetterAt)
	}

	if processed, err := projector.ProjectOne(ctx); !processed || !errors.Is(err, errLiveGraphUnavailable) {
		t.Fatalf("second ProjectOne = processed %t err %v, want true/graph unavailable", processed, err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT status, attempts, dead_letter_at FROM aura.adaptive_outbox WHERE id=$1`,
		event.ID).Scan(&status, &attempts, &deadLetterAt); err != nil {
		t.Fatalf("read dead-letter state: %v", err)
	}
	if status != "dead_letter" || attempts != 2 || deadLetterAt == nil {
		t.Fatalf("second outage state = %s attempt %d dead_letter %v, want dead_letter/2/set",
			status, attempts, deadLetterAt)
	}
}
