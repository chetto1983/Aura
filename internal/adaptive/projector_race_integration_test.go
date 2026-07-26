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
	purged  chan int
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
	purges := g.purges
	g.mu.Unlock()
	g.purged <- purges
	return nil
}

func (g *interleavingAdaptiveGraph) state() (bool, int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.node, g.purges
}

func TestProjectorWorkerDeletionFenceClosesLiveTOCTOUInterleaving(t *testing.T) {
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
		purged:  make(chan int, 2),
	}
	projector := NewProjector(store, graph, ProjectorConfig{WorkerID: "delete-race"})
	worker := NewProjectorWorker(projector, ProjectorWorkerConfig{PollInterval: time.Hour})
	if err := worker.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopProjectorWorker(t, worker)
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
	if purges := <-graph.purged; purges != 1 {
		t.Fatalf("deprovision purge count = %d, want 1", purges)
	}
	close(graph.release) // the already-in-flight projector writes after the purge
	select {
	case purges := <-graph.purged:
		if purges != 2 {
			t.Fatalf("worker late-projection purge count = %d, want 2", purges)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not purge the late projection")
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := worker.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
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

func TestProjectorWorkerGraphOutagePersistsRetryThenDeadLetterLive(t *testing.T) {
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
	worker := NewProjectorWorker(projector, ProjectorWorkerConfig{
		PollInterval:    time.Hour,
		ErrorBackoff:    time.Millisecond,
		MaxErrorBackoff: time.Millisecond,
	})
	if err := worker.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	stopProjectorWorker(t, worker)

	var status string
	var attempts int
	var deadLetterAt *time.Time
	deadline := time.After(5 * time.Second)
	for status != "dead_letter" {
		if err := pool.QueryRow(ctx,
			`SELECT status, attempts, dead_letter_at FROM aura.adaptive_outbox WHERE id=$1`,
			event.ID).Scan(&status, &attempts, &deadLetterAt); err != nil {
			t.Fatalf("read projector worker state: %v", err)
		}
		if status == "dead_letter" {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("worker state = %s attempt %d, want dead_letter/2", status, attempts)
		case <-time.After(time.Millisecond):
		}
	}
	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := worker.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if status != "dead_letter" || attempts != 2 || deadLetterAt == nil {
		t.Fatalf("second outage state = %s attempt %d dead_letter %v, want dead_letter/2/set",
			status, attempts, deadLetterAt)
	}
}
