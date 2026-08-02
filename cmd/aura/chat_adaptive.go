package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const adaptiveProjectorWorkerIDPrefix = "aura-private-graph-projector-"

func newAdaptiveProjectorWorkerID() string {
	return adaptiveProjectorWorkerIDPrefix + uuid.NewString()
}

type adaptiveProjectorLifecycle interface {
	Start(context.Context) error
	Stop(context.Context) error
}

// adaptiveProjectorRuntime owns the projector worker's lifecycle, and nothing
// else. It used to also own the graph client, because the Bolt-over-MCP client
// held a subprocess that had to be closed in order after the worker stopped. The
// ArcadeDB writer that replaced it is a stateless HTTP client, so there is no
// second resource and no ordering left to get wrong.
type adaptiveProjectorRuntime struct {
	worker adaptiveProjectorLifecycle
}

func startAdaptiveProjectorRuntime(
	ctx context.Context,
	worker adaptiveProjectorLifecycle,
) (*adaptiveProjectorRuntime, error) {
	if worker == nil {
		return nil, errors.New("adaptive projector runtime is not configured")
	}
	if err := worker.Start(ctx); err != nil {
		return nil, err
	}
	return &adaptiveProjectorRuntime{worker: worker}, nil
}

func (r *adaptiveProjectorRuntime) Stop(ctx context.Context) error {
	if r == nil {
		return nil
	}
	return r.worker.Stop(ctx)
}

func buildAdaptiveProjectorRuntime(
	ctx context.Context,
	cfg *config.ArcadeDBConfig,
	pool *pgxpool.Pool,
	open adaptiveGraphClientOpener,
) (*adaptiveProjectorRuntime, error) {
	if cfg == nil || pool == nil {
		return nil, errors.New("adaptive projector dependencies are incomplete")
	}
	if open == nil {
		open = openAdaptiveGraphClient
	}
	writer, err := open(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open adaptive projector graph client: %w", err)
	}
	projector := adaptive.NewProjector(
		adaptive.NewStore(pool, adaptive.StoreConfig{}),
		adaptive.NewGraphStore(writer),
		adaptive.ProjectorConfig{WorkerID: newAdaptiveProjectorWorkerID()},
	)
	worker := adaptive.NewProjectorWorker(
		projector,
		adaptive.ProjectorWorkerConfig{},
	)
	runtime, err := startAdaptiveProjectorRuntime(ctx, worker)
	if err != nil {
		return nil, fmt.Errorf("start adaptive projector: %w", err)
	}
	return runtime, nil
}
