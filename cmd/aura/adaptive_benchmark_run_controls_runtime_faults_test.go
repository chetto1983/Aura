package main

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

func TestExecuteAdaptiveBenchmarkRecorderWriteFailureUsesFailSoftTurn(
	t *testing.T,
) {
	tests := []struct {
		name            string
		controlID       string
		failingKind     adaptive.EventKind
		injected        error
		wantAssignments int
		wantDeliveries  int
	}{
		{
			name: "assignment", controlID: "assignment_write_failure",
			failingKind: adaptive.EventDecision,
			injected:    errAdaptiveBenchmarkInjectedAssignmentWrite,
		},
		{
			name: "delivery", controlID: "delivery_write_failure",
			failingKind:     adaptive.EventDelivery,
			injected:        errAdaptiveBenchmarkInjectedDeliveryWrite,
			wantAssignments: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ownerID := uuid.Must(uuid.NewV7())
			requestID := uuid.Must(uuid.NewV7())
			assignment := adaptiveBenchmarkTestAssignmentFor(
				t,
				ownerID,
				requestID,
				adaptive.DomainReasoning,
			)
			delivery := adaptiveBenchmarkTestDelivery(t, assignment)
			ledger := newAdaptiveBenchmarkLedgerFake()
			recorder, err := newAdaptiveBenchmarkTeeRecorder(ledger)
			if err != nil {
				t.Fatal(err)
			}
			delegate := &adaptiveBenchmarkLLMClientFake{
				stream: make(chan llm.Chunk),
			}
			observed, err := newAdaptiveBenchmarkObservedClient(delegate)
			if err != nil {
				t.Fatal(err)
			}
			runner := &adaptiveBenchmarkRunnerFake{
				items: []adaptiveBenchmarkTurnItem{{
					event: adaptiveBenchmarkTerminalEvent(requestID, "fallback"),
				}},
				beforeTurn: func(ctx context.Context) error {
					_, err := adaptive.DecideAndDeliver(
						ctx,
						assignment,
						func(ctx context.Context, _ adaptive.Event) error {
							_, err := recorder.RecordAssignment(ctx, assignment)
							return err
						},
						func(
							context.Context,
							adaptive.Assignment,
						) (adaptive.Exposure[struct{}], error) {
							return adaptive.Exposure[struct{}]{
								Delivery: delivery,
							}, nil
						},
						func(ctx context.Context, _ adaptive.Event) error {
							_, err := recorder.RecordDelivery(
								ctx,
								assignment.OwnerID,
								assignment.AssignmentID,
								delivery,
							)
							return err
						},
						func(context.Context) (struct{}, error) {
							return struct{}{}, nil
						},
					)
					if err != nil {
						return err
					}
					_, err = observed.Stream(
						tools.WithRequestID(ctx, requestID.String()),
						llm.Request{
							Model:     "static",
							SessionID: "fault-conversation",
						},
					)
					return err
				},
				deleteAffected: 1,
			}
			staticRunner := &adaptiveBenchmarkRunnerFake{
				items: []adaptiveBenchmarkTurnItem{{
					event: adaptiveBenchmarkTerminalEvent(requestID, "fallback"),
				}},
				beforeTurn: func(ctx context.Context) error {
					_, err := observed.Stream(
						tools.WithRequestID(ctx, requestID.String()),
						llm.Request{
							Model:     "static",
							SessionID: "baseline-conversation",
						},
					)
					return err
				},
				deleteAffected: 1,
			}
			subject := adaptiveBenchmarkControlSubject{
				runner: runner, recorder: recorder, observed: observed,
				ownerID: ownerID, runID: uuid.Must(uuid.NewV7()),
			}
			staticSubject := subject
			staticSubject.runner = staticRunner

			execution, err := executeAdaptiveBenchmarkRecorderWriteFailure(
				t.Context(),
				staticSubject,
				subject,
				test.controlID,
				test.failingKind,
				test.injected,
			)
			if err != nil {
				t.Fatalf("execute recorder write failure: %v", err)
			}
			if !slices.Equal(
				execution.EvidenceIDs,
				[]uuid.UUID{requestID, assignment.AssignmentID},
			) {
				t.Fatalf("evidence IDs=%v", execution.EvidenceIDs)
			}
			if !slices.Equal(
				execution.ObservedFailureReasonCodes,
				adaptiveBenchmarkExpectedControlFaults(test.controlID),
			) {
				t.Fatalf(
					"observed faults=%v",
					execution.ObservedFailureReasonCodes,
				)
			}
			assignments, deliveries := adaptiveBenchmarkFactCounts(
				ledger.records,
			)
			if assignments != test.wantAssignments ||
				deliveries != test.wantDeliveries {
				t.Fatalf(
					"committed facts=%d/%d, want %d/%d",
					assignments,
					deliveries,
					test.wantAssignments,
					test.wantDeliveries,
				)
			}
			if delegate.calls != 0 {
				t.Fatalf("uncontrolled model calls=%d, want 0", delegate.calls)
			}
			if runner.createdID == "" ||
				runner.deletedID != runner.createdID {
				t.Fatalf(
					"conversation lifecycle created=%q deleted=%q",
					runner.createdID,
					runner.deletedID,
				)
			}
			if staticRunner.createdID == "" ||
				staticRunner.deletedID != staticRunner.createdID {
				t.Fatalf(
					"static conversation lifecycle created=%q deleted=%q",
					staticRunner.createdID,
					staticRunner.deletedID,
				)
			}
		})
	}
}

