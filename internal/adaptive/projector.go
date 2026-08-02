package adaptive

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ProjectionQueue leases ordered outbox facts and enforces permanent owner fences.
type ProjectionQueue interface {
	Claim(context.Context, ClaimOptions) (*OutboxRecord, error)
	MarkProjected(context.Context, uuid.UUID, string, time.Time) error
	Retry(context.Context, uuid.UUID, string, string, time.Time, time.Time) error
	IsOwnerTombstoned(context.Context, uuid.UUID) (bool, error)
}

// AdaptiveGraph is the projection boundary for the derived graph view.
type AdaptiveGraph interface {
	Project(context.Context, OutboxRecord) error
	PurgeOwner(context.Context, uuid.UUID) error
}

// ProjectorConfig bounds leases and retry delays for one projector worker.
type ProjectorConfig struct {
	WorkerID     string
	Lease        time.Duration
	RetryBackoff time.Duration
}

// Projector copies authoritative PostgreSQL facts into the derived graph view.
type Projector struct {
	queue        ProjectionQueue
	graph        AdaptiveGraph
	workerID     string
	lease        time.Duration
	retryBackoff time.Duration
}

// NewProjector applies bounded defaults to a projector worker.
func NewProjector(queue ProjectionQueue, graph AdaptiveGraph, cfg ProjectorConfig) *Projector {
	lease := cfg.Lease
	if lease <= 0 {
		lease = time.Minute
	}
	retryBackoff := cfg.RetryBackoff
	if retryBackoff <= 0 {
		retryBackoff = 5 * time.Second
	}
	return &Projector{
		queue: queue, graph: graph, workerID: strings.TrimSpace(cfg.WorkerID),
		lease: lease, retryBackoff: retryBackoff,
	}
}

// ProjectOne processes at most one outbox event. The worker is safe to run in
// multiple processes because Claim uses SKIP LOCKED and per-aggregate ordering.
func (p *Projector) ProjectOne(ctx context.Context) (bool, error) {
	if p == nil || p.queue == nil || p.graph == nil || p.workerID == "" {
		return false, errors.New("adaptive projector is not fully configured")
	}
	now := time.Now().UTC()
	record, err := p.queue.Claim(ctx, ClaimOptions{
		WorkerID: p.workerID,
		Now:      now,
		Lease:    p.lease,
	})
	if err != nil {
		return false, err
	}
	if record == nil {
		return false, nil
	}
	tombstoned, err := p.queue.IsOwnerTombstoned(ctx, record.OwnerID)
	if err != nil {
		return true, err
	}
	if tombstoned {
		if err := p.graph.PurgeOwner(ctx, record.OwnerID); err != nil {
			return true, fmt.Errorf("purge tombstoned adaptive owner: %w", err)
		}
		// The identity delete cascades the leased Postgres row. Do not recreate
		// or acknowledge it; a missing row is the successful terminal state.
		return true, nil
	}
	if err := p.graph.Project(ctx, *record); err != nil {
		retryErr := p.queue.Retry(
			ctx, record.ID, p.workerID, "graph_projection",
			now, now.Add(p.retryBackoff),
		)
		if retryErr != nil {
			return true, errors.Join(err, retryErr)
		}
		return true, err
	}
	// Close the deletion TOCTOU window. A purge can fence the owner after the
	// first check but before this graph write. Re-read the permanent fence after
	// the write; if it appeared, remove the late projection and never ack it.
	tombstoned, err = p.queue.IsOwnerTombstoned(ctx, record.OwnerID)
	if err != nil {
		return true, fmt.Errorf("recheck adaptive owner tombstone after projection: %w", err)
	}
	if tombstoned {
		if err := p.graph.PurgeOwner(ctx, record.OwnerID); err != nil {
			return true, fmt.Errorf("purge late projection for tombstoned adaptive owner: %w", err)
		}
		return true, nil
	}
	if err := p.queue.MarkProjected(ctx, record.ID, p.workerID, now); err != nil {
		return true, err
	}
	return true, nil
}
