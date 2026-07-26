package main

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

func TestAdaptiveBenchmarkAuraDriverLinksTerminalAssignmentByDeliverySequence(
	t *testing.T,
) {
	driver, runner, _, outcomes, first, _ :=
		adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
	second := adaptiveBenchmarkTestAssignmentAtOrdinal(t, first, 2)
	adaptiveBenchmarkRecordAssignmentPair(
		t,
		driver,
		runner,
		first,
		second,
	)

	result, err := driver.RunScenario(
		t.Context(),
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: "r01",
			Domain:     adaptive.DomainReasoning,
			Prompt:     "prompt",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.RequestID != first.RequestID.String() ||
		result.AssignmentID != first.AssignmentID.String() {
		t.Fatalf("terminal linkage = %#v, want %s", result, first.AssignmentID)
	}
	if _, err := driver.RecordEvaluation(
		t.Context(),
		auraeval.AdaptiveBenchmarkEvaluationRecord{
			ScenarioID:      "r01",
			RequestID:       result.RequestID,
			AssignmentID:    result.AssignmentID,
			DeliveryEventID: result.DeliveryEventID,
			Evaluation: auraeval.AdaptiveBenchmarkEvaluation{
				Passed: true,
			},
		},
	); err != nil {
		t.Fatal(err)
	}
	if len(outcomes.observations) != 1 ||
		outcomes.observations[0].AssignmentID != first.AssignmentID {
		t.Fatalf("outcome linkage = %#v, want %s", outcomes.observations, first.AssignmentID)
	}
}

func TestAdaptiveBenchmarkAuraDriverRecoversUniqueMultiAssignmentRequest(
	t *testing.T,
) {
	driver, runner, _, _, first, _ :=
		adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
	second := adaptiveBenchmarkTestAssignmentAtOrdinal(t, first, 2)
	adaptiveBenchmarkRecordAssignmentPair(
		t,
		driver,
		runner,
		first,
		second,
	)
	runner.items = []adaptiveBenchmarkTurnItem{{
		err: &openai_compat.HTTPError{StatusCode: 502},
	}}

	result, err := driver.RunScenario(
		t.Context(),
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: "r01",
			Domain:     adaptive.DomainReasoning,
			Prompt:     "prompt",
		},
	)
	if err == nil ||
		!errors.Is(err, errAdaptiveBenchmarkScenarioFailure) ||
		result.RequestID != first.RequestID.String() ||
		result.AssignmentID != first.AssignmentID.String() ||
		!slices.Equal(
			result.FailureReasonCodes,
			[]string{
				auraeval.AdaptiveBenchmarkReasonModelTransportFailure,
			},
		) {
		t.Fatalf("recovered terminal linkage = %#v, error = %v", result, err)
	}
}

func adaptiveBenchmarkRecordAssignmentPair(
	t *testing.T,
	driver *adaptiveBenchmarkAuraDriver,
	runner *adaptiveBenchmarkRunnerFake,
	first adaptive.Assignment,
	second adaptive.Assignment,
) {
	t.Helper()
	runner.beforeTurn = func(ctx context.Context) error {
		for _, assignment := range []adaptive.Assignment{first, second} {
			if _, err := driver.recorder.RecordAssignment(
				ctx,
				assignment,
			); err != nil {
				return err
			}
		}
		for _, assignment := range []adaptive.Assignment{second, first} {
			if _, err := driver.recorder.RecordDelivery(
				ctx,
				assignment.OwnerID,
				assignment.AssignmentID,
				adaptiveBenchmarkTestDelivery(t, assignment),
			); err != nil {
				return err
			}
		}
		return nil
	}
}
