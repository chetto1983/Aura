package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/config"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAdaptiveBenchmarkEnvironmentRestartReassemblesWithoutFixtureCleanup(
	t *testing.T,
) {
	t.Parallel()
	oldPool := unreachablePool(t)
	newPool := unreachablePool(t)
	oldClient := adaptiveBenchmarkObservedClientForTest(t)
	newClient := adaptiveBenchmarkObservedClientForTest(t)
	cleanupCalls := 0
	restartCalls := 0
	environment := &adaptiveBenchmarkChatEnvironment{
		env:            &chatEnv{pool: oldPool},
		recorder:       &adaptiveBenchmarkTeeRecorder{},
		store:          &adaptive.Store{},
		observedClient: oldClient,
		fixtureCleanup: func(context.Context) error {
			cleanupCalls++
			return nil
		},
		reassemble: func(
			context.Context,
		) (adaptiveBenchmarkRuntimeState, error) {
			restartCalls++
			return adaptiveBenchmarkRuntimeState{
				env:            &chatEnv{pool: newPool},
				recorder:       &adaptiveBenchmarkTeeRecorder{},
				store:          &adaptive.Store{},
				observedClient: newClient,
			}, nil
		},
	}

	if err := environment.Restart(t.Context()); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	assertPoolClosed(t, oldPool)
	if restartCalls != 1 ||
		cleanupCalls != 0 ||
		environment.env == nil ||
		environment.env.pool != newPool ||
		environment.observedClient != newClient {
		t.Fatalf(
			"restart=%d cleanup=%d env=%#v client=%p",
			restartCalls,
			cleanupCalls,
			environment.env,
			environment.observedClient,
		)
	}
	if err := environment.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	assertPoolClosed(t, newPool)
	if cleanupCalls != 1 {
		t.Fatalf("fixture cleanup calls=%d, want 1", cleanupCalls)
	}
}

func TestAdaptiveBenchmarkEnvironmentRestartFailureLeavesCleanupOwned(
	t *testing.T,
) {
	t.Parallel()
	oldPool := unreachablePool(t)
	restartErr := errors.New("reassembly failed")
	cleanupCalls := 0
	environment := &adaptiveBenchmarkChatEnvironment{
		env:            &chatEnv{pool: oldPool},
		recorder:       &adaptiveBenchmarkTeeRecorder{},
		store:          &adaptive.Store{},
		observedClient: adaptiveBenchmarkObservedClientForTest(t),
		fixtureCleanup: func(context.Context) error {
			cleanupCalls++
			return nil
		},
		reassemble: func(
			context.Context,
		) (adaptiveBenchmarkRuntimeState, error) {
			return adaptiveBenchmarkRuntimeState{}, restartErr
		},
	}

	if err := environment.Restart(t.Context()); !errors.Is(
		err,
		restartErr,
	) {
		t.Fatalf("Restart error=%v", err)
	}
	assertPoolClosed(t, oldPool)
	if environment.env != nil || cleanupCalls != 0 {
		t.Fatalf(
			"env=%#v cleanup_calls=%d",
			environment.env,
			cleanupCalls,
		)
	}
	if err := environment.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if cleanupCalls != 1 {
		t.Fatalf("fixture cleanup calls=%d, want 1", cleanupCalls)
	}
}

func TestAdaptiveBenchmarkControlEnvironmentCoexistsAndClosesIndependently(
	t *testing.T,
) {
	t.Parallel()
	primaryPool := unreachablePool(t)
	controlPool := unreachablePool(t)
	bundle := &adaptiveBenchmarkDecisionBundle{}
	control := &adaptiveBenchmarkChatEnvironment{
		env:               &chatEnv{pool: controlPool},
		bundle:            bundle,
		recorder:          &adaptiveBenchmarkTeeRecorder{},
		store:             &adaptive.Store{},
		observedClient:    adaptiveBenchmarkObservedClientForTest(t),
		outcomesForbidden: true,
	}
	primary := &adaptiveBenchmarkChatEnvironment{
		env:                &chatEnv{pool: primaryPool},
		bundle:             bundle,
		controlEnvironment: control,
	}
	got, err := primary.ControlEnvironment()
	if err == nil || got != nil {
		t.Fatalf("incomplete control=%#v error=%v", got, err)
	}
	control.adaptiveControls.memoryRecall =
		adaptiveBenchmarkDynamicRecallControlFake{}
	got, err = primary.ControlEnvironment()
	if err != nil || got != control || !got.outcomesForbidden {
		t.Fatalf("control=%#v error=%v", got, err)
	}
	if err := primary.Restart(t.Context()); err == nil {
		t.Fatal("primary without restart seam restarted")
	}
	if control.env == nil {
		t.Fatal("primary operation closed coexisting control runtime")
	}
	if err := primary.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	assertPoolClosed(t, primaryPool)
	assertPoolClosed(t, controlPool)
	if control.env != nil {
		t.Fatal("control runtime pointer survived cleanup")
	}
}

