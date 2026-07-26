package main

import (
	"context"
	"errors"
	"sync"

	"github.com/chetto1983/aura/internal/config"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/settings"
	"github.com/google/uuid"
)

var (
	errAdaptiveBenchmarkInjectedSettingsApply = errors.New(
		"adaptive benchmark injected settings apply failure",
	)
	errAdaptiveBenchmarkInjectedSettingsRestore = errors.New(
		"adaptive benchmark injected settings restore failure",
	)
	errAdaptiveBenchmarkInjectedStaleRecovery = errors.New(
		"adaptive benchmark injected stale override recovery",
	)
)

type adaptiveBenchmarkControlPoolProbe struct {
	mu     sync.Mutex
	closed int
}

func (pool *adaptiveBenchmarkControlPoolProbe) Close() {
	pool.mu.Lock()
	pool.closed++
	pool.mu.Unlock()
}

func (pool *adaptiveBenchmarkControlPoolProbe) closeCount() int {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	return pool.closed
}

type adaptiveBenchmarkControlOverrideProbe struct {
	mu           sync.Mutex
	restoreCalls int
	failRestore  bool
}

func (*adaptiveBenchmarkControlOverrideProbe) Heartbeat(
	context.Context,
) error {
	return nil
}

func (override *adaptiveBenchmarkControlOverrideProbe) Restore(
	context.Context,
) error {
	override.mu.Lock()
	defer override.mu.Unlock()
	override.restoreCalls++
	if override.failRestore {
		override.failRestore = false
		return errAdaptiveBenchmarkInjectedSettingsRestore
	}
	return nil
}

func (override *adaptiveBenchmarkControlOverrideProbe) calls() int {
	override.mu.Lock()
	defer override.mu.Unlock()
	return override.restoreCalls
}

type adaptiveBenchmarkControlEnvironmentProbe struct {
	mu            sync.Mutex
	runCalls      int
	closeCalls    int
	finalizeCalls int
}

func (environment *adaptiveBenchmarkControlEnvironmentProbe) Run(
	context.Context,
	adaptiveBenchmarkRunRequest,
	uuid.UUID,
	auraeval.AdaptiveBenchmarkModel,
) (*adaptiveBenchmarkReportDraft, error) {
	environment.mu.Lock()
	environment.runCalls++
	environment.mu.Unlock()
	return &adaptiveBenchmarkReportDraft{}, nil
}

func (environment *adaptiveBenchmarkControlEnvironmentProbe) Close() error {
	environment.mu.Lock()
	environment.closeCalls++
	environment.mu.Unlock()
	return nil
}

func (environment *adaptiveBenchmarkControlEnvironmentProbe) Finalize(
	context.Context,
	*adaptiveBenchmarkReportDraft,
) ([]byte, error) {
	environment.mu.Lock()
	environment.finalizeCalls++
	environment.mu.Unlock()
	return []byte(`{}`), nil
}

func (environment *adaptiveBenchmarkControlEnvironmentProbe) counts() (
	int,
	int,
	int,
) {
	environment.mu.Lock()
	defer environment.mu.Unlock()
	return environment.runCalls,
		environment.closeCalls,
		environment.finalizeCalls
}

type adaptiveBenchmarkSettingsControlTrace struct {
	mu       sync.Mutex
	events   []string
	pool     *adaptiveBenchmarkControlPoolProbe
	override *adaptiveBenchmarkControlOverrideProbe
	env      *adaptiveBenchmarkControlEnvironmentProbe
}

func (trace *adaptiveBenchmarkSettingsControlTrace) record(event string) {
	trace.mu.Lock()
	trace.events = append(trace.events, event)
	trace.mu.Unlock()
}

func (trace *adaptiveBenchmarkSettingsControlTrace) snapshot() []string {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	return append([]string(nil), trace.events...)
}

