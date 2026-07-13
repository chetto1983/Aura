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
	// ErrInvalidL24Waiver rejects emergency degradation without an allowed L2.4 outcome.
	ErrInvalidL24Waiver = errors.New("invalid_l2_4_waiver")
)

// L24Waiver is the closed set of outcomes that may authorize L2.5 degradation.
type L24Waiver string

const (
	// WaiverDisabled records that semantic activation is explicitly disabled.
	WaiverDisabled L24Waiver = "disabled"
	// WaiverNoEligiblePrefix records that no closed semantic prefix exists.
	WaiverNoEligiblePrefix L24Waiver = "no_eligible_prefix"
	// WaiverProviderUnsupported records a fail-closed adapter capability outcome.
	WaiverProviderUnsupported L24Waiver = "provider_unsupported"
	// WaiverQualityRejected records deterministic candidate validation failure.
	WaiverQualityRejected L24Waiver = "quality_rejected"
	// WaiverTimeout records an elapsed bounded semantic attempt.
	WaiverTimeout L24Waiver = "timeout"
)

// Validate rejects outcomes outside the normative L2.4 waiver vocabulary.
func (w L24Waiver) Validate() error {
	switch w {
	case WaiverDisabled, WaiverNoEligiblePrefix, WaiverProviderUnsupported, WaiverQualityRejected, WaiverTimeout:
		return nil
	default:
		return ErrInvalidL24Waiver
	}
}

// BudgetReason is a bounded, content-free decision reason.
type BudgetReason string

const (
	// BudgetReasonBelowThreshold means no proactive action is required.
	BudgetReasonBelowThreshold BudgetReason = "below_threshold"
	// BudgetReasonHysteresis suppresses repeated work until utilization rises again.
	BudgetReasonHysteresis BudgetReason = "hysteresis"
	// BudgetReasonSemanticRequired requires L2.4 before any emergency candidate.
	BudgetReasonSemanticRequired BudgetReason = "semantic_required"
	// BudgetReasonEmergencyAllowed means a validated waiver authorizes L2.5.
	BudgetReasonEmergencyAllowed BudgetReason = "emergency_allowed"
)

// BudgetDecisionInput contains only pure projection state; coordinators own mutation.
type BudgetDecisionInput struct {
	BudgetInput
	L1Edits          []ContextEdit
	SemanticEligible bool
	HysteresisActive bool
	Waiver           L24Waiver
}

// BudgetDecision atomically describes the L1, L2.4 and L2.5 ladder outcome.
type BudgetDecision struct {
	Budget             CompactionBudget
	L1Edits            []ContextEdit
	SemanticCandidate  bool
	EmergencyCandidate bool
	Waiver             L24Waiver
	Reason             BudgetReason
}

// DecideCompactionBudget is the pure decision seam. It cannot persist, infer or emit rot events.
func DecideCompactionBudget(in BudgetDecisionInput) (BudgetDecision, error) {
	budget, err := CalculateCompactionBudget(in.BudgetInput)
	if err != nil {
		return BudgetDecision{}, err
	}
	d := BudgetDecision{Budget: budget, L1Edits: append([]ContextEdit(nil), in.L1Edits...), Reason: BudgetReasonBelowThreshold}
	if !budget.Triggered() {
		return d, nil
	}
	if in.HysteresisActive {
		d.Reason = BudgetReasonHysteresis
		return d, nil
	}
	if in.SemanticEligible {
		d.SemanticCandidate, d.Reason = true, BudgetReasonSemanticRequired
		return d, nil
	}
	if err := in.Waiver.Validate(); err != nil {
		return BudgetDecision{}, err
	}
	d.EmergencyCandidate, d.Waiver, d.Reason = true, in.Waiver, BudgetReasonEmergencyAllowed
	return d, nil
}

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
