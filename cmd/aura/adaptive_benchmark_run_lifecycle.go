package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/config"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/settings"
	"github.com/google/uuid"
)

const (
	adaptiveBenchmarkOverrideLease     = 2 * time.Minute
	adaptiveBenchmarkHeartbeatInterval = 30 * time.Second
)

type settingsBenchmarkOverrideRequest = settings.BenchmarkOverrideRequest

type adaptiveBenchmarkPool interface {
	Close()
}

type adaptiveBenchmarkOverride interface {
	Heartbeat(context.Context) error
	Restore(context.Context) error
}

type adaptiveBenchmarkEnvironment interface {
	Run(
		context.Context,
		adaptiveBenchmarkRunRequest,
		uuid.UUID,
		auraeval.AdaptiveBenchmarkModel,
	) (*adaptiveBenchmarkReportDraft, error)
	Close() error
	Finalize(
		context.Context,
		*adaptiveBenchmarkReportDraft,
	) ([]byte, error)
}

type adaptiveBenchmarkLifecycleOps struct {
	newRunID func() (uuid.UUID, error)

	preflightQwen   func(context.Context) (auraeval.AdaptiveBenchmarkModel, error)
	openControlPool func(context.Context) (adaptiveBenchmarkPool, error)
	authorizeQwen   func(
		context.Context,
		adaptiveBenchmarkPool,
		uuid.UUID,
	) (bool, error)
	beginOverride func(
		context.Context,
		adaptiveBenchmarkPool,
		settingsBenchmarkOverrideRequest,
	) (adaptiveBenchmarkOverride, error)
	overlaySettings func(context.Context, adaptiveBenchmarkPool) error
	loadServeConfig func() (*config.Config, error)
	openRuntimePool func(
		context.Context,
		*config.Config,
	) (adaptiveBenchmarkPool, error)
	assembleRuntime func(
		context.Context,
		*config.Config,
		adaptiveBenchmarkPool,
		adaptiveBenchmarkRunRequest,
		uuid.UUID,
		auraeval.AdaptiveBenchmarkModel,
	) (adaptiveBenchmarkEnvironment, error)

	bootProduction func(
		context.Context,
		adaptiveBenchmarkRunRequest,
		uuid.UUID,
	) (adaptiveBenchmarkEnvironment, auraeval.AdaptiveBenchmarkModel, error)

	heartbeatInterval time.Duration
}

type adaptiveBenchmarkLifecycle struct {
	request     adaptiveBenchmarkRunRequest
	runID       uuid.UUID
	model       auraeval.AdaptiveBenchmarkModel
	environment adaptiveBenchmarkEnvironment
	override    adaptiveBenchmarkOverride
	controlPool adaptiveBenchmarkPool

	heartbeatStop context.CancelFunc
	heartbeatDone chan struct{}
	failureCtx    context.Context
	fail          context.CancelCauseFunc

	closeOnce sync.Once
	closeErr  error

	stateMu   sync.Mutex
	run       bool
	closed    bool
	finalized bool
	draft     *adaptiveBenchmarkReportDraft
}

type adaptiveBenchmarkChatEnvironment struct {
	env                *chatEnv
	bundle             *adaptiveBenchmarkDecisionBundle
	recorder           *adaptiveBenchmarkTeeRecorder
	store              *adaptive.Store
	observedClient     *adaptiveBenchmarkObservedClient
	adaptiveControls   adaptiveControlSet
	fixtureAliases     adaptiveBenchmarkFixtureAliasResolver
	fixtureCleanup     func(context.Context) error
	reassemble         adaptiveBenchmarkRuntimeReassembler
	forkControl        adaptiveBenchmarkRuntimeForker
	controlEnvironment *adaptiveBenchmarkChatEnvironment
	outcomesForbidden  bool

	runtimeMu sync.Mutex
	closeOnce sync.Once
	closeErr  error
}

