package governance

// StopReason identifies which of the OR-of-four budget signals terminated a
// turn (US-OUT-08). Values are stored as stats.StopReason strings in the agent
// loop and persisted to the runs store stats_json column via agentloop.go.
const (
	// StopReasonMaxIterations fires when the iteration counter reaches MaxIterations.
	StopReasonMaxIterations = "max_iterations_hit"
	// StopReasonTokenBudget fires when cumulative prompt+completion tokens reach MaxTokens.
	StopReasonTokenBudget = "token_budget_exceeded"
	// StopReasonWallClock fires when elapsed wall-clock time reaches MaxElapsed.
	StopReasonWallClock = "wall_clock_exceeded"
	// StopReasonCostBudget fires when estimated USD cost reaches MaxCostUSD.
	// Requires EstimateCost to be wired; skipped when nil (R6 fallback).
	StopReasonCostBudget = "cost_budget_exceeded"
	// StopReasonEmptyResponse fires when the LLM returns empty content with no
	// tool calls, signalling it has nothing useful to emit.
	StopReasonEmptyResponse = "empty_llm_response"
)

// Default OR-of-four budget ceilings applied when the corresponding Options
// fields are zero (US-OUT-08). Operators can override via env vars or the
// runtime settings table.
const (
	// DefaultMaxTokens caps cumulative prompt+completion tokens per turn.
	DefaultMaxTokens = 500_000
	// DefaultMaxCostUSD caps estimated USD cost per turn.
	DefaultMaxCostUSD = 20.0
)