func TestAdaptiveBenchmarkEnvironmentForkControlAppliesMemoryTransform(
	t *testing.T,
) {
	t.Parallel()
	forkPool := unreachablePool(t)
	original := &adaptiveBenchmarkDynamicRecallControlFake{}
	transformed := &adaptiveBenchmarkDynamicRecallControlFake{}
	forkCalls := 0
	primary := &adaptiveBenchmarkChatEnvironment{
		env:    &chatEnv{},
		bundle: &adaptiveBenchmarkDecisionBundle{},
		forkControl: func(
			_ context.Context,
			transform adaptiveBenchmarkMemoryControlTransform,
		) (adaptiveBenchmarkRuntimeState, error) {
			forkCalls++
			got := transform(original)
			if got != transformed {
				t.Fatalf("transformed control=%T", got)
			}
			return adaptiveBenchmarkRuntimeState{
				env:            &chatEnv{pool: forkPool},
				recorder:       &adaptiveBenchmarkTeeRecorder{},
				store:          &adaptive.Store{},
				observedClient: adaptiveBenchmarkObservedClientForTest(t),
				adaptiveControls: adaptiveControlSet{
					memoryRecall: got,
				},
			}, nil
		},
	}
	fork, err := primary.ForkControl(
		t.Context(),
		func(
			control runner.DynamicRecallControl,
		) runner.DynamicRecallControl {
			if control != original {
				t.Fatalf("original control=%T", control)
			}
			return transformed
		},
	)
	if err != nil {
		t.Fatalf("ForkControl: %v", err)
	}
	if forkCalls != 1 ||
		fork == primary ||
		fork.bundle != primary.bundle ||
		!fork.outcomesForbidden ||
		fork.adaptiveControls.memoryRecall != transformed {
		t.Fatalf(
			"calls=%d fork=%#v",
			forkCalls,
			fork,
		)
	}
	if err := fork.Close(); err != nil {
		t.Fatalf("Close fork: %v", err)
	}
	assertPoolClosed(t, forkPool)
	if primary.env == nil {
		t.Fatal("fork closed primary runtime")
	}
}

func TestAdaptiveBenchmarkReassemblerUsesFrozenRuntimeInputs(
	t *testing.T,
) {
	t.Parallel()
	cfg := &config.Config{}
	request := adaptiveBenchmarkRunRequest{
		OwnerID: uuid.Must(uuid.NewV7()),
		Mode:    auraeval.AdaptiveBenchmarkModeProductionShadow,
	}
	runID := uuid.Must(uuid.NewV7())
	model := auraeval.AdaptiveBenchmarkModel{
		ProviderID: "provider",
		ModelID:    "model",
	}
	bundle := &adaptiveBenchmarkDecisionBundle{}
	pool := unreachablePool(t)
	events := []string{}
	reassemble := newAdaptiveBenchmarkRuntimeReassembler(
		cfg,
		request,
		runID,
		model,
		bundle,
		adaptiveBenchmarkRestartOps{
			openPool: func(
				_ context.Context,
				got *config.Config,
			) (*pgxpool.Pool, error) {
				events = append(events, "open")
				if got != cfg {
					t.Fatal("restart changed config")
				}
				return pool, nil
			},
			checkMigration: func(context.Context, string) error {
				events = append(events, "migration")
				return nil
			},
			assemble: func(
				_ context.Context,
				gotConfig *config.Config,
				gotPool *pgxpool.Pool,
				gotRequest adaptiveBenchmarkRunRequest,
				gotRunID uuid.UUID,
				gotModel auraeval.AdaptiveBenchmarkModel,
				gotBundle *adaptiveBenchmarkDecisionBundle,
				gotTransform adaptiveBenchmarkMemoryControlTransform,
			) (adaptiveBenchmarkRuntimeState, error) {
				events = append(events, "assemble")
				if gotConfig != cfg ||
					gotPool != pool ||
					gotRequest != request ||
					gotRunID != runID ||
					gotModel != model ||
					gotBundle != bundle ||
					gotTransform != nil {
					t.Fatal("restart inputs drifted")
				}
				return adaptiveBenchmarkRuntimeState{
					env:            &chatEnv{pool: pool},
					recorder:       &adaptiveBenchmarkTeeRecorder{},
					store:          &adaptive.Store{},
					observedClient: adaptiveBenchmarkObservedClientForTest(t),
				}, nil
			},
		},
		nil,
	)
	state, err := reassemble(t.Context())
	if err != nil {
		t.Fatalf("reassemble: %v", err)
	}
	if len(events) != 3 ||
		events[0] != "open" ||
		events[1] != "migration" ||
		events[2] != "assemble" {
		t.Fatalf("events=%v", events)
	}
	state.env.close()
}

func adaptiveBenchmarkObservedClientForTest(
	t *testing.T,
) *adaptiveBenchmarkObservedClient {
	t.Helper()
	client, err := newAdaptiveBenchmarkObservedClient(
		&adaptiveBenchmarkLLMClientFake{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

type adaptiveBenchmarkDynamicRecallControlFake struct{}

func (adaptiveBenchmarkDynamicRecallControlFake) PrepareDynamicRecall(
	context.Context,
	runner.DynamicRecallInput,
	runner.DynamicRecallProvider,
) (*runner.PreparedDynamicRecall, error) {
	return nil, nil
}
