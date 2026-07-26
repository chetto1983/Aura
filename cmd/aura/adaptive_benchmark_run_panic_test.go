package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"slices"
	"syscall"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

func TestRunAdaptiveBenchmarkCommandCancelsOnInterruptAndRestores(
	t *testing.T,
) {
	t.Parallel()
	request := validAdaptiveBenchmarkRunRequest(
		t,
		"qwen-portability",
	)
	execution := &adaptiveBenchmarkSignalExecution{}
	var registered []os.Signal
	stopCalls := 0
	err := runAdaptiveBenchmarkRunCommandWithSignalNotifier(
		t.Context(),
		adaptiveBenchmarkRunArgs(request),
		&bytes.Buffer{},
		func(
			context.Context,
			adaptiveBenchmarkRunRequest,
		) (adaptiveBenchmarkRunExecution, error) {
			return execution, nil
		},
		func(
			parent context.Context,
			signals ...os.Signal,
		) (context.Context, context.CancelFunc) {
			registered = append([]os.Signal(nil), signals...)
			signalCtx, cancel := context.WithCancel(parent)
			cancel()
			return signalCtx, func() {
				stopCalls++
			}
		},
	)
	if !errors.Is(err, context.Canceled) ||
		!execution.runCanceled ||
		execution.closeCalls != 1 ||
		execution.finalizeCalls != 0 ||
		stopCalls != 1 ||
		len(registered) != 2 ||
		registered[0] != os.Interrupt ||
		registered[1] != syscall.SIGTERM {
		t.Fatalf(
			"error=%v run_canceled=%t close=%d finalize=%d stop=%d signals=%v",
			err,
			execution.runCanceled,
			execution.closeCalls,
			execution.finalizeCalls,
			stopCalls,
			registered,
		)
	}
}

func TestRunAdaptiveBenchmarkCommandClosesExecutionAfterRunPanic(
	t *testing.T,
) {
	t.Parallel()
	panicValue := &struct{ label string }{label: "runtime panic"}
	execution := &adaptiveBenchmarkPanicExecution{
		panicValue: panicValue,
	}
	request := validAdaptiveBenchmarkRunRequest(
		t,
		"qwen-portability",
	)
	recovered := captureAdaptiveBenchmarkPanic(t, func() {
		_ = runAdaptiveBenchmarkRunCommand(
			t.Context(),
			adaptiveBenchmarkRunArgs(request),
			&bytes.Buffer{},
			func(
				context.Context,
				adaptiveBenchmarkRunRequest,
			) (adaptiveBenchmarkRunExecution, error) {
				return execution, nil
			},
		)
	})
	if recovered != panicValue ||
		execution.closeCalls != 1 ||
		execution.finalizeCalls != 0 {
		t.Fatalf(
			"panic=%v close_calls=%d finalize_calls=%d",
			recovered,
			execution.closeCalls,
			execution.finalizeCalls,
		)
	}
}

func TestNewAdaptiveBenchmarkLifecycleRestoresAfterAssemblyPanic(
	t *testing.T,
) {
	t.Parallel()
	panicValue := &struct{ label string }{label: "assembly panic"}
	request := validAdaptiveBenchmarkRunRequest(
		t,
		"qwen-portability",
	)
	events := make([]string, 0, 12)
	control := &adaptiveBenchmarkPoolFake{
		name: "control", events: &events,
	}
	runtime := &adaptiveBenchmarkPoolFake{
		name: "runtime", events: &events,
	}
	override := &adaptiveBenchmarkOverrideFake{events: &events}
	ops := adaptiveBenchmarkLifecycleOpsFixture(&events)
	ops.openControlPool = func(
		context.Context,
	) (adaptiveBenchmarkPool, error) {
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
		panic(panicValue)
	}

	recovered := captureAdaptiveBenchmarkPanic(t, func() {
		_, _ = newAdaptiveBenchmarkRunExecutionWithOps(
			t.Context(),
			request,
			ops,
		)
	})
	wantEvents := []string{
		"preflight",
		"open-control",
		"authorize",
		"begin-override",
		"overlay",
		"load-serve",
		"open-runtime",
		"assemble-runtime",
		"close-runtime",
		"restore",
		"close-control",
	}
	if recovered != panicValue || !slices.Equal(events, wantEvents) {
		t.Fatalf(
			"panic=%v events=%v want=%v",
			recovered,
			events,
			wantEvents,
		)
	}
}

func captureAdaptiveBenchmarkPanic(
	t *testing.T,
	run func(),
) (recovered any) {
	t.Helper()
	defer func() {
		recovered = recover()
	}()
	run()
	t.Fatal("operation did not panic")
	return nil
}

type adaptiveBenchmarkPanicExecution struct {
	panicValue    any
	closeCalls    int
	finalizeCalls int
}

type adaptiveBenchmarkSignalExecution struct {
	runCanceled   bool
	closeCalls    int
	finalizeCalls int
}

func (execution *adaptiveBenchmarkSignalExecution) Run(
	ctx context.Context,
) error {
	<-ctx.Done()
	execution.runCanceled = true
	return context.Cause(ctx)
}

func (execution *adaptiveBenchmarkSignalExecution) Close() error {
	execution.closeCalls++
	return nil
}

func (execution *adaptiveBenchmarkSignalExecution) Finalize(
	context.Context,
) ([]byte, error) {
	execution.finalizeCalls++
	return nil, nil
}

func (execution *adaptiveBenchmarkPanicExecution) Run(context.Context) error {
	panic(execution.panicValue)
}

func (execution *adaptiveBenchmarkPanicExecution) Close() error {
	execution.closeCalls++
	return nil
}

func (execution *adaptiveBenchmarkPanicExecution) Finalize(
	context.Context,
) ([]byte, error) {
	execution.finalizeCalls++
	return []byte(`{"unexpected":true}`), nil
}
