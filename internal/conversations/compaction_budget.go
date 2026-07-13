package conversations

import (
	"errors"
	"math"

	"github.com/chetto1983/aura/internal/llm"
)

var (
	// ErrInsufficientInputCapacity rejects proactive work before ratio math.
	ErrInsufficientInputCapacity = errors.New("insufficient_input_capacity")
	// ErrContextUnavailable rejects overflow that cannot fit fixed plus pending input.
	ErrContextUnavailable = errors.New("context_unavailable")
	// ErrInvalidBudgetPolicy identifies invalid trigger/target ratios.
	ErrInvalidBudgetPolicy = errors.New("invalid_budget_policy")
	// ErrInsufficientSavings rejects candidates below the absolute savings gate.
	ErrInsufficientSavings = errors.New("insufficient_saved_tokens")
	// ErrInsufficientReduction rejects candidates below ratio or target gates.
	ErrInsufficientReduction = errors.New("insufficient_reduction")
)

// BudgetPolicy is the validated proactive trigger and target policy.
type BudgetPolicy struct{ TriggerRatio, TargetRatio float64 }

func (p BudgetPolicy) normalized() BudgetPolicy {
	if p.TriggerRatio == 0 {
		p.TriggerRatio = .80
	}
	if p.TargetRatio == 0 {
		p.TargetRatio = .55
	}
	return p
}

// Validate enforces 0.30 <= target < trigger <= 0.90.
func (p BudgetPolicy) Validate() error {
	p = p.normalized()
	if p.TargetRatio < .30 || p.TargetRatio >= p.TriggerRatio || p.TriggerRatio > .90 {
		return ErrInvalidBudgetPolicy
	}
	return nil
}

// BudgetInput contains pairwise-disjoint rendered token quantities.
type BudgetInput struct {
	WindowTokens, ReservedOutputTokens, EstimatorErrorTokens       int
	CalibrationSamples, CalibratedP99Undercount                    int
	RenderedFixedTokens, RenderedHistoryTokens, PendingInputTokens int
	ForecastSamples, ObservedP95TurnTokens                         int
	Policy                                                         BudgetPolicy
	Overflow                                                       bool
	EstimatorID, EstimatorVersion, CalibrationVersion              string
}

// CompactionBudget is the exact integer-token decision basis.
type CompactionBudget struct {
	WindowTokens, ReservedOutputTokens, SafetyMarginTokens, EstimatorErrorTokens       int
	RenderedFixedTokens, RenderedHistoryTokens, PendingInputTokens, ForecastTurnTokens int
	InputCapacity, ProjectedInput, MinimumSavedTokens                                  int
	TriggerRatio, TargetRatio, MinimumReductionRatio                                   float64
	EstimatorID, EstimatorVersion, CalibrationVersion                                  string
}

// BudgetInputFromCapability seeds budget inputs from a validated adapter row.
func BudgetInputFromCapability(c llm.ProviderCapability) BudgetInput {
	return BudgetInput{WindowTokens: c.WindowTokens, ReservedOutputTokens: c.MaximumOutputTokens, EstimatorErrorTokens: c.Estimator.DeclaredErrorTokens, EstimatorID: c.Estimator.ID, EstimatorVersion: c.Estimator.Version}
}

// CalculateCompactionBudget applies the normative capacity and forecast formulas.
func CalculateCompactionBudget(in BudgetInput) (CompactionBudget, error) {
	p := in.Policy.normalized()
	if err := p.Validate(); err != nil {
		return CompactionBudget{}, err
	}
	if in.WindowTokens <= 0 || in.ReservedOutputTokens < 0 {
		return CompactionBudget{}, ErrInsufficientInputCapacity
	}
	errTokens := in.EstimatorErrorTokens
	if errTokens == 0 && in.CalibrationSamples >= 1000 {
		errTokens = in.CalibratedP99Undercount
	}
	safety := max(1024, ceilRatio(in.WindowTokens, .02), errTokens)
	capacity := in.WindowTokens - in.ReservedOutputTokens - safety
	if capacity <= 0 {
		return CompactionBudget{}, ErrInsufficientInputCapacity
	}
	if in.RenderedFixedTokens+in.PendingInputTokens >= capacity {
		if in.Overflow {
			return CompactionBudget{}, ErrContextUnavailable
		}
		return CompactionBudget{}, ErrInsufficientInputCapacity
	}
	forecast := min(8192, ceilRatio(in.WindowTokens, .05))
	if in.ForecastSamples >= 200 && in.ObservedP95TurnTokens > 0 {
		forecast = in.ObservedP95TurnTokens
	}
	return CompactionBudget{WindowTokens: in.WindowTokens, ReservedOutputTokens: in.ReservedOutputTokens, SafetyMarginTokens: safety, EstimatorErrorTokens: errTokens, RenderedFixedTokens: in.RenderedFixedTokens, RenderedHistoryTokens: in.RenderedHistoryTokens, PendingInputTokens: in.PendingInputTokens, ForecastTurnTokens: forecast, InputCapacity: capacity, ProjectedInput: in.RenderedFixedTokens + in.RenderedHistoryTokens + in.PendingInputTokens + forecast, MinimumSavedTokens: max(4096, ceilRatio(capacity, .10)), TriggerRatio: p.TriggerRatio, TargetRatio: p.TargetRatio, MinimumReductionRatio: .20, EstimatorID: in.EstimatorID, EstimatorVersion: in.EstimatorVersion, CalibrationVersion: in.CalibrationVersion}, nil
}

// Triggered reports whether utilization or forecast headroom requires compaction.
func (b CompactionBudget) Triggered() bool {
	if b.InputCapacity <= 0 {
		return false
	}
	return float64(b.ProjectedInput)/float64(b.InputCapacity) >= b.TriggerRatio || b.InputCapacity-b.ProjectedInput < b.ForecastTurnTokens
}

// ValidateActivation enforces absolute, relative, and post-target savings gates.
func (b CompactionBudget) ValidateActivation(before, after int) error {
	saved := before - after
	if saved < b.MinimumSavedTokens {
		return ErrInsufficientSavings
	}
	if before <= 0 || float64(saved)/float64(before) < b.MinimumReductionRatio {
		return ErrInsufficientReduction
	}
	if b.InputCapacity <= 0 || float64(after)/float64(b.InputCapacity) > b.TargetRatio {
		return ErrInsufficientReduction
	}
	return nil
}
func ceilRatio(n int, r float64) int { return int(math.Ceil(float64(n) * r)) }
