package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync/atomic"

	"github.com/chetto1983/aura/internal/adaptive"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

var (
	errAdaptiveBenchmarkInjectedAssignmentWrite = errors.New(
		"adaptive benchmark injected assignment write failure",
	)
	errAdaptiveBenchmarkInjectedDeliveryWrite = errors.New(
		"adaptive benchmark injected delivery write failure",
	)
	errAdaptiveBenchmarkInjectedOutcomeWrite = errors.New(
		"adaptive benchmark injected outcome write failure",
	)
	errAdaptiveBenchmarkInjectedTransport = fmt.Errorf(
		"adaptive benchmark injected model transport failure: %w",
		io.ErrUnexpectedEOF,
	)
)

func (runtime *adaptiveBenchmarkControlRuntime) runAssignmentWriteFailure(
	ctx context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error) {
	return runtime.runRecorderWriteFailure(
		ctx,
		control,
		"assignment_write_failure",
		adaptive.EventDecision,
		errAdaptiveBenchmarkInjectedAssignmentWrite,
	)
}

func (runtime *adaptiveBenchmarkControlRuntime) runDeliveryWriteFailure(
	ctx context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error) {
	return runtime.runRecorderWriteFailure(
		ctx,
		control,
		"delivery_write_failure",
		adaptive.EventDelivery,
		errAdaptiveBenchmarkInjectedDeliveryWrite,
	)
}

func (runtime *adaptiveBenchmarkControlRuntime) runRecorderWriteFailure(
	ctx context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
	controlID string,
	failingKind adaptive.EventKind,
	injected error,
) (adaptiveBenchmarkControlExecution, error) {
	if err := adaptiveBenchmarkUnexpectedControlCase(
		control,
		controlID,
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	subject, err := runtime.primarySubject()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	trace := newAdaptiveBenchmarkControlProbeTrace()
	var attempted adaptiveBenchmarkRecorderProbeFact
	handle, err := subject.recorder.InstallProbe(
		adaptiveBenchmarkRecorderProbe{
			BeforeWrite: func(
				_ context.Context,
				fact adaptiveBenchmarkRecorderProbeFact,
			) error {
				if fact.Domain == adaptive.DomainReasoning &&
					fact.Kind == failingKind {
					attempted = fact
					return injected
				}
				return nil
			},
			AfterCommit: func(
				_ context.Context,
				fact adaptiveBenchmarkRecorderProbeFact,
			) error {
				trace.afterCommit(fact)
				return nil
			},
		},
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	defer func() { _ = handle.Clear() }()
	var modelCalls atomic.Int64
	clearObserver := subject.observed.SetModelStartObserver(
		func(context.Context, string) {
			modelCalls.Add(1)
		},
	)
	defer clearObserver()
	_, _, _, runErr := adaptiveBenchmarkControlRunScenario(
		ctx,
		subject,
		"r01",
		nil,
	)
	if runErr == nil ||
		attempted.Kind != failingKind ||
		attempted.RequestID == uuid.Nil ||
		attempted.AssignmentID == uuid.Nil ||
		modelCalls.Load() != 0 {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark write failure was not contained",
			)
	}
	trace.mu.Lock()
	assignments := slices.Clone(trace.assignments)
	deliveries := slices.Clone(trace.deliveries)
	trace.mu.Unlock()
	if (failingKind == adaptive.EventDecision &&
		(len(assignments) != 0 || len(deliveries) != 0)) ||
		(failingKind == adaptive.EventDelivery &&
			(len(assignments) != 1 || len(deliveries) != 0)) {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark write failure persisted partial exposure",
			)
	}
	return adaptiveBenchmarkControlExecution{
		EvidenceIDs: []uuid.UUID{
			attempted.RequestID,
			attempted.AssignmentID,
		},
		ObservedFailureReasonCodes: adaptiveBenchmarkExpectedControlFaults(controlID),
	}, nil
}