func TestExecuteAdaptiveBenchmarkRecorderWriteFailureAcceptsEveryFailedDelivery(
	t *testing.T,
) {
	ownerID := uuid.Must(uuid.NewV7())
	requestID := uuid.Must(uuid.NewV7())
	first := adaptiveBenchmarkTestAssignmentFor(
		t,
		ownerID,
		requestID,
		adaptive.DomainReasoning,
	)
	second := first
	second.PointOrdinal++
	var err error
	second.AssignmentID, err = adaptive.AssignmentIDForPoint(
		second.OwnerID,
		second.RequestID,
		second.Point,
		second.PointOrdinal,
	)
	if err != nil {
		t.Fatal(err)
	}
	assignments := []adaptive.Assignment{first, second}
	ledger := newAdaptiveBenchmarkLedgerFake()
	recorder, err := newAdaptiveBenchmarkTeeRecorder(ledger)
	if err != nil {
		t.Fatal(err)
	}
	delegate := &adaptiveBenchmarkLLMClientFake{
		stream: make(chan llm.Chunk),
	}
	observed, err := newAdaptiveBenchmarkObservedClient(delegate)
	if err != nil {
		t.Fatal(err)
	}
	runner := &adaptiveBenchmarkRunnerFake{
		items: []adaptiveBenchmarkTurnItem{{
			event: adaptiveBenchmarkTerminalEvent(requestID, "fallback"),
		}},
		beforeTurn: func(ctx context.Context) error {
			for _, assignment := range assignments {
				delivery := adaptiveBenchmarkTestDelivery(t, assignment)
				if _, err := adaptive.DecideAndDeliver(
					ctx,
					assignment,
					func(ctx context.Context, _ adaptive.Event) error {
						_, err := recorder.RecordAssignment(ctx, assignment)
						return err
					},
					func(
						context.Context,
						adaptive.Assignment,
					) (adaptive.Exposure[struct{}], error) {
						return adaptive.Exposure[struct{}]{
							Delivery: delivery,
						}, nil
					},
					func(ctx context.Context, _ adaptive.Event) error {
						_, err := recorder.RecordDelivery(
							ctx,
							assignment.OwnerID,
							assignment.AssignmentID,
							delivery,
						)
						return err
					},
					func(context.Context) (struct{}, error) {
						return struct{}{}, nil
					},
				); err != nil {
					return err
				}
				if _, err := observed.Stream(
					tools.WithRequestID(ctx, requestID.String()),
					llm.Request{
						Model:     "static",
						SessionID: "fault-conversation",
					},
				); err != nil {
					return err
				}
			}
			return nil
		},
		deleteAffected: 1,
	}
	staticRunner := &adaptiveBenchmarkRunnerFake{
		items: []adaptiveBenchmarkTurnItem{{
			event: adaptiveBenchmarkTerminalEvent(requestID, "fallback"),
		}},
		beforeTurn: func(ctx context.Context) error {
			for range assignments {
				if _, err := observed.Stream(
					tools.WithRequestID(ctx, requestID.String()),
					llm.Request{
						Model:     "static",
						SessionID: "baseline-conversation",
					},
				); err != nil {
					return err
				}
			}
			return nil
		},
		deleteAffected: 1,
	}
	subject := adaptiveBenchmarkControlSubject{
		runner: runner, recorder: recorder, observed: observed,
		ownerID: ownerID, runID: uuid.Must(uuid.NewV7()),
	}
	staticSubject := subject
	staticSubject.runner = staticRunner

	if _, err := executeAdaptiveBenchmarkRecorderWriteFailure(
		t.Context(),
		staticSubject,
		subject,
		"delivery_write_failure",
		adaptive.EventDelivery,
		errAdaptiveBenchmarkInjectedDeliveryWrite,
	); err != nil {
		t.Fatalf("execute recorder write failure: %v", err)
	}
	gotAssignments, gotDeliveries := adaptiveBenchmarkFactCounts(ledger.records)
	if gotAssignments != len(assignments) || gotDeliveries != 0 {
		t.Fatalf(
			"committed facts=%d/%d, want %d/0",
			gotAssignments,
			gotDeliveries,
			len(assignments),
		)
	}
}

