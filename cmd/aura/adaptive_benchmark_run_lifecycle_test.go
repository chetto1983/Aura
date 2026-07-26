package main

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

func TestNewAdaptiveBenchmarkLifecycleQwenOrdersSeparatePoolsAndRestore(t *testing.T) {
	t.Parallel()
	request := validAdaptiveBenchmarkRunRequest(t, "qwen-portability")
	events := make([]string, 0, 10)
	control := &adaptiveBenchmarkPoolFake{name: "control", events: &events}
	runtime := &adaptiveBenchmarkPoolFake{name: "runtime", events: &events}
	override := &adaptiveBenchmarkOverrideFake{events: &events}
	environment := &adaptiveBenchmarkEnvironmentFake{
		events:  &events,
		payload: []byte(`{"ok":true}`),
	}
	ops := adaptiveBenchmarkLifecycleOpsFixture(&events)
	ops.preflightQwen = func(context.Context) (auraeval.AdaptiveBenchmarkModel, error) {
		events = append(events, "preflight")
		return auraeval.AdaptiveBenchmarkModel{ProviderID: "llamacpp", ModelID: "qwen"}, nil
	}
	ops.openControlPool = func(context.Context) (adaptiveBenchmarkPool, error) {
		events = append(events, "open-control")
		return control, nil
	}
	ops.authorizeQwen = func(
		context.Context,
		adaptiveBenchmarkPool,
		uuid.UUID,
	) (bool, error) {
		events = append(events, "authorize")
		return true, nil
	}
	ops.beginOverride = func(
		_ context.Context,
		gotPool adaptiveBenchmarkPool,
		got settingsBenchmarkOverrideRequest,
	) (adaptiveBenchmarkOverride, error) {
		events = append(events, "begin-override")
		if gotPool != control || got.OwnerID != request.OwnerID ||
			got.RunID == uuid.Nil || got.LeaseDuration != 2*time.Minute {
			t.Fatalf("begin override pool=%T request=%#v", gotPool, got)
		}
		return override, nil
	}
	ops.overlaySettings = func(context.Context, adaptiveBenchmarkPool) error {
		events = append(events, "overlay")
		return nil
	}
	ops.loadServeConfig = func() (*config.Config, error) {
		events = append(events, "load-serve")
		return &config.Config{}, nil
	}
	ops.openRuntimePool = func(
		context.Context,
		*config.Config,
	) (adaptiveBenchmarkPool, error) {
		events = append(events, "open-runtime")
		return runtime, nil
	}
	ops.assembleRuntime = func(
		context.Context,
		*config.Config,
		adaptiveBenchmarkPool,
		adaptiveBenchmarkRunRequest,
		uuid.UUID,
		auraeval.AdaptiveBenchmarkModel,
	) (adaptiveBenchmarkEnvironment, error) {
		events = append(events, "assemble-runtime")
		return environment, nil
	}

	execution, err := newAdaptiveBenchmarkRunExecutionWithOps(
		t.Context(),
		request,
		ops,
	)
	if err != nil {
		t.Fatalf("newAdaptiveBenchmarkRunExecutionWithOps: %v", err)
	}
	if err := execution.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := execution.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	payload, err := execution.Finalize(t.Context())
	if err != nil || string(payload) != `{"ok":true}` {
		t.Fatalf("Finalize payload=%q error=%v", payload, err)
	}
	want := []string{
		"preflight",
		"open-control",
		"authorize",
		"begin-override",
		"overlay",
		"load-serve",
		"open-runtime",
		"assemble-runtime",
		"run",
		"close-runtime",
		"restore",
		"close-control",
		"finalize",
	}
	if !slices.Equal(events, want) {
		t.Fatalf("events=%v want=%v", events, want)
	}
}