type adaptiveBenchmarkFailingOutcomeWriter struct {
	err error
}

func (writer adaptiveBenchmarkFailingOutcomeWriter) RecordOutcome(
	context.Context,
	adaptive.OutcomeObservation,
) (int64, error) {
	return 0, writer.err
}

func (runtime *adaptiveBenchmarkControlRuntime) runOutcomeWriteFailure(
	ctx context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error) {
	if err := adaptiveBenchmarkUnexpectedControlCase(
		control,
		"outcome_write_failure",
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	subject, err := runtime.primarySubject()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	scenario, result, driver, err :=
		adaptiveBenchmarkControlRunScenario(
			ctx,
			subject,
			"r01",
			adaptiveBenchmarkFailingOutcomeWriter{
				err: errAdaptiveBenchmarkInjectedOutcomeWrite,
			},
		)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	evaluation, err := auraeval.EvaluateAdaptiveBenchmarkScenario(
		scenario,
		result.Observed,
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	_, outcomeErr := driver.RecordEvaluation(
		ctx,
		auraeval.AdaptiveBenchmarkEvaluationRecord{
			ScenarioID: scenario.ScenarioID,
			RequestID:  result.RequestID, AssignmentID: result.AssignmentID,
			DeliveryEventID: result.DeliveryEventID,
			Evaluation:      evaluation,
		},
	)
	if outcomeErr == nil {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark outcome failure was not observed",
			)
	}
	evidence, err := adaptiveBenchmarkControlEvidenceFromResult(result)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	requestID, _ := uuid.Parse(result.RequestID)
	records, err := subject.ledger.ListAggregate(
		ctx,
		subject.ownerID,
		requestID.String(),
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	for _, record := range records {
		if record.Kind == adaptive.EventOutcome {
			return adaptiveBenchmarkControlExecution{},
				adaptiveBenchmarkControlError(
					"adaptive benchmark fabricated an outcome after write failure",
				)
		}
	}
	return adaptiveBenchmarkControlExecution{
		EvidenceIDs:                evidence,
		ObservedFailureReasonCodes: adaptiveBenchmarkExpectedControlFaults(control.ControlID),
	}, nil
}

func (runtime *adaptiveBenchmarkControlRuntime) runModelTransportFailure(
	ctx context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error) {
	if err := adaptiveBenchmarkUnexpectedControlCase(
		control,
		"model_transport_failure",
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	subject, err := runtime.primarySubject()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	armed := false
	handle, err := subject.recorder.InstallProbe(
		adaptiveBenchmarkRecorderProbe{
			BeforeWrite: func(
				context.Context,
				adaptiveBenchmarkRecorderProbeFact,
			) error {
				return nil
			},
			AfterCommit: func(
				_ context.Context,
				fact adaptiveBenchmarkRecorderProbeFact,
			) error {
				if fact.Kind == adaptive.EventDelivery &&
					fact.Domain == adaptive.DomainReasoning &&
					!armed {
					armed = true
					return subject.observed.FailNextTransport(
						errAdaptiveBenchmarkInjectedTransport,
					)
				}
				return nil
			},
		},
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	defer func() { _ = handle.Clear() }()
	_, result, _, runErr := adaptiveBenchmarkControlRunScenario(
		ctx,
		subject,
		"r01",
		nil,
	)
	if runErr == nil ||
		!armed ||
		!slices.Equal(
			result.FailureReasonCodes,
			[]string{auraeval.AdaptiveBenchmarkReasonModelTransportFailure},
		) {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark transport failure linkage was not recovered",
			)
	}
	evidence, err := adaptiveBenchmarkControlEvidenceFromResult(result)
	if err != nil || len(evidence) != 3 {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark transport failure evidence is incomplete",
			)
	}
	return adaptiveBenchmarkControlExecution{
		EvidenceIDs:                evidence,
		ObservedFailureReasonCodes: adaptiveBenchmarkExpectedControlFaults(control.ControlID),
	}, nil
}
