package main

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

func TestNewAdaptiveBenchmarkLifecycleQwenAuthorizesBeforeOverride(
	t *testing.T,
) {
	t.Parallel()
	authorizationErr := errors.New("capability lookup failed")
	tests := []struct {
		name    string
		allowed bool
		err     error
	}{
		{name: "deny"},
		{name: "error", err: authorizationErr},
		{name: "allow", allowed: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			request := validAdaptiveBenchmarkRunRequest(
				t,
				"qwen-portability",
			)
			events := make([]string, 0, 12)
			overrideCalls := 0
			ops := adaptiveBenchmarkLifecycleOpsFixture(&events)
			ops.authorizeQwen = func(
				_ context.Context,
				_ adaptiveBenchmarkPool,
				ownerID uuid.UUID,
			) (bool, error) {
				events = append(events, "authorize")
				if ownerID != request.OwnerID {
					t.Fatalf("owner=%s", ownerID)
				}
				return testCase.allowed, testCase.err
			}
			ops.beginOverride = func(
				context.Context,
				adaptiveBenchmarkPool,
				settingsBenchmarkOverrideRequest,
			) (adaptiveBenchmarkOverride, error) {
				overrideCalls++
				events = append(events, "begin-override")
				return &adaptiveBenchmarkOverrideFake{
					events: &events,
				}, nil
			}

			execution, err := newAdaptiveBenchmarkRunExecutionWithOps(
				t.Context(),
				request,
				ops,
			)
			if testCase.allowed {
				if err != nil || execution == nil || overrideCalls != 1 {
					t.Fatalf(
						"execution=%v error=%v override_calls=%d",
						execution,
						err,
						overrideCalls,
					)
				}
				if closeErr := execution.Close(); closeErr != nil {
					t.Fatalf("Close: %v", closeErr)
				}
				return
			}
			if execution != nil ||
				err == nil ||
				overrideCalls != 0 ||
				!slices.Equal(
					events,
					[]string{
						"preflight",
						"open-control",
						"authorize",
						"close-control",
					},
				) {
				t.Fatalf(
					"execution=%v error=%v override_calls=%d events=%v",
					execution,
					err,
					overrideCalls,
					events,
				)
			}
			if testCase.err != nil &&
				!errors.Is(err, testCase.err) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestAdaptiveBenchmarkLifecycleDoesNotFinalizeAfterRestoreFailure(
	t *testing.T,
) {
	t.Parallel()
	request := validAdaptiveBenchmarkRunRequest(t, "qwen-portability")
	restoreErr := errors.New("restore failed")
	events := make([]string, 0, 12)
	environment := &adaptiveBenchmarkEnvironmentFake{
		events:  &events,
		payload: []byte(`{"must_not_finalize":true}`),
	}
	ops := adaptiveBenchmarkLifecycleOpsFixture(&events)
	ops.beginOverride = func(
		context.Context,
		adaptiveBenchmarkPool,
		settingsBenchmarkOverrideRequest,
	) (adaptiveBenchmarkOverride, error) {
		events = append(events, "begin-override")
		return &adaptiveBenchmarkOverrideFake{
			events:     &events,
			restoreErr: restoreErr,
		}, nil
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
		t.Fatal(err)
	}
	if err := execution.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := execution.Close(); !errors.Is(err, restoreErr) {
		t.Fatalf("Close error=%v", err)
	}
	if payload, err := execution.Finalize(t.Context()); err == nil ||
		payload != nil ||
		environment.finalizeCalls != 0 {
		t.Fatalf(
			"payload=%q error=%v finalize_calls=%d",
			payload,
			err,
			environment.finalizeCalls,
		)
	}
}
