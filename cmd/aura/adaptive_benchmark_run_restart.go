package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type adaptiveBenchmarkMemoryControlTransform func(
	runner.DynamicRecallControl,
) runner.DynamicRecallControl

type adaptiveBenchmarkRuntimeState struct {
	env              *chatEnv
	recorder         *adaptiveBenchmarkTeeRecorder
	store            *adaptive.Store
	observedClient   *adaptiveBenchmarkObservedClient
	adaptiveControls adaptiveControlSet
}

type adaptiveBenchmarkRuntimeReassembler func(
	context.Context,
) (adaptiveBenchmarkRuntimeState, error)

type adaptiveBenchmarkRuntimeForker func(
	context.Context,
	adaptiveBenchmarkMemoryControlTransform,
) (adaptiveBenchmarkRuntimeState, error)

type adaptiveBenchmarkRuntimeStateAssembler func(
	context.Context,
	*config.Config,
	*pgxpool.Pool,
	adaptiveBenchmarkRunRequest,
	uuid.UUID,
	auraeval.AdaptiveBenchmarkModel,
	*adaptiveBenchmarkDecisionBundle,
	adaptiveBenchmarkMemoryControlTransform,
) (adaptiveBenchmarkRuntimeState, error)

type adaptiveBenchmarkRestartOps struct {
	openPool       func(context.Context, *config.Config) (*pgxpool.Pool, error)
	checkMigration migrationHeadChecker
	assemble       adaptiveBenchmarkRuntimeStateAssembler
}

func (environment *adaptiveBenchmarkChatEnvironment) Restart(
	ctx context.Context,
) error {
	if environment == nil {
		return errors.New("adaptive benchmark runtime is unavailable")
	}
	environment.runtimeMu.Lock()
	defer environment.runtimeMu.Unlock()
	if environment.env == nil || environment.reassemble == nil {
		return errors.New("adaptive benchmark runtime cannot restart")
	}
	environment.env.close()
	environment.env = nil
	environment.recorder = nil
	environment.store = nil
	environment.observedClient = nil
	environment.adaptiveControls = adaptiveControlSet{}

	state, err := environment.reassemble(ctx)
	if err != nil {
		return fmt.Errorf("reassemble adaptive benchmark runtime: %w", err)
	}
	if err := validateAdaptiveBenchmarkRuntimeState(state); err != nil {
		if state.env != nil {
			state.env.close()
		}
		return err
	}
	environment.env = state.env
	environment.recorder = state.recorder
	environment.store = state.store
	environment.observedClient = state.observedClient
	environment.adaptiveControls = state.adaptiveControls
	return nil
}

func (environment *adaptiveBenchmarkChatEnvironment) ForkControl(
	ctx context.Context,
	transform adaptiveBenchmarkMemoryControlTransform,
) (*adaptiveBenchmarkChatEnvironment, error) {
	if environment == nil || transform == nil {
		return nil, errors.New(
			"adaptive benchmark control fork is unavailable",
		)
	}
	environment.runtimeMu.Lock()
	defer environment.runtimeMu.Unlock()
	if environment.env == nil ||
		environment.bundle == nil ||
		environment.forkControl == nil {
		return nil, errors.New(
			"adaptive benchmark control fork is unavailable",
		)
	}
	state, err := environment.forkControl(ctx, transform)
	if err != nil {
		return nil, fmt.Errorf(
			"assemble adaptive benchmark control fork: %w",
			err,
		)
	}
	if err := validateAdaptiveBenchmarkRuntimeState(state); err != nil {
		if state.env != nil {
			state.env.close()
		}
		return nil, err
	}
	if state.adaptiveControls.memoryRecall == nil {
		state.env.close()
		return nil, errors.New(
			"adaptive benchmark control fork has no memory control",
		)
	}
	return &adaptiveBenchmarkChatEnvironment{
		env:               state.env,
		bundle:            environment.bundle,
		recorder:          state.recorder,
		store:             state.store,
		observedClient:    state.observedClient,
		adaptiveControls:  state.adaptiveControls,
		outcomesForbidden: true,
	}, nil
}

