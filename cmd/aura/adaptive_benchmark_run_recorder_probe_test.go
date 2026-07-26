package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/google/uuid"
)

func TestAdaptiveBenchmarkTeeRecorderProbeRunsAroundCommittedCapture(
	t *testing.T,
) {
	ledger := newAdaptiveBenchmarkProbeLedger()
	recorder, err := newAdaptiveBenchmarkTeeRecorder(ledger)
	if err != nil {
		t.Fatal(err)
	}
	assignment := adaptiveBenchmarkTestAssignment(
		t,
		adaptive.DomainReasoning,
	)
	var handle *adaptiveBenchmarkRecorderProbeHandle
	handle, err = recorder.InstallProbe(adaptiveBenchmarkRecorderProbe{
		BeforeWrite: func(
			_ context.Context,
			fact adaptiveBenchmarkRecorderProbeFact,
		) error {
			if fact.Kind != adaptive.EventDecision ||
				fact.Sequence != 0 ||
				ledger.assignmentCount() != 0 {
				t.Fatalf("before-write fact = %#v", fact)
			}
			return nil
		},
		AfterCommit: func(
			_ context.Context,
			fact adaptiveBenchmarkRecorderProbeFact,
		) error {
			if fact.Kind != adaptive.EventDecision ||
				fact.Sequence <= 0 ||
				ledger.assignmentCount() != 1 {
				t.Fatalf("after-commit fact = %#v", fact)
			}
			recorder.mu.Lock()
			captured := recorder.assignments[fact.AssignmentID]
			recorder.mu.Unlock()
			if captured.RequestID != fact.RequestID {
				t.Fatal("after-commit callback ran before capture")
			}
			if err := handle.Clear(); err != nil {
				t.Fatalf("clear probe from callback: %v", err)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := recorder.RecordAssignment(
		t.Context(),
		assignment,
	); err != nil {
		t.Fatalf("RecordAssignment: %v", err)
	}
	second := adaptiveBenchmarkTestAssignment(
		t,
		adaptive.DomainReasoning,
	)
	if _, err := recorder.RecordAssignment(
		t.Context(),
		second,
	); err != nil {
		t.Fatalf("RecordAssignment without probe: %v", err)
	}
}

func TestAdaptiveBenchmarkTeeRecorderProbeInjectsBeforeWriteFailures(
	t *testing.T,
) {
	tests := []struct {
		name string
		kind adaptive.EventKind
	}{
		{name: "assignment", kind: adaptive.EventDecision},
		{name: "delivery", kind: adaptive.EventDelivery},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ledger := newAdaptiveBenchmarkProbeLedger()
			recorder, err := newAdaptiveBenchmarkTeeRecorder(ledger)
			if err != nil {
				t.Fatal(err)
			}
			assignment := adaptiveBenchmarkTestAssignment(
				t,
				adaptive.DomainReasoning,
			)
			delivery := adaptiveBenchmarkTestDelivery(t, assignment)
			injected := errors.New("injected write failure")
			var afterCalls atomic.Int64
			handle, err := recorder.InstallProbe(
				adaptiveBenchmarkRecorderProbe{
					BeforeWrite: func(
						_ context.Context,
						fact adaptiveBenchmarkRecorderProbeFact,
					) error {
						if fact.Kind == test.kind {
							return injected
						}
						return nil
					},
					AfterCommit: func(
						context.Context,
						adaptiveBenchmarkRecorderProbeFact,
					) error {
						afterCalls.Add(1)
						return nil
					},
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := handle.Clear(); err != nil {
					t.Fatalf("Clear: %v", err)
				}
			}()

			if test.kind == adaptive.EventDecision {
				_, err = recorder.RecordAssignment(
					t.Context(),
					assignment,
				)
				if !errors.Is(err, injected) ||
					ledger.assignmentCount() != 0 ||
					afterCalls.Load() != 0 {
					t.Fatalf(
						"assignment fault: count=%d after=%d err=%v",
						ledger.assignmentCount(),
						afterCalls.Load(),
						err,
					)
				}
				return
			}
			if _, err := recorder.RecordAssignment(
				t.Context(),
				assignment,
			); err != nil {
				t.Fatal(err)
			}
			_, err = recorder.RecordDelivery(
				t.Context(),
				assignment.OwnerID,
				assignment.AssignmentID,
				delivery,
			)
			if !errors.Is(err, injected) ||
				ledger.deliveryCount() != 0 ||
				afterCalls.Load() != 1 {
				t.Fatalf(
					"delivery fault: count=%d after=%d err=%v",
					ledger.deliveryCount(),
					afterCalls.Load(),
					err,
				)
			}
		})
	}
}

func TestAdaptiveBenchmarkTeeRecorderProbeInstallAndClearFailClosed(
	t *testing.T,
) {
	ledger := newAdaptiveBenchmarkProbeLedger()
	recorder, err := newAdaptiveBenchmarkTeeRecorder(ledger)
	if err != nil {
		t.Fatal(err)
	}
	valid := adaptiveBenchmarkRecorderProbe{
		BeforeWrite: func(
			context.Context,
			adaptiveBenchmarkRecorderProbeFact,
		) error {
			return nil
		},
		AfterCommit: func(
			context.Context,
			adaptiveBenchmarkRecorderProbeFact,
		) error {
			return nil
		},
	}
	if _, err := recorder.InstallProbe(
		adaptiveBenchmarkRecorderProbe{},
	); err == nil {
		t.Fatal("installed incomplete probe")
	}
	first, err := recorder.InstallProbe(valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.InstallProbe(valid); err == nil {
		t.Fatal("replaced an installed probe")
	}
	if err := first.Clear(); err != nil {
		t.Fatal(err)
	}
	second, err := recorder.InstallProbe(valid)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Clear(); err == nil {
		t.Fatal("stale handle cleared another probe")
	}
	if err := second.Clear(); err != nil {
		t.Fatal(err)
	}
	if err := second.Clear(); err == nil {
		t.Fatal("double clear succeeded")
	}
	var nilHandle *adaptiveBenchmarkRecorderProbeHandle
	if err := nilHandle.Clear(); err == nil {
		t.Fatal("nil handle clear succeeded")
	}
}

func TestAdaptiveBenchmarkTeeRecorderProbeIsConcurrent(
	t *testing.T,
) {
	ledger := newAdaptiveBenchmarkProbeLedger()
	recorder, err := newAdaptiveBenchmarkTeeRecorder(ledger)
	if err != nil {
		t.Fatal(err)
	}
	var beforeCalls atomic.Int64
	var afterCalls atomic.Int64
	handle, err := recorder.InstallProbe(adaptiveBenchmarkRecorderProbe{
		BeforeWrite: func(
			_ context.Context,
			fact adaptiveBenchmarkRecorderProbeFact,
		) error {
			if fact.Sequence != 0 {
				return errors.New("before-write sequence is nonzero")
			}
			beforeCalls.Add(1)
			return nil
		},
		AfterCommit: func(
			_ context.Context,
			fact adaptiveBenchmarkRecorderProbeFact,
		) error {
			if fact.Sequence <= 0 {
				return errors.New("after-commit sequence is not positive")
			}
			afterCalls.Add(1)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := handle.Clear(); err != nil {
			t.Fatalf("Clear: %v", err)
		}
	}()

	const workers = 16
	assignments := make([]adaptive.Assignment, workers)
	deliveries := make([]adaptive.Delivery, workers)
	for index := range workers {
		assignments[index] = adaptiveBenchmarkTestAssignment(
			t,
			adaptive.DomainReasoning,
		)
		deliveries[index] = adaptiveBenchmarkTestDelivery(
			t,
			assignments[index],
		)
	}
	var wait sync.WaitGroup
	errs := make(chan error, workers)
	for index := range workers {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			assignment := assignments[index]
			if _, err := recorder.RecordAssignment(
				t.Context(),
				assignment,
			); err != nil {
				errs <- err
				return
			}
			_, err := recorder.RecordDelivery(
				t.Context(),
				assignment.OwnerID,
				assignment.AssignmentID,
				deliveries[index],
			)
			errs <- err
		}(index)
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if beforeCalls.Load() != workers*2 ||
		afterCalls.Load() != workers*2 {
		t.Fatalf(
			"probe calls before=%d after=%d",
			beforeCalls.Load(),
			afterCalls.Load(),
		)
	}
}

type adaptiveBenchmarkProbeLedger struct {
	mu          sync.Mutex
	assignments map[uuid.UUID]adaptive.Assignment
	records     []adaptive.OutboxRecord
	deliveries  int
}

func newAdaptiveBenchmarkProbeLedger() *adaptiveBenchmarkProbeLedger {
	return &adaptiveBenchmarkProbeLedger{
		assignments: make(map[uuid.UUID]adaptive.Assignment),
	}
}

func (ledger *adaptiveBenchmarkProbeLedger) RecordAssignment(
	_ context.Context,
	assignment adaptive.Assignment,
) (int64, error) {
	event, err := adaptive.NewAssignmentEvent(assignment)
	if err != nil {
		return 0, err
	}
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	ledger.assignments[assignment.AssignmentID] = assignment
	return ledger.appendLocked(event), nil
}

func (ledger *adaptiveBenchmarkProbeLedger) RecordDelivery(
	_ context.Context,
	ownerID uuid.UUID,
	assignmentID uuid.UUID,
	delivery adaptive.Delivery,
) (int64, error) {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	assignment, ok := ledger.assignments[assignmentID]
	if !ok || assignment.OwnerID != ownerID {
		return 0, adaptive.ErrAssignmentNotFound
	}
	event, err := adaptive.NewDeliveryEvent(assignment, delivery)
	if err != nil {
		return 0, err
	}
	ledger.deliveries++
	return ledger.appendLocked(event), nil
}

func (ledger *adaptiveBenchmarkProbeLedger) ListAggregate(
	_ context.Context,
	_ uuid.UUID,
	_ string,
) ([]adaptive.OutboxRecord, error) {
	return nil, nil
}

func (ledger *adaptiveBenchmarkProbeLedger) assignmentCount() int {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	return len(ledger.assignments)
}

func (ledger *adaptiveBenchmarkProbeLedger) deliveryCount() int {
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	return ledger.deliveries
}

func (ledger *adaptiveBenchmarkProbeLedger) appendLocked(
	event adaptive.Event,
) int64 {
	sequence := int64(len(ledger.records) + 1)
	ledger.records = append(ledger.records, adaptive.OutboxRecord{
		ID: event.ID, OwnerID: event.OwnerID,
		AggregateID: event.AggregateID, Sequence: sequence,
		DecisionID: event.DecisionID, Kind: event.Kind,
		Payload: event.Payload, PayloadHash: event.PayloadHash,
		CreatedAt: event.CreatedAt,
	})
	return sequence
}