func TestNewAdaptiveBenchmarkLifecycleProductionMakesNoSettingsOverride(t *testing.T) {
	t.Parallel()
	request := validAdaptiveBenchmarkRunRequest(t, "production-shadow")
	events := make([]string, 0, 4)
	environment := &adaptiveBenchmarkEnvironmentFake{
		events:  &events,
		payload: []byte(`{"ok":true}`),
	}
	ops := adaptiveBenchmarkLifecycleOpsFixture(&events)
	ops.bootProduction = func(
		context.Context,
		adaptiveBenchmarkRunRequest,
		uuid.UUID,
	) (adaptiveBenchmarkEnvironment, auraeval.AdaptiveBenchmarkModel, error) {
		events = append(events, "boot-production")
		return environment, auraeval.AdaptiveBenchmarkModel{
			ProviderID: "openai",
			ModelID:    "configured",
		}, nil
	}
	ops.openControlPool = func(context.Context) (adaptiveBenchmarkPool, error) {
		t.Fatal("production shadow opened a control pool")
		return nil, nil
	}
	ops.beginOverride = func(
		context.Context,
		adaptiveBenchmarkPool,
		settingsBenchmarkOverrideRequest,
	) (adaptiveBenchmarkOverride, error) {
		t.Fatal("production shadow began a settings override")
		return nil, nil
	}

	execution, err := newAdaptiveBenchmarkRunExecutionWithOps(
		t.Context(),
		request,
		ops,
	)
	if err != nil {
		t.Fatalf("newAdaptiveBenchmarkRunExecutionWithOps: %v", err)
	}
	if err := execution.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := execution.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := execution.Finalize(t.Context()); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	want := []string{
		"boot-production",
		"run",
		"close-runtime",
		"finalize",
	}
	if !slices.Equal(events, want) {
		t.Fatalf("events=%v want=%v", events, want)
	}
}

func TestNewAdaptiveBenchmarkLifecycleRestoresAfterQwenAssemblyFailure(t *testing.T) {
	t.Parallel()
	request := validAdaptiveBenchmarkRunRequest(t, "qwen-portability")
	assembleErr := errors.New("assemble failed")
	events := make([]string, 0, 10)
	control := &adaptiveBenchmarkPoolFake{name: "control", events: &events}
	runtime := &adaptiveBenchmarkPoolFake{name: "runtime", events: &events}
	override := &adaptiveBenchmarkOverrideFake{events: &events}
	ops := adaptiveBenchmarkLifecycleOpsFixture(&events)
	ops.openControlPool = func(context.Context) (adaptiveBenchmarkPool, error) {
		events = append(events, "open-control")
		return control, nil
	}
	ops.beginOverride = func(
		context.Context,
		adaptiveBenchmarkPool,
		settingsBenchmarkOverrideRequest,
	) (adaptiveBenchmarkOverride, error) {
		events = append(events, "begin-override")
		return override, nil
	}
	ops.openRuntimePool = func(
		context.Context,
		*config.Config,
	) (adaptiveBenchmarkPool, error) {
		events = append(events, "open-runtime")
		return runtime, nil
	}
	ops.assembleRuntime = func(
		context.Context,
		*config.Config,
		adaptiveBenchmarkPool,
		adaptiveBenchmarkRunRequest,
		uuid.UUID,
		auraeval.AdaptiveBenchmarkModel,
	) (adaptiveBenchmarkEnvironment, error) {
		events = append(events, "assemble-runtime")
		return nil, assembleErr
	}

	execution, err := newAdaptiveBenchmarkRunExecutionWithOps(
		t.Context(),
		request,
		ops,
	)
	if execution != nil || !errors.Is(err, assembleErr) {
		t.Fatalf("execution=%v error=%v", execution, err)
	}
	if indexOfBenchmarkEvent(events, "close-runtime") >
		indexOfBenchmarkEvent(events, "restore") {
		t.Fatalf("runtime was not closed before restore: %v", events)
	}
	if !slices.Contains(events, "close-control") {
		t.Fatalf("control pool was not closed: %v", events)
	}
}

