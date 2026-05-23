package agent

import (
	"log/slog"

	governance "github.com/aura/aura/internal/agent/governance"
)

// applyBudgetDefaults fills zero-value token and cost caps with the
// governance defaults. Called once at the start of runLoop.
func applyBudgetDefaults(opts *Options) {
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = governance.DefaultMaxTokens
	}
	if opts.MaxCostUSD <= 0 {
		opts.MaxCostUSD = governance.DefaultMaxCostUSD
	}
}

// checkTokenBudget returns true and sets stats.StopReason when the
// accumulated token count has reached the per-run ceiling (signal 2 of the
// OR-of-four guard). The caller is responsible for cancelling iterCancel and
// calling gracefulFinalize.
func checkTokenBudget(stats *Stats, opts Options, logger *slog.Logger, iteration int) bool {
	if stats.TokensTotal < opts.MaxTokens {
		return false
	}
	stats.StopReason = governance.StopReasonTokenBudget
	logger.Warn("agent: token_budget_exceeded",
		"iteration", iteration,
		"tokens_total", stats.TokensTotal,
		"max_tokens", opts.MaxTokens,
	)
	return true
}

// checkCostBudget returns true and sets stats.StopReason when estimated cost
// exceeds the per-run ceiling (signal 4 of the OR-of-four guard). Returns
// false immediately when EstimateCost is nil — R6: never block on cost alone
// when the provider returns no usage data.
func checkCostBudget(stats *Stats, opts Options, logger *slog.Logger, iteration int) bool {
	if opts.EstimateCost == nil || stats.CostUSD < opts.MaxCostUSD {
		return false
	}
	stats.StopReason = governance.StopReasonCostBudget
	logger.Warn("agent: cost_budget_exceeded",
		"iteration", iteration,
		"cost_usd", stats.CostUSD,
		"max_cost_usd", opts.MaxCostUSD,
	)
	return true
}