func newAdaptiveBenchmarkRuntimeReassembler(
	cfg *config.Config,
	request adaptiveBenchmarkRunRequest,
	runID uuid.UUID,
	model auraeval.AdaptiveBenchmarkModel,
	bundle *adaptiveBenchmarkDecisionBundle,
	ops adaptiveBenchmarkRestartOps,
	memoryControlTransform adaptiveBenchmarkMemoryControlTransform,
) adaptiveBenchmarkRuntimeReassembler {
	return func(
		ctx context.Context,
	) (_ adaptiveBenchmarkRuntimeState, returnedErr error) {
		if cfg == nil ||
			request.OwnerID == uuid.Nil ||
			runID == uuid.Nil ||
			bundle == nil ||
			ops.openPool == nil ||
			ops.checkMigration == nil ||
			ops.assemble == nil {
			return adaptiveBenchmarkRuntimeState{}, errors.New(
				"adaptive benchmark restart is unavailable",
			)
		}
		pool, err := ops.openPool(ctx, cfg)
		if err != nil {
			return adaptiveBenchmarkRuntimeState{}, fmt.Errorf(
				"open adaptive benchmark restart pool: %w",
				err,
			)
		}
		if pool == nil {
			return adaptiveBenchmarkRuntimeState{}, errors.New(
				"adaptive benchmark restart pool is unavailable",
			)
		}
		success := false
		defer func() {
			if !success {
				pool.Close()
			}
		}()
		if err := ops.checkMigration(ctx, cfg.DB.MigrateURL); err != nil {
			return adaptiveBenchmarkRuntimeState{}, fmt.Errorf(
				"postgres migration compatibility: %w",
				err,
			)
		}
		state, err := ops.assemble(
			ctx,
			cfg,
			pool,
			request,
			runID,
			model,
			bundle,
			memoryControlTransform,
		)
		if err != nil {
			return adaptiveBenchmarkRuntimeState{}, err
		}
		if err := validateAdaptiveBenchmarkRuntimeState(state); err != nil {
			return adaptiveBenchmarkRuntimeState{}, err
		}
		success = true
		return state, nil
	}
}

func newAdaptiveBenchmarkRuntimeForker(
	cfg *config.Config,
	request adaptiveBenchmarkRunRequest,
	runID uuid.UUID,
	model auraeval.AdaptiveBenchmarkModel,
	bundle *adaptiveBenchmarkDecisionBundle,
	ops adaptiveBenchmarkRestartOps,
) adaptiveBenchmarkRuntimeForker {
	return func(
		ctx context.Context,
		transform adaptiveBenchmarkMemoryControlTransform,
	) (adaptiveBenchmarkRuntimeState, error) {
		if transform == nil {
			return adaptiveBenchmarkRuntimeState{}, errors.New(
				"adaptive benchmark memory control transform is unavailable",
			)
		}
		return newAdaptiveBenchmarkRuntimeReassembler(
			cfg,
			request,
			runID,
			model,
			bundle,
			ops,
			transform,
		)(ctx)
	}
}

func productionAdaptiveBenchmarkRestartOps() adaptiveBenchmarkRestartOps {
	return adaptiveBenchmarkRestartOps{
		openPool: func(
			ctx context.Context,
			cfg *config.Config,
		) (*pgxpool.Pool, error) {
			return db.Open(ctx, &cfg.DB)
		},
		checkMigration: db.CheckMigrationHead,
		assemble:       assembleAdaptiveBenchmarkRuntimeState,
	}
}

func validateAdaptiveBenchmarkRuntimeState(
	state adaptiveBenchmarkRuntimeState,
) error {
	if state.env == nil ||
		state.recorder == nil ||
		state.store == nil ||
		state.observedClient == nil {
		return errors.New(
			"adaptive benchmark reassembled runtime is incomplete",
		)
	}
	return nil
}
