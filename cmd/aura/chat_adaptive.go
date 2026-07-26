package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/knowledge"
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

type adaptiveGraphClientOpener func(
	context.Context,
	*knowledge.Config,
) (adaptiveGraphClient, error)

type adaptiveProjectorRuntime struct {
	worker adaptiveProjectorLifecycle
	client adaptiveGraphClient
}

func startAdaptiveProjectorRuntime(
	ctx context.Context,
	worker adaptiveProjectorLifecycle,
	client adaptiveGraphClient,
) (*adaptiveProjectorRuntime, error) {
	if worker == nil || client == nil {
		return nil, errors.New("adaptive projector runtime is not configured")
	}
	if err := worker.Start(ctx); err != nil {
		return nil, errors.Join(err, client.Close())
	}
	return &adaptiveProjectorRuntime{worker: worker, client: client}, nil
}

func (r *adaptiveProjectorRuntime) Stop(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if err := r.worker.Stop(ctx); err != nil {
		return err
	}
	return r.client.Close()
}

func buildAdaptiveProjectorRuntime(
	ctx context.Context,
	cfg *knowledge.Config,
	pool *pgxpool.Pool,
	open adaptiveGraphClientOpener,
) (*adaptiveProjectorRuntime, error) {
	if cfg == nil || pool == nil {
		return nil, errors.New("adaptive projector dependencies are incomplete")
	}
	if open == nil {
		open = openAdaptiveGraphClient
	}
	client, err := open(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open adaptive projector graph client: %w", err)
	}
	projector := adaptive.NewProjector(
		adaptive.NewStore(pool, adaptive.StoreConfig{}),
		adaptive.NewGraphStore(client),
		adaptive.ProjectorConfig{WorkerID: newAdaptiveProjectorWorkerID()},
	)
	worker := adaptive.NewProjectorWorker(
		projector,
		adaptive.ProjectorWorkerConfig{},
	)
	runtime, err := startAdaptiveProjectorRuntime(ctx, worker, client)
	if err != nil {
		return nil, fmt.Errorf("start adaptive projector: %w", err)
	}
	return runtime, nil
}