func (runtime *adaptiveBenchmarkControlRuntime) settingsControlOps(
	runID uuid.UUID,
	trace *adaptiveBenchmarkSettingsControlTrace,
) adaptiveBenchmarkLifecycleOps {
	requestModel := auraeval.AdaptiveBenchmarkModel{
		ProviderID: runtime.environment.bundle.providerID,
		ModelID:    runtime.environment.bundle.modelID,
	}
	return adaptiveBenchmarkLifecycleOps{
		newRunID: func() (uuid.UUID, error) {
			return runID, nil
		},
		preflightQwen: func(context.Context) (
			auraeval.AdaptiveBenchmarkModel,
			error,
		) {
			trace.record("preflight")
			return requestModel, nil
		},
		openControlPool: func(context.Context) (
			adaptiveBenchmarkPool,
			error,
		) {
			trace.record("open-control")
			return trace.pool, nil
		},
		authorizeQwen: func(
			context.Context,
			adaptiveBenchmarkPool,
			uuid.UUID,
		) (bool, error) {
			trace.record("authorize")
			return true, nil
		},
		beginOverride: func(
			context.Context,
			adaptiveBenchmarkPool,
			settingsBenchmarkOverrideRequest,
		) (adaptiveBenchmarkOverride, error) {
			trace.record("begin")
			return trace.override, nil
		},
		overlaySettings: func(
			context.Context,
			adaptiveBenchmarkPool,
		) error {
			trace.record("overlay")
			return nil
		},
		loadServeConfig: func() (*config.Config, error) {
			trace.record("load-config")
			return &config.Config{}, nil
		},
		openRuntimePool: func(
			context.Context,
			*config.Config,
		) (adaptiveBenchmarkPool, error) {
			trace.record("open-runtime")
			return &adaptiveBenchmarkControlPoolProbe{}, nil
		},
		assembleRuntime: func(
			context.Context,
			*config.Config,
			adaptiveBenchmarkPool,
			adaptiveBenchmarkRunRequest,
			uuid.UUID,
			auraeval.AdaptiveBenchmarkModel,
		) (adaptiveBenchmarkEnvironment, error) {
			trace.record("assemble")
			return trace.env, nil
		},
		bootProduction: func(
			context.Context,
			adaptiveBenchmarkRunRequest,
			uuid.UUID,
		) (
			adaptiveBenchmarkEnvironment,
			auraeval.AdaptiveBenchmarkModel,
			error,
		) {
			return nil, auraeval.AdaptiveBenchmarkModel{}, errors.New(
				"production boot is outside settings fault control",
			)
		},
		heartbeatInterval: adaptiveBenchmarkHeartbeatInterval,
	}
}

func (runtime *adaptiveBenchmarkControlRuntime) settingsControlRequest() adaptiveBenchmarkRunRequest {
	return adaptiveBenchmarkRunRequest{
		OwnerID:                   runtime.request.OwnerID,
		OutputDir:                 runtime.request.OutputDir,
		Mode:                      auraeval.AdaptiveBenchmarkModeQwenPortability,
		ConfirmedSettingsOverride: true,
	}
}

func (runtime *adaptiveBenchmarkControlRuntime) runSettingsApplyFailure(
	ctx context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error) {
	if err := adaptiveBenchmarkUnexpectedControlCase(
		control,
		"settings_apply_failure",
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	runID, err := uuid.NewV7()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	trace := &adaptiveBenchmarkSettingsControlTrace{
		pool:     &adaptiveBenchmarkControlPoolProbe{},
		override: &adaptiveBenchmarkControlOverrideProbe{},
		env:      &adaptiveBenchmarkControlEnvironmentProbe{},
	}
	ops := runtime.settingsControlOps(runID, trace)
	ops.overlaySettings = func(
		context.Context,
		adaptiveBenchmarkPool,
	) error {
		trace.record("overlay-failed")
		return errAdaptiveBenchmarkInjectedSettingsApply
	}
	execution, err := newAdaptiveBenchmarkRunExecutionWithOps(
		ctx,
		runtime.settingsControlRequest(),
		ops,
	)
	if execution != nil ||
		!errors.Is(err, errAdaptiveBenchmarkInjectedSettingsApply) ||
		trace.override.calls() != 1 ||
		trace.pool.closeCount() != 1 ||
		containsAdaptiveBenchmarkControlEvent(
			trace.snapshot(),
			"open-runtime",
		) {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark settings apply failure was not contained",
			)
	}
	return adaptiveBenchmarkControlExecution{
		EvidenceIDs:                []uuid.UUID{runID},
		ObservedFailureReasonCodes: adaptiveBenchmarkExpectedControlFaults(control.ControlID),
	}, nil
}