func TestExecuteAdaptiveBenchmarkRecorderWriteFailureRejectsLearnedRequest(
	t *testing.T,
) {
	ownerID := uuid.Must(uuid.NewV7())
	requestID := uuid.Must(uuid.NewV7())
	assignment := adaptiveBenchmarkTestAssignmentFor(
		t,
		ownerID,
		requestID,
		adaptive.DomainReasoning,
	)
	delivery := adaptiveBenchmarkTestDelivery(t, assignment)
	ledger := newAdaptiveBenchmarkLedgerFake()
	recorder, err := newAdaptiveBenchmarkTeeRecorder(ledger)
	if err != nil {
		t.Fatal(err)
	}
	delegate := &adaptiveBenchmarkLLMClientFake{
		stream: make(chan llm.Chunk),
	}
	observed, err := newAdaptiveBenchmarkObservedClient(delegate)
	if err != nil {
		t.Fatal(err)
	}
	newRunner := func(model string) *adaptiveBenchmarkRunnerFake {
		return &adaptiveBenchmarkRunnerFake{
			items: []adaptiveBenchmarkTurnItem{{
				event: adaptiveBenchmarkTerminalEvent(requestID, "fallback"),
			}},
			beforeTurn: func(ctx context.Context) error {
				if model == "learned" {
					if _, err := adaptive.DecideAndDeliver(
						ctx,
						assignment,
						func(ctx context.Context, _ adaptive.Event) error {
							_, err := recorder.RecordAssignment(ctx, assignment)
							return err
						},
						func(
							context.Context,
							adaptive.Assignment,
						) (adaptive.Exposure[struct{}], error) {
							return adaptive.Exposure[struct{}]{
								Delivery: delivery,
							}, nil
						},
						func(ctx context.Context, _ adaptive.Event) error {
							_, err := recorder.RecordDelivery(
								ctx,
								assignment.OwnerID,
								assignment.AssignmentID,
								delivery,
							)
							return err
						},
						func(context.Context) (struct{}, error) {
							return struct{}{}, nil
						},
					); err != nil {
						return err
					}
				}
				_, err := observed.Stream(
					tools.WithRequestID(ctx, requestID.String()),
					llm.Request{
						Model:     model,
						SessionID: model + "-conversation",
					},
				)
				return err
			},
			deleteAffected: 1,
		}
	}
	subject := adaptiveBenchmarkControlSubject{
		runner: newRunner("learned"), recorder: recorder, observed: observed,
		ownerID: ownerID, runID: uuid.Must(uuid.NewV7()),
	}
	staticSubject := subject
	staticSubject.runner = newRunner("static")

	if _, err := executeAdaptiveBenchmarkRecorderWriteFailure(
		t.Context(),
		staticSubject,
		subject,
		"delivery_write_failure",
		adaptive.EventDelivery,
		errAdaptiveBenchmarkInjectedDeliveryWrite,
	); err == nil {
		t.Fatal("accepted a learned request after the delivery write failed")
	}
}

