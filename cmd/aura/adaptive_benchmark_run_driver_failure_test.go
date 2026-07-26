package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

func TestAdaptiveBenchmarkAuraDriverRecoversCommittedFactsAfterTurnFailure(
	t *testing.T,
) {
	tests := []struct {
		name       string
		turnErr    error
		wantReason string
	}{
		{
			name:       "transport",
			turnErr:    &openai_compat.HTTPError{StatusCode: 502},
			wantReason: auraeval.AdaptiveBenchmarkReasonModelTransportFailure,
		},
		{
			name:       "parser",
			turnErr:    &json.SyntaxError{Offset: 1},
			wantReason: auraeval.AdaptiveBenchmarkReasonModelParserFailure,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			driver, runner, _, outcomes, assignment, delivery :=
				adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
			deliveryEvent, err := adaptive.NewDeliveryEvent(
				assignment,
				delivery,
			)
			if err != nil {
				t.Fatal(err)
			}
			runner.items = []adaptiveBenchmarkTurnItem{{err: test.turnErr}}

			result, err := driver.RunScenario(
				t.Context(),
				auraeval.AdaptiveBenchmarkSubject{
					ScenarioID: "r01", Domain: adaptive.DomainReasoning,
					Prompt: "prompt",
				},
			)
			if err == nil ||
				!errors.Is(err, errAdaptiveBenchmarkScenarioFailure) ||
				result.RequestID != assignment.RequestID.String() ||
				result.AssignmentID != assignment.AssignmentID.String() ||
				result.DeliveryEventID != deliveryEvent.ID.String() ||
				result.ChampionActionID != adaptive.StaticActionID ||
				result.RecommendedActionID != "candidate" ||
				result.IntendedActionID != adaptive.StaticActionID ||
				result.ActualActionID != adaptive.StaticActionID ||
				result.LatencyNS != int64(25*time.Millisecond) ||
				!slices.Equal(
					result.FailureReasonCodes,
					[]string{test.wantReason},
				) {
				t.Fatalf("recovered failure result=%#v error=%v", result, err)
			}
			if len(result.ActionProbabilities) != 2 ||
				result.ActionProbabilities[0].ActionID != "candidate" ||
				result.ActionProbabilities[0].Probability != 0 ||
				result.ActionProbabilities[1].ActionID !=
					adaptive.StaticActionID ||
				result.ActionProbabilities[1].Probability != 1 {
				t.Fatalf(
					"recovered probabilities = %#v",
					result.ActionProbabilities,
				)
			}
			if len(outcomes.observations) != 0 {
				t.Fatal("turn failure fabricated an outcome")
			}
			driver.mu.Lock()
			pending := len(driver.pending)
			driver.mu.Unlock()
			if pending != 0 {
				t.Fatal("turn failure became outcome-eligible")
			}
		})
	}
}

func TestAdaptiveBenchmarkAuraDriverRecoversCommittedFactsFromParserExits(
	t *testing.T,
) {
	tests := []struct {
		name        string
		items       func(adaptive.Assignment) []adaptiveBenchmarkTurnItem
		wantLatency time.Duration
	}{
		{
			name: "duplicate terminal",
			items: func(
				assignment adaptive.Assignment,
			) []adaptiveBenchmarkTurnItem {
				return []adaptiveBenchmarkTurnItem{
					{event: adaptiveBenchmarkTerminalEvent(
						assignment.RequestID,
						"first",
					)},
					{event: adaptiveBenchmarkTerminalEvent(
						assignment.RequestID,
						"duplicate",
					)},
				}
			},
			wantLatency: 30 * time.Millisecond,
		},
		{
			name: "clean end without terminal",
			items: func(
				adaptive.Assignment,
			) []adaptiveBenchmarkTurnItem {
				return []adaptiveBenchmarkTurnItem{}
			},
			wantLatency: 25 * time.Millisecond,
		},
		{
			name: "invalid terminal usage",
			items: func(
				assignment adaptive.Assignment,
			) []adaptiveBenchmarkTurnItem {
				terminal := adaptiveBenchmarkTerminalEvent(
					assignment.RequestID,
					"answer",
				)
				terminal.Actions.StateDelta["cost_usd"] = math.NaN()
				return []adaptiveBenchmarkTurnItem{{event: terminal}}
			},
			wantLatency: 25 * time.Millisecond,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			driver, runner, _, outcomes, assignment, delivery :=
				adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
			deliveryEvent, err := adaptive.NewDeliveryEvent(
				assignment,
				delivery,
			)
			if err != nil {
				t.Fatal(err)
			}
			runner.items = test.items(assignment)

			result, err := driver.RunScenario(
				t.Context(),
				auraeval.AdaptiveBenchmarkSubject{
					ScenarioID: "r01", Domain: adaptive.DomainReasoning,
					Prompt: "prompt",
				},
			)
			if err == nil ||
				!errors.Is(err, errAdaptiveBenchmarkScenarioFailure) ||
				result.RequestID != assignment.RequestID.String() ||
				result.AssignmentID != assignment.AssignmentID.String() ||
				result.DeliveryEventID != deliveryEvent.ID.String() ||
				result.ChampionActionID != adaptive.StaticActionID ||
				result.ActualActionID != adaptive.StaticActionID ||
				result.LatencyNS != test.wantLatency.Nanoseconds() ||
				!slices.Equal(
					result.FailureReasonCodes,
					[]string{
						auraeval.AdaptiveBenchmarkReasonModelParserFailure,
					},
				) {
				t.Fatalf(
					"%s result=%#v error=%v",
					test.name,
					result,
					err,
				)
			}
			if len(outcomes.observations) != 0 {
				t.Fatal("parser failure fabricated an outcome")
			}
			driver.mu.Lock()
			pending := len(driver.pending)
			driver.mu.Unlock()
			if pending != 0 {
				t.Fatal("parser failure became outcome-eligible")
			}
		})
	}
}

