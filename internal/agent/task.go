package agent

import (
	"log/slog"
	"time"

	"github.com/aura/aura/internal/agent/governance"
	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/llm"
)

const (
	// defaultMaxIterations is the fallback when RunTaskDeps.MaxIterations <= 0.
	// Aligned with MaxIterationsCeiling (loop.go) per Phase-F principle: cap
	// LATENCY and COST, not CAPABILITY. The wall-clock cap (defaultTimeout)
	// and the budget guardrail still bound runaway loops; the iteration count
	// just needs enough headroom for legitimately wide research/debug
	// workflows. Operators can still narrow via env/dashboard.
	defaultMaxIterations = 100
	// defaultTimeout is the fallback wall clock when RunTaskDeps.Timeout <= 0.
	// Aligned with DefaultAuraBotTimeoutSec (config.go = 300s) so background
	// agents have the same wall-clock budget as the cfg defaults declare.
	defaultTimeout     = 300 * time.Second
	defaultToolTimeout = 30 * time.Second
)

// Task is one isolated background-agent assignment.
type Task struct {
	SystemPrompt        string
	Prompt              string
	Messages            []llm.Message
	ToolAllowlist       []string
	UserID              string
	Temperature         *float64
	MaxToolCalls        int
	MaxToolResultChars  int
	FinalizationTimeout time.Duration
	CompleteOnDeadline  bool
}

// Result captures the final response and enough telemetry for callers to
// persist/audit the agent turn.
type Result struct {
	Content   string
	Messages  []llm.Message
	LLMCalls  int
	ToolCalls int
	Tokens    llm.TokenUsage
	Elapsed   time.Duration
}

// RunTaskDeps groups the wiring needed by RunTask. Limits are per-call
// parameters read fresh from the caller's config each invocation, so dashboard
// live-tune of MaxIterations/Timeout/ToolTimeout propagates to the next call
// without mutating shared state.
type RunTaskDeps struct {
	LLM             llm.Client
	Tools           *tools.Registry
	Model           string
	ReasoningEffort string
	RunID           string
	Logger          *slog.Logger
	// AttemptsRepo persists tool-call observations to tool_attempts (Phase-6
	// US-J03). Nil disables persistence without affecting tool execution.
	AttemptsRepo attempts.Repo
	// TokenJuiceEnabled activates rule-driven tool-output compaction before
	// the result enters the LLM conversation window.
	TokenJuiceEnabled bool
	// SpillDir is the runtime-workspace base directory for spilled tool results
	// (US-OUT-02). Empty string disables spill; routing falls back to inline
	// truncation.
	SpillDir string
	// Per-call limits — read once at RunTask entry; runtime changes apply
	// to the NEXT call, never to a call already in flight.
	MaxIterations int
	Timeout       time.Duration
	ToolTimeout   time.Duration
	// BudgetCaps overrides the per-class tool call caps for this turn
	// (US-OUT-07). Keys match governance.Class* constants ("web", "wiki", ...).
	// Nil uses the hardcoded defaults from governance.NewBudgetTracker.
	BudgetCaps map[string]int
	// PayloadSummarizer intercepts oversized tool results AFTER TokenJuice
	// compaction (US-CTX-03). Nil disables payload summarization — MUST be nil
	// for the summarizer sub-agent itself to prevent recursive dispatch (R1).
	PayloadSummarizer governance.PayloadSummarizer
}