func TestExecuteAdaptiveBenchmarkRecorderWriteFailureRejectsUnattemptedDelivery(
	t *testing.T,
) {
	ownerID := uuid.Must(uuid.NewV7())
	requestID := uuid.Must(uuid.NewV7())
	first := adaptiveBenchmarkTestAssignmentFor(
		t,
		ownerID,
		requestID,
		adaptive.DomainReasoning,
	)
	second := first
	second.PointOrdinal++
	var err error
	second.AssignmentID, err = adaptive.AssignmentIDForPoint(
		second.OwnerID,
		second.RequestID,
		second.Point,
		second.PointOrdinal,
	)
	if err != nil {
		t.Fatal(err)
	}
	ledger := newAdaptiveBenchmarkLedgerFake()
	recorder, err := newAdaptiveBenchmarkTeeRecorder(ledger)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := newAdaptiveBenchmarkObservedClient(
		&adaptiveBenchmarkLLMClientFake{stream: make(chan llm.Chunk)},
	)
	if err != nil {
		t.Fatal(err)
	}
	streamStatic := func(ctx context.Context) error {
		_, err := observed.Stream(
			tools.WithRequestID(ctx, requestID.String()),
			llm.Request{Model: "static", SessionID: uuid.NewString()},
		)
		return err
	}
	staticRunner := &adaptiveBenchmarkRunnerFake{
		items: []adaptiveBenchmarkTurnItem{{
			event: adaptiveBenchmarkTerminalEvent(requestID, "fallback"),
		}},
		beforeTurn:     streamStatic,
		deleteAffected: 1,
	}
	faultedRunner := &adaptiveBenchmarkRunnerFake{
		items: []adaptiveBenchmarkTurnItem{{
			event: adaptiveBenchmarkTerminalEvent(requestID, "fallback"),
		}},
		beforeTurn: func(ctx context.Context) error {
			if _, err := recorder.RecordAssignment(ctx, first); err != nil {
				return err
			}
			if _, err := recorder.RecordDelivery(
				ctx,
				first.OwnerID,
				first.AssignmentID,
				adaptiveBenchmarkTestDelivery(t, first),
			); !errors.Is(err, errAdaptiveBenchmarkInjectedDeliveryWrite) {
				return errors.New("delivery fault was not injected")
			}
			if _, err := recorder.RecordAssignment(ctx, second); err != nil {
				return err
			}
			return streamStatic(ctx)
		},
		deleteAffected: 1,
	}
	subject := adaptiveBenchmarkControlSubject{
		runner: faultedRunner, recorder: recorder, observed: observed,
		ownerID: ownerID, runID: uuid.Must(uuid.NewV7()),
	}
	staticSubject := subject
	staticSubject.runner = staticRunner

	if _, err := executeAdaptiveBenchmarkRecorderWriteFailure(
		t.Context(),
		staticSubject,
		subject,
		"delivery_write_failure",
		adaptive.EventDelivery,
		errAdaptiveBenchmarkInjectedDeliveryWrite,
	); err == nil {
		t.Fatal("accepted a committed assignment without a delivery attempt")
	}
}

func adaptiveBenchmarkFactCounts(
	records []adaptive.OutboxRecord,
) (assignments int, deliveries int) {
	for _, record := range records {
		switch record.Kind {
		case adaptive.EventDecision:
			assignments++
		case adaptive.EventDelivery:
			deliveries++
		}
	}
	return assignments, deliveries
}