func TestAdaptiveBenchmarkAuraDriverRejectsNonMonotonicFailureLatency(
	t *testing.T,
) {
	driver, runner, _, _, assignment, delivery :=
		adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
	fixed := time.Date(2026, 7, 26, 19, 0, 0, 0, time.UTC)
	driver.now = func() time.Time { return fixed }
	deliveryEvent, err := adaptive.NewDeliveryEvent(assignment, delivery)
	if err != nil {
		t.Fatal(err)
	}
	runner.items = []adaptiveBenchmarkTurnItem{{
		err: &openai_compat.HTTPError{StatusCode: 502},
	}}

	result, err := driver.RunScenario(
		t.Context(),
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: "r01", Domain: adaptive.DomainReasoning,
			Prompt: "prompt",
		},
	)
	if err == nil ||
		result.RequestID != assignment.RequestID.String() ||
		result.AssignmentID != assignment.AssignmentID.String() ||
		result.DeliveryEventID != deliveryEvent.ID.String() ||
		result.LatencyNS != 0 ||
		!slices.Equal(
			result.FailureReasonCodes,
			[]string{auraeval.AdaptiveBenchmarkReasonProvenanceMissing},
		) {
		t.Fatalf("non-monotonic failure result=%#v error=%v", result, err)
	}
}

func TestAdaptiveBenchmarkAuraDriverDoesNotInventMissingFailureFacts(
	t *testing.T,
) {
	driver, runner, _, _, assignment, _ :=
		adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
	runner.beforeTurn = nil
	runner.items = []adaptiveBenchmarkTurnItem{
		{event: &agent.Event{RequestID: assignment.RequestID}},
		{err: &openai_compat.HTTPError{StatusCode: 503}},
	}

	result, err := driver.RunScenario(
		t.Context(),
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: "r01", Domain: adaptive.DomainReasoning,
			Prompt: "prompt",
		},
	)
	if err == nil ||
		result.RequestID != "" ||
		result.AssignmentID != "" ||
		result.DeliveryEventID != "" ||
		result.LatencyNS != 0 ||
		!slices.Equal(
			result.FailureReasonCodes,
			[]string{auraeval.AdaptiveBenchmarkReasonModelTransportFailure},
		) {
		t.Fatalf("missing facts result=%#v error=%v", result, err)
	}
}

func TestAdaptiveBenchmarkAuraDriverDoesNotConvertCancellationWithFacts(
	t *testing.T,
) {
	driver, runner, _, _, assignment, _ :=
		adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
	runner.items = []adaptiveBenchmarkTurnItem{
		{event: &agent.Event{RequestID: assignment.RequestID}},
		{err: context.Canceled},
	}

	result, err := driver.RunScenario(
		t.Context(),
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: "r01", Domain: adaptive.DomainReasoning,
			Prompt: "prompt",
		},
	)
	if !errors.Is(err, context.Canceled) ||
		result.RequestID != "" ||
		result.AssignmentID != "" ||
		result.DeliveryEventID != "" ||
		result.LatencyNS != 0 ||
		len(result.FailureReasonCodes) != 0 {
		t.Fatalf("cancellation result=%#v error=%v", result, err)
	}
}

func TestAdaptiveBenchmarkAuraDriverDoesNotConvertUnsafeFailureWithFacts(
	t *testing.T,
) {
	driver, runner, _, _, assignment, _ :=
		adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
	runner.items = []adaptiveBenchmarkTurnItem{
		{event: &agent.Event{RequestID: assignment.RequestID}},
		{err: auraeval.ErrAdaptiveBenchmarkUnsafeDispatch},
	}

	result, err := driver.RunScenario(
		t.Context(),
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: "r01", Domain: adaptive.DomainReasoning,
			Prompt: "prompt",
		},
	)
	if !errors.Is(err, auraeval.ErrAdaptiveBenchmarkUnsafeDispatch) ||
		result.RequestID != "" ||
		result.AssignmentID != "" ||
		result.DeliveryEventID != "" ||
		result.LatencyNS != 0 ||
		len(result.FailureReasonCodes) != 0 {
		t.Fatalf("unsafe result=%#v error=%v", result, err)
	}
}
