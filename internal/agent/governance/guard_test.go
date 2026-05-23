package governance_test

import (
	"testing"

	governance "github.com/aura/aura/internal/agent/governance"
)

func TestStopReasonConstantsNonEmpty(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"MaxIterations", governance.StopReasonMaxIterations},
		{"TokenBudget", governance.StopReasonTokenBudget},
		{"WallClock", governance.StopReasonWallClock},
		{"CostBudget", governance.StopReasonCostBudget},
		{"EmptyResponse", governance.StopReasonEmptyResponse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value == "" {
				t.Errorf("StopReason%s is empty string", tc.name)
			}
		})
	}
}

func TestDefaultBudgetCeilings(t *testing.T) {
	if governance.DefaultMaxTokens <= 0 {
		t.Errorf("DefaultMaxTokens = %d, want > 0", governance.DefaultMaxTokens)
	}
	if governance.DefaultMaxCostUSD <= 0 {
		t.Errorf("DefaultMaxCostUSD = %f, want > 0", governance.DefaultMaxCostUSD)
	}
}

func TestStopReasonValuesMatchExpected(t *testing.T) {
	if governance.StopReasonMaxIterations != "max_iterations_hit" {
		t.Errorf("StopReasonMaxIterations = %q, want max_iterations_hit", governance.StopReasonMaxIterations)
	}
	if governance.StopReasonTokenBudget != "token_budget_exceeded" {
		t.Errorf("StopReasonTokenBudget = %q, want token_budget_exceeded", governance.StopReasonTokenBudget)
	}
	if governance.StopReasonWallClock != "wall_clock_exceeded" {
		t.Errorf("StopReasonWallClock = %q, want wall_clock_exceeded", governance.StopReasonWallClock)
	}
	if governance.StopReasonCostBudget != "cost_budget_exceeded" {
		t.Errorf("StopReasonCostBudget = %q, want cost_budget_exceeded", governance.StopReasonCostBudget)
	}
	if governance.StopReasonEmptyResponse != "empty_llm_response" {
		t.Errorf("StopReasonEmptyResponse = %q, want empty_llm_response", governance.StopReasonEmptyResponse)
	}
}
