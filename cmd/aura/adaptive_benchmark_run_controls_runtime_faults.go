package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"sync"

	"github.com/chetto1983/aura/internal/adaptive"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/llm"
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
	staticRunner, err := adaptiveBenchmarkStaticRunner(
		runtime.environment.env,
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	staticSubject := subject
	staticSubject.runner = staticRunner
	return executeAdaptiveBenchmarkRecorderWriteFailure(
		ctx,
		staticSubject,
		subject,
		controlID,
		failingKind,
		injected,
	)
}

func executeAdaptiveBenchmarkRecorderWriteFailure(
	ctx context.Context,
	staticSubject adaptiveBenchmarkControlSubject,
	subject adaptiveBenchmarkControlSubject,
	controlID string,
	failingKind adaptive.EventKind,
	injected error,
) (adaptiveBenchmarkControlExecution, error) {
	scenario, err := adaptiveBenchmarkControlScenario("r01")
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	message, err := adaptiveBenchmarkSubjectJSON(
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: scenario.ScenarioID,
			Domain:     scenario.Domain,
			Prompt:     scenario.Prompt,
		},
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	baseline, baselineRequests, err :=
		adaptiveBenchmarkRecorderFailureTurn(
			ctx,
			staticSubject,
			message,
		)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	trace := newAdaptiveBenchmarkControlProbeTrace()
	var attemptedMu sync.Mutex
	attempted := []adaptiveBenchmarkRecorderProbeFact{}
	handle, err := subject.recorder.InstallProbe(
		adaptiveBenchmarkRecorderProbe{
			BeforeWrite: func(
				_ context.Context,
				fact adaptiveBenchmarkRecorderProbeFact,
			) error {
				if fact.Domain == adaptive.DomainReasoning &&
					fact.Kind == failingKind {
					attemptedMu.Lock()
					attempted = append(attempted, fact)
					attemptedMu.Unlock()
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
	turn, faultedRequests, err := adaptiveBenchmarkRecorderFailureTurn(
		ctx,
		subject,
		message,
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	if err := adaptiveBenchmarkValidateRecorderFailureFallback(
		baseline,
		baselineRequests,
		turn,
		faultedRequests,
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	attemptedMu.Lock()
	attempts := slices.Clone(attempted)
	attemptedMu.Unlock()
	if len(attempts) == 0 {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark write failure was not contained",
			)
	}
	trace.mu.Lock()
	assignments := slices.Clone(trace.assignments)
	deliveries := slices.Clone(trace.deliveries)
	trace.mu.Unlock()
	if len(deliveries) != 0 ||
		(failingKind == adaptive.EventDecision && len(assignments) != 0) ||
		(failingKind == adaptive.EventDelivery && len(assignments) == 0) {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark write failure persisted partial exposure",
			)
	}
	assignmentIDs := make(map[uuid.UUID]struct{}, len(assignments))
	for _, assignment := range assignments {
		assignmentIDs[assignment.AssignmentID] = struct{}{}
	}
	attemptedAssignmentIDs := make(
		map[uuid.UUID]struct{},
		len(attempts),
	)
	evidence := []uuid.UUID{turn.RequestID}
	for _, attempt := range attempts {
		if attempt.Kind != failingKind ||
			attempt.RequestID == uuid.Nil ||
			attempt.AssignmentID == uuid.Nil ||
			attempt.RequestID != turn.RequestID {
			return adaptiveBenchmarkControlExecution{},
				adaptiveBenchmarkControlError(
					"adaptive benchmark write failure was not contained",
				)
		}
		if failingKind == adaptive.EventDelivery {
			if _, ok := assignmentIDs[attempt.AssignmentID]; !ok {
				return adaptiveBenchmarkControlExecution{},
					adaptiveBenchmarkControlError(
						"adaptive benchmark failed delivery has no assignment",
					)
			}
		}
		attemptedAssignmentIDs[attempt.AssignmentID] = struct{}{}
		evidence = append(evidence, attempt.AssignmentID)
	}
	if failingKind == adaptive.EventDelivery &&
		!reflect.DeepEqual(assignmentIDs, attemptedAssignmentIDs) {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark assignment omitted a failed delivery",
			)
	}
	return adaptiveBenchmarkControlExecution{
		EvidenceIDs:                evidence,
		ObservedFailureReasonCodes: adaptiveBenchmarkExpectedControlFaults(controlID),
	}, nil
}

func adaptiveBenchmarkRecorderFailureTurn(
	ctx context.Context,
	subject adaptiveBenchmarkControlSubject,
	message string,
) (
	adaptiveBenchmarkControlTurn,
	[]llm.Request,
	error,
) {
	if subject.observed == nil {
		return adaptiveBenchmarkControlTurn{}, nil,
			adaptiveBenchmarkControlError(
				"adaptive benchmark model observer is unavailable",
			)
	}
	var requestsMu sync.Mutex
	requests := []llm.Request{}
	clearInterceptor, err := subject.observed.SetStreamInterceptor(
		func(
			_ context.Context,
			request llm.Request,
		) (<-chan llm.Chunk, error) {
			request.SessionID = ""
			requestsMu.Lock()
			requests = append(requests, request)
			requestsMu.Unlock()
			stream := make(chan llm.Chunk, 1)
			stream <- llm.Chunk{
				Text:         "adaptive benchmark fault control",
				FinishReason: "stop",
			}
			close(stream)
			return stream, nil
		},
	)
	if err != nil {
		return adaptiveBenchmarkControlTurn{}, nil, err
	}
	defer clearInterceptor()
	conversationID, err := adaptiveBenchmarkControlCreateConversation(
		ctx,
		subject,
	)
	if err != nil {
		return adaptiveBenchmarkControlTurn{}, nil, err
	}
	turn, runErr := adaptiveBenchmarkControlRunTurn(
		ctx,
		subject,
		conversationID,
		message,
	)
	deleteErr := adaptiveBenchmarkControlDeleteConversation(
		ctx,
		subject,
		conversationID,
	)
	requestsMu.Lock()
	captured := slices.Clone(requests)
	requestsMu.Unlock()
	return turn, captured, errors.Join(runErr, deleteErr)
}

func adaptiveBenchmarkValidateRecorderFailureFallback(
	baseline adaptiveBenchmarkControlTurn,
	baselineRequests []llm.Request,
	faulted adaptiveBenchmarkControlTurn,
	faultedRequests []llm.Request,
) error {
	if baseline.Terminal == nil ||
		baseline.Terminal.LLMResponse == nil ||
		faulted.Terminal == nil ||
		faulted.Terminal.LLMResponse == nil ||
		len(baselineRequests) == 0 ||
		!reflect.DeepEqual(baselineRequests, faultedRequests) ||
		baseline.Terminal.LLMResponse.Content !=
			faulted.Terminal.LLMResponse.Content {
		return adaptiveBenchmarkControlError(
			"adaptive benchmark write failure exposed a non-static request",
		)
	}
	return nil
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
	var clearTransportFailure func()
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
					clear, err :=
						subject.observed.FailTransportUntilCleared(
							errAdaptiveBenchmarkInjectedTransport,
						)
					if err != nil {
						return err
					}
					clearTransportFailure = clear
					armed = true
				}
				return nil
			},
		},
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	defer func() {
		_ = handle.Clear()
		if clearTransportFailure != nil {
			clearTransportFailure()
		}
	}()
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
