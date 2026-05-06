package budget

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/aura/aura/internal/llm"
)

// Tracker manages global token usage and budget enforcement.
type Tracker struct {
	mu                 sync.Mutex
	totalTokens        int
	promptTokens       int
	completionTokens   int
	totalCost          float64
	softBudget         float64
	hardBudget         float64
	inputPerMTokens    float64
	outputPerMTokens   float64
	logger             *slog.Logger
	softBudgetHit      bool
	hardBudgetHit      bool
	softBudgetNotified bool
}

// Config holds budget configuration.
type Config struct {
	SoftBudget            float64
	HardBudget            float64
	InputCostPerMTokens   float64 // USD per 1M input/prompt tokens
	OutputCostPerMTokens  float64 // USD per 1M output/completion tokens
	LegacyCostPerTokenUSD float64 // compatibility with the old USD/token setting
}

// NewTracker creates a new budget tracker.
func NewTracker(cfg Config, logger *slog.Logger) *Tracker {
	inputPerM, outputPerM := normalizePrices(cfg)
	return &Tracker{
		softBudget:       cfg.SoftBudget,
		hardBudget:       cfg.HardBudget,
		inputPerMTokens:  inputPerM,
		outputPerMTokens: outputPerM,
		logger:           logger,
	}
}

// ApplyConfig updates budget limits and prices without resetting usage totals.
func (t *Tracker) ApplyConfig(cfg Config) {
	if t == nil {
		return
	}
	inputPerM, outputPerM := normalizePrices(cfg)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.softBudget = cfg.SoftBudget
	t.hardBudget = cfg.HardBudget
	t.inputPerMTokens = inputPerM
	t.outputPerMTokens = outputPerM
	t.totalCost = costFor(t.promptTokens, t.completionTokens, inputPerM, outputPerM)
	t.softBudgetHit = t.softBudget > 0 && t.totalCost >= t.softBudget
	t.hardBudgetHit = t.hardBudget > 0 && t.totalCost >= t.hardBudget
}

func normalizePrices(cfg Config) (float64, float64) {
	inputPerM := cfg.InputCostPerMTokens
	outputPerM := cfg.OutputCostPerMTokens
	if inputPerM <= 0 && outputPerM <= 0 && cfg.LegacyCostPerTokenUSD > 0 {
		perM := cfg.LegacyCostPerTokenUSD * 1_000_000
		inputPerM = perM
		outputPerM = perM
	}
	if inputPerM <= 0 {
		inputPerM = 0.20
	}
	if outputPerM <= 0 {
		outputPerM = 0.80
	}
	return inputPerM, outputPerM
}

// RecordUsage records token usage from an LLM call.
func (t *Tracker) RecordUsage(usage llm.TokenUsage) {
	t.mu.Lock()
	defer t.mu.Unlock()

	promptTokens := usage.PromptTokens
	completionTokens := usage.CompletionTokens
	if promptTokens == 0 && completionTokens == 0 {
		promptTokens = usage.TotalTokens
	}
	totalTokens := usage.TotalTokens
	if totalTokens == 0 {
		totalTokens = promptTokens + completionTokens
	}
	t.totalTokens += totalTokens
	t.promptTokens += promptTokens
	t.completionTokens += completionTokens
	t.totalCost += costFor(promptTokens, completionTokens, t.inputPerMTokens, t.outputPerMTokens)

	// Check soft budget
	if t.softBudget > 0 && t.totalCost >= t.softBudget && !t.softBudgetHit {
		t.softBudgetHit = true
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

// IsSoftBudgetExceeded returns true if the soft budget has been exceeded.
func (t *Tracker) IsSoftBudgetExceeded() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.softBudgetHit
}

// ShouldNotifySoftBudget returns true the first time soft budget is exceeded,
// then returns false on subsequent calls. Used for one-time Telegram notifications.
func (t *Tracker) ShouldNotifySoftBudget() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.softBudgetHit && !t.softBudgetNotified {
		t.softBudgetNotified = true
		return true
	}
	return false
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
	return costFor(contextTokens, expectedOutputTokens, t.inputPerMTokens, t.outputPerMTokens)
}

// CanAfford checks whether an upcoming call would stay within the hard budget.
func (t *Tracker) CanAfford(contextTokens int, expectedOutputTokens int) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.hardBudget <= 0 {
		return true // no hard budget set
	}

	predictedCost := t.totalCost + costFor(contextTokens, expectedOutputTokens, t.inputPerMTokens, t.outputPerMTokens)
	return predictedCost < t.hardBudget
}

// Status returns current budget status for observability.
func (t *Tracker) Status() Status {
	t.mu.Lock()
	defer t.mu.Unlock()

	return Status{
		TotalTokens:        t.totalTokens,
		PromptTokens:       t.promptTokens,
		CompletionTokens:   t.completionTokens,
		TotalCost:          t.totalCost,
		SoftBudget:         t.softBudget,
		HardBudget:         t.hardBudget,
		InputPerMTokens:    t.inputPerMTokens,
		OutputPerMTokens:   t.outputPerMTokens,
		SoftBudgetExceeded: t.softBudgetHit,
		BudgetExceeded:     t.hardBudgetHit,
	}
}

func costFor(promptTokens, completionTokens int, inputPerM, outputPerM float64) float64 {
	return (float64(promptTokens)*inputPerM + float64(completionTokens)*outputPerM) / 1_000_000
}

// Status represents the current budget status.
type Status struct {
	TotalTokens        int
	PromptTokens       int
	CompletionTokens   int
	TotalCost          float64
	SoftBudget         float64
	HardBudget         float64
	InputPerMTokens    float64
	OutputPerMTokens   float64
	SoftBudgetExceeded bool
	BudgetExceeded     bool
}