func (environment *adaptiveBenchmarkChatEnvironment) Run(
	ctx context.Context,
	request adaptiveBenchmarkRunRequest,
	runID uuid.UUID,
	model auraeval.AdaptiveBenchmarkModel,
) (*adaptiveBenchmarkReportDraft, error) {
	if environment == nil || environment.env == nil {
		return nil, errors.New("adaptive benchmark runtime is unavailable")
	}
	return runAdaptiveBenchmarkInChatEnvironment(
		ctx,
		environment,
		request,
		runID,
		model,
	)
}

func (environment *adaptiveBenchmarkChatEnvironment) Finalize(
	ctx context.Context,
	draft *adaptiveBenchmarkReportDraft,
) ([]byte, error) {
	return finalizeAdaptiveBenchmarkReportDraft(ctx, draft)
}

func (environment *adaptiveBenchmarkChatEnvironment) ControlEnvironment() (
	*adaptiveBenchmarkChatEnvironment,
	error,
) {
	if environment == nil {
		return nil, errors.New(
			"adaptive benchmark control runtime is unavailable",
		)
	}
	environment.runtimeMu.Lock()
	defer environment.runtimeMu.Unlock()
	control := environment.controlEnvironment
	if control == nil ||
		control.env == nil ||
		control.bundle == nil ||
		control.recorder == nil ||
		control.store == nil ||
		control.observedClient == nil ||
		control.adaptiveControls.memoryRecall == nil ||
		!control.outcomesForbidden {
		return nil, errors.New(
			"adaptive benchmark control runtime is unavailable",
		)
	}
	return control, nil
}

func (environment *adaptiveBenchmarkChatEnvironment) Close() error {
	if environment == nil {
		return nil
	}
	environment.closeOnce.Do(func() {
		environment.runtimeMu.Lock()
		defer environment.runtimeMu.Unlock()
		if environment.controlEnvironment != nil {
			environment.closeErr = errors.Join(
				environment.closeErr,
				environment.controlEnvironment.Close(),
			)
			environment.controlEnvironment = nil
		}
		if environment.fixtureCleanup != nil {
			cleanupCtx, cancel := context.WithTimeout(
				context.Background(),
				adaptiveBenchmarkFixtureCleanupTimeout,
			)
			environment.closeErr = errors.Join(
				environment.closeErr,
				environment.fixtureCleanup(cleanupCtx),
			)
			cancel()
			environment.fixtureCleanup = nil
		}
		if environment.env != nil {
			environment.env.close()
			environment.env = nil
		}
		environment.reassemble = nil
	})
	return environment.closeErr
}

func newAdaptiveBenchmarkRunExecution(
	ctx context.Context,
	request adaptiveBenchmarkRunRequest,
) (adaptiveBenchmarkRunExecution, error) {
	return newAdaptiveBenchmarkRunExecutionWithOps(
		ctx,
		request,
		productionAdaptiveBenchmarkLifecycleOps(),
	)
}

func newAdaptiveBenchmarkRunExecutionWithOps(
	ctx context.Context,
	request adaptiveBenchmarkRunRequest,
	ops adaptiveBenchmarkLifecycleOps,
) (adaptiveBenchmarkRunExecution, error) {
	if err := validateAdaptiveBenchmarkLifecycleOps(ops); err != nil {
		return nil, err
	}
	runID, err := ops.newRunID()
	if err != nil || runID == uuid.Nil {
		return nil, errors.New("create adaptive benchmark run identity")
	}
	if request.Mode == auraeval.AdaptiveBenchmarkModeProductionShadow {
		environment, model, bootErr := ops.bootProduction(
			ctx,
			request,
			runID,
		)
		if bootErr != nil {
			return nil, bootErr
		}
		if environment == nil {
			return nil, errors.New("adaptive benchmark runtime is unavailable")
		}
		return &adaptiveBenchmarkLifecycle{
			request: request, runID: runID, model: model,
			environment: environment,
		}, nil
	}
	return newQwenAdaptiveBenchmarkLifecycle(
		ctx,
		request,
		runID,
		ops,
	)
}

