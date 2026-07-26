package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	auraeval "github.com/chetto1983/aura/internal/eval"
)

func TestAdaptiveBenchmarkAuraDriverBoundsOutcomeFailures(
	t *testing.T,
) {
	tests := []struct {
		name             string
		writerErr        error
		wantCancellation bool
	}{
		{
			name:      "storage",
			writerErr: errors.New("secret database detail"),
		},
		{
			name:             "cancellation",
			writerErr:        context.Canceled,
			wantCancellation: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			driver, _, _, outcomes, _, _ :=
				adaptiveBenchmarkDriverFixture(t, adaptive.DomainReasoning)
			run, err := driver.RunScenario(
				t.Context(),
				auraeval.AdaptiveBenchmarkSubject{
					ScenarioID: "r01", Domain: adaptive.DomainReasoning,
					Prompt: "prompt",
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			outcomes.err = test.writerErr
			_, err = driver.RecordEvaluation(
				t.Context(),
				auraeval.AdaptiveBenchmarkEvaluationRecord{
					ScenarioID: "r01", RequestID: run.RequestID,
					AssignmentID:    run.AssignmentID,
					DeliveryEventID: run.DeliveryEventID,
					Evaluation: auraeval.AdaptiveBenchmarkEvaluation{
						Passed: true, ReasonCodes: []string{},
					},
				},
			)
			if test.wantCancellation {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation error = %v", err)
				}
				return
			}
			if !errors.Is(err, errAdaptiveBenchmarkScenarioFailure) ||
				errors.Is(err, test.writerErr) {
				t.Fatalf("bounded outcome error = %v", err)
			}
		})
	}
}