func (runtime *adaptiveBenchmarkControlRuntime) runSettingsRestoreFailure(
	ctx context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error) {
	if err := adaptiveBenchmarkUnexpectedControlCase(
		control,
		"settings_restore_failure",
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	runID, err := uuid.NewV7()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	trace := &adaptiveBenchmarkSettingsControlTrace{
		pool: &adaptiveBenchmarkControlPoolProbe{},
		override: &adaptiveBenchmarkControlOverrideProbe{
			failRestore: true,
		},
		env: &adaptiveBenchmarkControlEnvironmentProbe{},
	}
	execution, err := newAdaptiveBenchmarkRunExecutionWithOps(
		ctx,
		runtime.settingsControlRequest(),
		runtime.settingsControlOps(runID, trace),
	)
	if err != nil || execution == nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	runErr := execution.Run(ctx)
	closeErr := execution.Close()
	_, finalizeErr := execution.Finalize(ctx)
	_, _, finalizeCalls := trace.env.counts()
	recoveryErr := trace.override.Restore(ctx)
	if runErr != nil ||
		!errors.Is(
			closeErr,
			errAdaptiveBenchmarkInjectedSettingsRestore,
		) ||
		finalizeErr == nil ||
		finalizeCalls != 0 ||
		recoveryErr != nil ||
		trace.override.calls() != 2 {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark settings restore failure was publishable or unrecoverable",
			)
	}
	return adaptiveBenchmarkControlExecution{
		EvidenceIDs:                []uuid.UUID{runID},
		ObservedFailureReasonCodes: adaptiveBenchmarkExpectedControlFaults(control.ControlID),
	}, nil
}

func (runtime *adaptiveBenchmarkControlRuntime) runStaleOverrideRecovery(
	ctx context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error) {
	if err := adaptiveBenchmarkUnexpectedControlCase(
		control,
		"stale_override_recovery",
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	runID, err := uuid.NewV7()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	trace := &adaptiveBenchmarkSettingsControlTrace{
		pool:     &adaptiveBenchmarkControlPoolProbe{},
		override: &adaptiveBenchmarkControlOverrideProbe{},
		env:      &adaptiveBenchmarkControlEnvironmentProbe{},
	}
	ops := runtime.settingsControlOps(runID, trace)
	ops.beginOverride = func(
		context.Context,
		adaptiveBenchmarkPool,
		settings.BenchmarkOverrideRequest,
	) (adaptiveBenchmarkOverride, error) {
		trace.record("stale-recovery-failed")
		return nil, errAdaptiveBenchmarkInjectedStaleRecovery
	}
	execution, err := newAdaptiveBenchmarkRunExecutionWithOps(
		ctx,
		runtime.settingsControlRequest(),
		ops,
	)
	if execution != nil ||
		!errors.Is(err, errAdaptiveBenchmarkInjectedStaleRecovery) ||
		trace.pool.closeCount() != 1 ||
		containsAdaptiveBenchmarkControlEvent(
			trace.snapshot(),
			"overlay",
		) ||
		containsAdaptiveBenchmarkControlEvent(
			trace.snapshot(),
			"open-runtime",
		) {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark stale recovery failure reached runtime",
			)
	}
	return adaptiveBenchmarkControlExecution{
		EvidenceIDs:                []uuid.UUID{runID},
		ObservedFailureReasonCodes: adaptiveBenchmarkExpectedControlFaults(control.ControlID),
	}, nil
}

func containsAdaptiveBenchmarkControlEvent(
	events []string,
	want string,
) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}
	return false
}
