package main

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

func TestAdaptiveBenchmarkControlDriverExecutesExactMatrix(t *testing.T) {
	runID := uuid.MustParse("018f0000-0000-7000-8000-000000000001")
	start := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	var tick atomic.Int64
	calls := make([]string, 0, len(auraeval.AdaptiveBenchmarkControlMatrix()))
	var callsMu sync.Mutex
	executors := make(map[string]adaptiveBenchmarkControlExecutor)
	for _, controlID := range auraeval.AdaptiveBenchmarkControlMatrix() {
		executors[controlID] = func(
			_ context.Context,
			control auraeval.AdaptiveBenchmarkControlCase,
		) (adaptiveBenchmarkControlExecution, error) {
			callsMu.Lock()
			calls = append(calls, control.ControlID)
			callsMu.Unlock()
			return adaptiveBenchmarkControlExecution{
				EvidenceIDs: []uuid.UUID{
					uuid.MustParse(
						"018f0000-0000-7000-8000-000000000002",
					),
				},
				ObservedFailureReasonCodes: adaptiveBenchmarkExpectedControlFaults(controlID),
			}, nil
		}
	}
	driver, err := newAdaptiveBenchmarkControlDriverWithOps(
		runID,
		adaptiveBenchmarkControlDriverOps{
			Now: func() time.Time {
				return start.Add(
					time.Duration(tick.Add(1)) * time.Millisecond,
				)
			},
			Timeout: time.Second, Executors: executors,
		},
	)
	if err != nil {
		t.Fatalf("newAdaptiveBenchmarkControlDriverWithOps: %v", err)
	}
	for _, controlID := range auraeval.AdaptiveBenchmarkControlMatrix() {
		result, err := driver.RunControl(
			t.Context(),
			auraeval.AdaptiveBenchmarkControlCase{ControlID: controlID},
		)
		if err != nil {
			t.Fatalf("RunControl(%s): %v", controlID, err)
		}
		if result.Status != auraeval.AdaptiveBenchmarkStatusPass ||
			len(result.EvidenceIDs) != 1 ||
			!slices.Equal(
				result.ObservedFailureReasonCodes,
				adaptiveBenchmarkExpectedControlFaults(controlID),
			) {
			t.Fatalf("RunControl(%s) = %#v", controlID, result)
		}
	}
	if !slices.Equal(calls, auraeval.AdaptiveBenchmarkControlMatrix()) {
		t.Fatalf("control calls = %v", calls)
	}
}