func newQwenAdaptiveBenchmarkLifecycle(
	ctx context.Context,
	request adaptiveBenchmarkRunRequest,
	runID uuid.UUID,
	ops adaptiveBenchmarkLifecycleOps,
) (_ adaptiveBenchmarkRunExecution, returnedErr error) {
	model, err := ops.preflightQwen(ctx)
	if err != nil {
		return nil, err
	}
	controlPool, err := ops.openControlPool(ctx)
	if err != nil {
		return nil, fmt.Errorf("open benchmark control pool: %w", err)
	}
	if controlPool == nil {
		return nil, errors.New("benchmark control pool is unavailable")
	}
	allowed, err := ops.authorizeQwen(
		ctx,
		controlPool,
		request.OwnerID,
	)
	if err != nil {
		controlPool.Close()
		return nil, fmt.Errorf(
			"authorize Qwen settings override: %w",
			err,
		)
	}
	if !allowed {
		controlPool.Close()
		return nil, errors.New(
			"qwen settings override requires governance.write",
		)
	}
	override, err := ops.beginOverride(
		ctx,
		controlPool,
		settings.BenchmarkOverrideRequest{
			RunID: runID, OwnerID: request.OwnerID,
			LeaseDuration: adaptiveBenchmarkOverrideLease,
		},
	)
	if err != nil {
		controlPool.Close()
		return nil, fmt.Errorf("begin Qwen settings override: %w", err)
	}
	if override == nil {
		controlPool.Close()
		return nil, errors.New("qwen settings override is unavailable")
	}
	lifecycle := &adaptiveBenchmarkLifecycle{
		request: request, runID: runID, model: model,
		override: override, controlPool: controlPool,
	}
	lifecycleOwned := true
	defer func() {
		if lifecycleOwned {
			returnedErr = errors.Join(returnedErr, lifecycle.Close())
		}
	}()
	lifecycle.startHeartbeat(ops.heartbeatInterval)
	if err := ops.overlaySettings(ctx, controlPool); err != nil {
		return nil, fmt.Errorf("overlay Qwen benchmark settings: %w", err)
	}
	cfg, err := ops.loadServeConfig()
	if err != nil {
		return nil, fmt.Errorf("reload Qwen benchmark config: %w", err)
	}
	runtimePool, err := ops.openRuntimePool(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open benchmark runtime pool: %w", err)
	}
	if runtimePool == nil {
		return nil, errors.New("benchmark runtime pool is unavailable")
	}
	runtimePoolOwned := true
	defer func() {
		if runtimePoolOwned {
			runtimePool.Close()
		}
	}()
	environment, err := ops.assembleRuntime(
		ctx,
		cfg,
		runtimePool,
		request,
		runID,
		model,
	)
	if err != nil {
		return nil, err
	}
	if environment == nil {
		return nil, errors.New("adaptive benchmark runtime is unavailable")
	}
	lifecycle.environment = environment
	runtimePoolOwned = false
	lifecycleOwned = false
	return lifecycle, nil
}

func (lifecycle *adaptiveBenchmarkLifecycle) startHeartbeat(
	interval time.Duration,
) {
	if lifecycle.override == nil {
		return
	}
	heartbeatCtx, stop := context.WithCancel(context.Background())
	failureCtx, fail := context.WithCancelCause(context.Background())
	lifecycle.heartbeatStop = stop
	lifecycle.heartbeatDone = make(chan struct{})
	lifecycle.failureCtx = failureCtx
	lifecycle.fail = fail
	go func() {
		defer close(lifecycle.heartbeatDone)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				if err := lifecycle.override.Heartbeat(heartbeatCtx); err != nil {
					fail(err)
					return
				}
			}
		}
	}()
}

