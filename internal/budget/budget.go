package budget

import (
	"fmt"
	"log/slog"
	"sync"
)

// Tracker manages global token usage and budget enforcement.
type Tracker struct {
	mu            sync.Mutex
	totalTokens   int
	totalCost     float64
	softBudget    float64
	hardBudget    float64
	costPerToken  float64
	logger        *slog.Logger
	hardBudgetHit bool
}

// Config holds budget configuration.
type Config struct {
	SoftBudget   float64
	HardBudget   float64
	CostPerToken float64 // cost per 1K tokens
}

// NewTracker creates a new budget tracker.
func NewTracker(cfg Config, logger *slog.Logger) *Tracker {
	costPerToken := cfg.CostPerToken
	if costPerToken <= 0 {
		costPerToken = 0.01 / 1000 // $0.01 per 1K tokens as a reasonable default
	}
	return &Tracker{
		softBudget:   cfg.SoftBudget,
		hardBudget:   cfg.HardBudget,
		costPerToken: costPerToken,
		logger:       logger,
	}
}

// RecordUsage records token usage from an LLM call.
func (t *Tracker) RecordUsage(totalTokens int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.totalTokens += totalTokens
	t.totalCost = float64(t.totalTokens) * t.costPerToken

	// Check soft budget
	if t.softBudget > 0 && t.totalCost >= t.softBudget && t.totalCost < t.hardBudget {
		t.logger.Warn("soft budget limit reached",
			"cost", fmt.Sprintf("%.4f", t.totalCost),
			"soft_budget", t.softBudget,
			"hard_budget", t.hardBudget,
		)
	}

	// Check hard budget
	if t.hardBudget > 0 && t.totalCost >= t.hardBudget {
		if !t.hardBudgetHit {
			t.hardBudgetHit = true
			t.logger.Error("hard budget limit reached — LLM calls will be halted",
				"cost", fmt.Sprintf("%.4f", t.totalCost),
				"hard_budget", t.hardBudget,
			)
		}
	}
}

// IsHardBudgetExceeded returns true if the hard budget has been exceeded.
func (t *Tracker) IsHardBudgetExceeded() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hardBudgetHit
}

// TotalTokens returns the cumulative token count.
func (t *Tracker) TotalTokens() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.totalTokens
}

// TotalCost returns the estimated total cost.
func (t *Tracker) TotalCost() float64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.totalCost
}

// PredictCost estimates the cost of an upcoming LLM call.
func (t *Tracker) PredictCost(contextTokens int, expectedOutputTokens int) float64 {
	total := contextTokens + expectedOutputTokens
	return float64(total) * t.costPerToken
}

// CanAfford checks whether an upcoming call would stay within the hard budget.
func (t *Tracker) CanAfford(contextTokens int, expectedOutputTokens int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.hardBudget <= 0 {
		return true // no hard budget set
	}

	predictedCost := t.totalCost + float64(contextTokens+expectedOutputTokens)*t.costPerToken
	return predictedCost < t.hardBudget
}

// Status returns current budget status for observability.
func (t *Tracker) Status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()

	return Status{
		TotalTokens:    t.totalTokens,
		TotalCost:      t.totalCost,
		SoftBudget:     t.softBudget,
		HardBudget:     t.hardBudget,
		BudgetExceeded: t.hardBudgetHit,
	}
}

// Status represents the current budget status.
type Status struct {
	TotalTokens    int
	TotalCost      float64
	SoftBudget     float64
	HardBudget     float64
	BudgetExceeded bool
}