func TestAdaptiveBenchmarkControlDriverBoundsAndClassifiesFailure(
	t *testing.T,
) {
	runID := uuid.MustParse("018f0000-0000-7000-8000-000000000010")
	injected := errors.New("injected control failure")
	executors := adaptiveBenchmarkControlExecutorMap(func(
		_ context.Context,
		control auraeval.AdaptiveBenchmarkControlCase,
	) (adaptiveBenchmarkControlExecution, error) {
		return adaptiveBenchmarkControlExecution{
			EvidenceIDs:                []uuid.UUID{runID},
			ObservedFailureReasonCodes: adaptiveBenchmarkExpectedControlFaults(control.ControlID),
		}, injected
	})
	driver, err := newAdaptiveBenchmarkControlDriverWithOps(
		runID,
		adaptiveBenchmarkControlDriverOps{
			Now: time.Now, Timeout: time.Second, Executors: executors,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := driver.RunControl(
		t.Context(),
		auraeval.AdaptiveBenchmarkControlCase{
			ControlID: "assignment_write_failure",
		},
	)
	if !errors.Is(err, injected) ||
		result.Status != auraeval.AdaptiveBenchmarkStatusInvalid ||
		!slices.Equal(
			result.ReasonCodes,
			[]string{auraeval.AdaptiveBenchmarkReasonControlFailed},
		) ||
		!slices.Equal(
			result.ObservedFailureReasonCodes,
			[]string{auraeval.AdaptiveBenchmarkReasonAssignmentMissing},
		) {
		t.Fatalf("failure result=%#v err=%v", result, err)
	}
}

func TestAdaptiveBenchmarkControlDriverEmitsNonNilEmptyReasonCodes(
	t *testing.T,
) {
	runID := uuid.MustParse("018f0000-0000-7000-8000-000000000012")
	driver, err := newAdaptiveBenchmarkControlDriverWithOps(
		runID,
		adaptiveBenchmarkControlDriverOps{
			Now: time.Now, Timeout: time.Second,
			Executors: adaptiveBenchmarkControlExecutorMap(func(
				context.Context,
				auraeval.AdaptiveBenchmarkControlCase,
			) (adaptiveBenchmarkControlExecution, error) {
				return adaptiveBenchmarkControlExecution{}, nil
			}),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := driver.RunControl(
		t.Context(),
		auraeval.AdaptiveBenchmarkControlCase{
			ControlID: "negative_control",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.ReasonCodes == nil || len(result.ReasonCodes) != 0 {
		t.Fatalf("ReasonCodes = %#v, want non-nil empty", result.ReasonCodes)
	}
}

func TestAdaptiveBenchmarkControlDriverRejectsUnknownControlAndNilReceiver(
	t *testing.T,
) {
	var missing *adaptiveBenchmarkControlDriver
	if _, err := missing.RunControl(
		t.Context(),
		auraeval.AdaptiveBenchmarkControlCase{
			ControlID: "negative_control",
		},
	); err == nil {
		t.Fatal("nil driver RunControl error = nil")
	}
	driver, err := newAdaptiveBenchmarkControlDriverWithOps(
		uuid.MustParse("018f0000-0000-7000-8000-000000000011"),
		adaptiveBenchmarkControlDriverOps{
			Now: time.Now, Timeout: time.Second,
			Executors: adaptiveBenchmarkControlExecutorMap(func(
				context.Context,
				auraeval.AdaptiveBenchmarkControlCase,
			) (adaptiveBenchmarkControlExecution, error) {
				return adaptiveBenchmarkControlExecution{}, nil
			}),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := driver.RunControl(
		t.Context(),
		auraeval.AdaptiveBenchmarkControlCase{ControlID: "unknown"},
	); err == nil {
		t.Fatal("unknown control RunControl error = nil")
	}
}

func TestAdaptiveBenchmarkControlDriverRejectsIncompleteDependencies(
	t *testing.T,
) {
	runID := uuid.MustParse("018f0000-0000-7000-8000-000000000020")
	valid := adaptiveBenchmarkControlDriverOps{
		Now:     time.Now,
		Timeout: time.Second,
		Executors: adaptiveBenchmarkControlExecutorMap(func(
			context.Context,
			auraeval.AdaptiveBenchmarkControlCase,
		) (adaptiveBenchmarkControlExecution, error) {
			return adaptiveBenchmarkControlExecution{}, nil
		}),
	}
	tests := []struct {
		name string
		edit func(*adaptiveBenchmarkControlDriverOps)
	}{
		{name: "clock", edit: func(ops *adaptiveBenchmarkControlDriverOps) {
			ops.Now = nil
		}},
		{name: "timeout", edit: func(ops *adaptiveBenchmarkControlDriverOps) {
			ops.Timeout = 0
		}},
		{name: "executor", edit: func(ops *adaptiveBenchmarkControlDriverOps) {
			delete(ops.Executors, "negative_control")
		}},
		{name: "nil executor", edit: func(ops *adaptiveBenchmarkControlDriverOps) {
			ops.Executors["negative_control"] = nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ops := valid
			ops.Executors = cloneAdaptiveBenchmarkControlExecutors(
				valid.Executors,
			)
			test.edit(&ops)
			if _, err := newAdaptiveBenchmarkControlDriverWithOps(
				runID,
				ops,
			); err == nil {
				t.Fatal("constructor accepted incomplete dependencies")
			}
		})
	}
	if _, err := newAdaptiveBenchmarkControlDriverWithOps(
		uuid.Nil,
		valid,
	); err == nil {
		t.Fatal("constructor accepted nil run ID")
	}
}

func adaptiveBenchmarkControlExecutorMap(
	executor adaptiveBenchmarkControlExecutor,
) map[string]adaptiveBenchmarkControlExecutor {
	executors := make(map[string]adaptiveBenchmarkControlExecutor)
	for _, controlID := range auraeval.AdaptiveBenchmarkControlMatrix() {
		executors[controlID] = executor
	}
	return executors
}

func cloneAdaptiveBenchmarkControlExecutors(
	source map[string]adaptiveBenchmarkControlExecutor,
) map[string]adaptiveBenchmarkControlExecutor {
	cloned := make(map[string]adaptiveBenchmarkControlExecutor, len(source))
	maps.Copy(cloned, source)
	return cloned
}