func TestNewAdaptiveBenchmarkLifecycleRejectsMissingQwenOverride(t *testing.T) {
	t.Parallel()
	request := validAdaptiveBenchmarkRunRequest(t, "qwen-portability")
	events := make([]string, 0, 4)
	control := &adaptiveBenchmarkPoolFake{name: "control", events: &events}
	ops := adaptiveBenchmarkLifecycleOpsFixture(&events)
	ops.openControlPool = func(context.Context) (adaptiveBenchmarkPool, error) {
		events = append(events, "open-control")
		return control, nil
	}
	ops.beginOverride = func(
		context.Context,
		adaptiveBenchmarkPool,
		settingsBenchmarkOverrideRequest,
	) (adaptiveBenchmarkOverride, error) {
		events = append(events, "begin-override")
		return nil, nil
	}

	execution, err := newAdaptiveBenchmarkRunExecutionWithOps(
		t.Context(),
		request,
		ops,
	)
	if execution != nil || err == nil {
		t.Fatalf("execution=%v error=%v", execution, err)
	}
	want := []string{
		"preflight",
		"open-control",
		"authorize",
		"begin-override",
		"close-control",
	}
	if !slices.Equal(events, want) {
		t.Fatalf("events=%v want=%v", events, want)
	}
}

func TestAdaptiveBenchmarkLifecycleSurfacesHeartbeatFailure(t *testing.T) {
	t.Parallel()
	request := validAdaptiveBenchmarkRunRequest(t, "qwen-portability")
	heartbeatErr := errors.New("heartbeat failed")
	events := make([]string, 0, 12)
	override := &adaptiveBenchmarkOverrideFake{
		events:       &events,
		heartbeatErr: heartbeatErr,
	}
	environment := &adaptiveBenchmarkEnvironmentFake{
		events: &events,
		run: func(ctx context.Context) (*adaptiveBenchmarkReportDraft, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	ops := adaptiveBenchmarkLifecycleOpsFixture(&events)
	ops.heartbeatInterval = time.Millisecond
	ops.beginOverride = func(
		context.Context,
		adaptiveBenchmarkPool,
		settingsBenchmarkOverrideRequest,
	) (adaptiveBenchmarkOverride, error) {
		events = append(events, "begin-override")
		return override, nil
	}
	ops.assembleRuntime = func(
		context.Context,
		*config.Config,
		adaptiveBenchmarkPool,
		adaptiveBenchmarkRunRequest,
		uuid.UUID,
		auraeval.AdaptiveBenchmarkModel,
	) (adaptiveBenchmarkEnvironment, error) {
		events = append(events, "assemble-runtime")
		return environment, nil
	}

	execution, err := newAdaptiveBenchmarkRunExecutionWithOps(
		t.Context(),
		request,
		ops,
	)
	if err != nil {
		t.Fatalf("newAdaptiveBenchmarkRunExecutionWithOps: %v", err)
	}
	runErr := execution.Run(t.Context())
	if !errors.Is(runErr, heartbeatErr) {
		t.Fatalf("Run error=%v", runErr)
	}
	if err := execution.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func adaptiveBenchmarkLifecycleOpsFixture(
	events *[]string,
) adaptiveBenchmarkLifecycleOps {
	control := &adaptiveBenchmarkPoolFake{name: "control", events: events}
	runtime := &adaptiveBenchmarkPoolFake{name: "runtime", events: events}
	return adaptiveBenchmarkLifecycleOps{
		newRunID: func() (uuid.UUID, error) {
			return uuid.Must(uuid.NewV7()), nil
		},
		preflightQwen: func(context.Context) (auraeval.AdaptiveBenchmarkModel, error) {
			*events = append(*events, "preflight")
			return auraeval.AdaptiveBenchmarkModel{
				ProviderID: "llamacpp",
				ModelID:    "qwen",
			}, nil
		},
		openControlPool: func(context.Context) (adaptiveBenchmarkPool, error) {
			*events = append(*events, "open-control")
			return control, nil
		},
		authorizeQwen: func(
			context.Context,
			adaptiveBenchmarkPool,
			uuid.UUID,
		) (bool, error) {
			*events = append(*events, "authorize")
			return true, nil
		},
		beginOverride: func(
			context.Context,
			adaptiveBenchmarkPool,
			settingsBenchmarkOverrideRequest,
		) (adaptiveBenchmarkOverride, error) {
			*events = append(*events, "begin-override")
			return &adaptiveBenchmarkOverrideFake{events: events}, nil
		},
		overlaySettings: func(context.Context, adaptiveBenchmarkPool) error {
			*events = append(*events, "overlay")
			return nil
		},
		loadServeConfig: func() (*config.Config, error) {
			*events = append(*events, "load-serve")
			return &config.Config{}, nil
		},
		openRuntimePool: func(
			context.Context,
			*config.Config,
		) (adaptiveBenchmarkPool, error) {
			*events = append(*events, "open-runtime")
			return runtime, nil
		},
		assembleRuntime: func(
			context.Context,
			*config.Config,
			adaptiveBenchmarkPool,
			adaptiveBenchmarkRunRequest,
			uuid.UUID,
			auraeval.AdaptiveBenchmarkModel,
		) (adaptiveBenchmarkEnvironment, error) {
			*events = append(*events, "assemble-runtime")
			return &adaptiveBenchmarkEnvironmentFake{
				events:  events,
				payload: []byte(`{"ok":true}`),
			}, nil
		},
		bootProduction: func(
			context.Context,
			adaptiveBenchmarkRunRequest,
			uuid.UUID,
		) (adaptiveBenchmarkEnvironment, auraeval.AdaptiveBenchmarkModel, error) {
			*events = append(*events, "boot-production")
			return &adaptiveBenchmarkEnvironmentFake{
					events:  events,
					payload: []byte(`{"ok":true}`),
				},
				auraeval.AdaptiveBenchmarkModel{
					ProviderID: "openai",
					ModelID:    "configured",
				},
				nil
		},
		heartbeatInterval: 30 * time.Second,
	}
}

type adaptiveBenchmarkPoolFake struct {
	name   string
	events *[]string
}

func (fake *adaptiveBenchmarkPoolFake) Close() {
	*fake.events = append(*fake.events, "close-"+fake.name)
}

type adaptiveBenchmarkOverrideFake struct {
	events       *[]string
	heartbeatErr error
	restoreErr   error
}

func (fake *adaptiveBenchmarkOverrideFake) Heartbeat(context.Context) error {
	return fake.heartbeatErr
}

func (fake *adaptiveBenchmarkOverrideFake) Restore(context.Context) error {
	*fake.events = append(*fake.events, "restore")
	return fake.restoreErr
}

type adaptiveBenchmarkEnvironmentFake struct {
	events        *[]string
	payload       []byte
	run           func(context.Context) (*adaptiveBenchmarkReportDraft, error)
	finalizeCalls int
}

func (fake *adaptiveBenchmarkEnvironmentFake) Run(
	ctx context.Context,
	_ adaptiveBenchmarkRunRequest,
	_ uuid.UUID,
	_ auraeval.AdaptiveBenchmarkModel,
) (*adaptiveBenchmarkReportDraft, error) {
	*fake.events = append(*fake.events, "run")
	if fake.run != nil {
		return fake.run(ctx)
	}
	return &adaptiveBenchmarkReportDraft{}, nil
}

func (fake *adaptiveBenchmarkEnvironmentFake) Close() error {
	*fake.events = append(*fake.events, "close-runtime")
	return nil
}

func (fake *adaptiveBenchmarkEnvironmentFake) Finalize(
	context.Context,
	*adaptiveBenchmarkReportDraft,
) ([]byte, error) {
	fake.finalizeCalls++
	*fake.events = append(*fake.events, "finalize")
	return append([]byte(nil), fake.payload...), nil
}

func indexOfBenchmarkEvent(events []string, want string) int {
	for index, event := range events {
		if event == want {
			return index
		}
	}
	return len(events)
}