func (lifecycle *adaptiveBenchmarkLifecycle) Run(
	ctx context.Context,
) error {
	if lifecycle == nil || lifecycle.environment == nil {
		return errors.New("adaptive benchmark runtime is unavailable")
	}
	lifecycle.stateMu.Lock()
	if lifecycle.run || lifecycle.closed {
		lifecycle.stateMu.Unlock()
		return errors.New("adaptive benchmark execution already advanced")
	}
	lifecycle.run = true
	lifecycle.stateMu.Unlock()
	var draft *adaptiveBenchmarkReportDraft
	var err error
	if lifecycle.failureCtx == nil {
		draft, err = lifecycle.environment.Run(
			ctx,
			lifecycle.request,
			lifecycle.runID,
			lifecycle.model,
		)
	} else {
		runCtx, cancel := context.WithCancelCause(ctx)
		stopFailureRelay := context.AfterFunc(lifecycle.failureCtx, func() {
			cancel(context.Cause(lifecycle.failureCtx))
		})
		draft, err = lifecycle.environment.Run(
			runCtx,
			lifecycle.request,
			lifecycle.runID,
			lifecycle.model,
		)
		stopFailureRelay()
		cancel(context.Canceled)
		if cause := context.Cause(lifecycle.failureCtx); cause != nil {
			err = errors.Join(cause, err)
		}
	}
	if err != nil {
		return err
	}
	if draft == nil {
		return errors.New("adaptive benchmark report draft is unavailable")
	}
	lifecycle.stateMu.Lock()
	lifecycle.draft = draft
	lifecycle.stateMu.Unlock()
	return nil
}

func (lifecycle *adaptiveBenchmarkLifecycle) Close() error {
	if lifecycle == nil {
		return nil
	}
	lifecycle.closeOnce.Do(func() {
		if lifecycle.heartbeatStop != nil {
			lifecycle.heartbeatStop()
			<-lifecycle.heartbeatDone
		}
		var closeErr error
		if lifecycle.environment != nil {
			closeErr = lifecycle.environment.Close()
		}
		if lifecycle.override != nil {
			closeErr = errors.Join(
				closeErr,
				lifecycle.override.Restore(context.Background()),
			)
		}
		if lifecycle.controlPool != nil {
			lifecycle.controlPool.Close()
		}
		lifecycle.stateMu.Lock()
		lifecycle.closeErr = closeErr
		lifecycle.closed = true
		lifecycle.stateMu.Unlock()
	})
	lifecycle.stateMu.Lock()
	defer lifecycle.stateMu.Unlock()
	return lifecycle.closeErr
}

func (lifecycle *adaptiveBenchmarkLifecycle) Finalize(
	ctx context.Context,
) ([]byte, error) {
	if lifecycle == nil || lifecycle.environment == nil {
		return nil, errors.New("adaptive benchmark runtime is unavailable")
	}
	lifecycle.stateMu.Lock()
	if !lifecycle.run ||
		!lifecycle.closed ||
		lifecycle.closeErr != nil ||
		lifecycle.draft == nil ||
		lifecycle.finalized {
		lifecycle.stateMu.Unlock()
		return nil, errors.New(
			"adaptive benchmark execution cannot be finalized",
		)
	}
	lifecycle.finalized = true
	draft := lifecycle.draft
	lifecycle.stateMu.Unlock()
	return lifecycle.environment.Finalize(ctx, draft)
}

func validateAdaptiveBenchmarkLifecycleOps(
	ops adaptiveBenchmarkLifecycleOps,
) error {
	if ops.newRunID == nil ||
		ops.preflightQwen == nil ||
		ops.openControlPool == nil ||
		ops.authorizeQwen == nil ||
		ops.beginOverride == nil ||
		ops.overlaySettings == nil ||
		ops.loadServeConfig == nil ||
		ops.openRuntimePool == nil ||
		ops.assembleRuntime == nil ||
		ops.bootProduction == nil ||
		ops.heartbeatInterval <= 0 {
		return errors.New("adaptive benchmark lifecycle is unavailable")
	}
	return nil
}
