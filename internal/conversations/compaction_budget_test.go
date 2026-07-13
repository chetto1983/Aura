package conversations

import (
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestCompactionBudgetExact(t *testing.T) {
	got, err := CalculateCompactionBudget(BudgetInput{WindowTokens: 100_000, ReservedOutputTokens: 10_000, EstimatorErrorTokens: 3_000, RenderedFixedTokens: 2_000, RenderedHistoryTokens: 55_000, PendingInputTokens: 1_000})
	if err != nil {
		t.Fatal(err)
	}
	if got.SafetyMarginTokens != 3_000 || got.InputCapacity != 87_000 || got.ForecastTurnTokens != 5_000 || got.ProjectedInput != 63_000 {
		t.Fatalf("unexpected: %+v", got)
	}
	if got.MinimumSavedTokens != 8_700 {
		t.Fatalf("minimum saved=%d", got.MinimumSavedTokens)
	}
}

func TestCompactionBudgetForecastAndCalibration(t *testing.T) {
	got, err := CalculateCompactionBudget(BudgetInput{WindowTokens: 200_000, ReservedOutputTokens: 10_000, RenderedFixedTokens: 1, ForecastSamples: 199, ObservedP95TurnTokens: 123})
	if err != nil {
		t.Fatal(err)
	}
	if got.ForecastTurnTokens != 8192 {
		t.Fatalf("cold forecast=%d", got.ForecastTurnTokens)
	}
	got, err = CalculateCompactionBudget(BudgetInput{WindowTokens: 200_000, ReservedOutputTokens: 10_000, RenderedFixedTokens: 1, ForecastSamples: 200, ObservedP95TurnTokens: 123, CalibrationSamples: 1000, CalibratedP99Undercount: 250})
	if err != nil {
		t.Fatal(err)
	}
	if got.ForecastTurnTokens != 123 || got.EstimatorErrorTokens != 250 {
		t.Fatalf("calibrated=%+v", got)
	}
}

func TestCompactionBudgetFailsBeforeRatioMath(t *testing.T) {
	for _, in := range []BudgetInput{{WindowTokens: 0}, {WindowTokens: 100, ReservedOutputTokens: 100}, {WindowTokens: 5000, ReservedOutputTokens: 1000, RenderedFixedTokens: 4000}} {
		_, err := CalculateCompactionBudget(in)
		if !errors.Is(err, ErrInsufficientInputCapacity) {
			t.Fatalf("in=%+v err=%v", in, err)
		}
	}
	_, err := CalculateCompactionBudget(BudgetInput{WindowTokens: 5000, ReservedOutputTokens: 1000, RenderedFixedTokens: 3000, PendingInputTokens: 1000, Overflow: true})
	if !errors.Is(err, ErrContextUnavailable) {
		t.Fatalf("overflow err=%v", err)
	}
}

func TestCompactionBudgetRatiosAndSavings(t *testing.T) {
	if err := (BudgetPolicy{TriggerRatio: .5, TargetRatio: .5}).Validate(); !errors.Is(err, ErrInvalidBudgetPolicy) {
		t.Fatalf("err=%v", err)
	}
	b := CompactionBudget{InputCapacity: 100_000, ProjectedInput: 80_000, MinimumSavedTokens: 10_000, TargetRatio: .55}
	if err := b.ValidateActivation(80_000, 60_000); !errors.Is(err, ErrInsufficientReduction) {
		t.Fatalf("target err=%v", err)
	}
	if err := b.ValidateActivation(80_000, 54_000); err != nil {
		t.Fatal(err)
	}
	b.MinimumSavedTokens = 30_000
	if err := b.ValidateActivation(80_000, 54_000); !errors.Is(err, ErrInsufficientSavings) {
		t.Fatalf("savings err=%v", err)
	}
}

func TestProviderBudgetInput(t *testing.T) {
	cap, err := llm.CapabilityFor(llm.Config{Provider: "openrouter", Model: "m", ContextWindow: 100_000, MaxOutputTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CalculateCompactionBudget(BudgetInputFromCapability(cap)); err != nil {
		t.Fatal(err)
	}
}
