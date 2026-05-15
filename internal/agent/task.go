package agent

import (
	"log/slog"
	"time"

	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/llm"
)

const (
	defaultMaxIterations = 5
	defaultTimeout       = 60 * time.Second
	defaultToolTimeout   = 30 * time.Second
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

// RunTaskDeps groups the wiring needed by RunTask. Unlike the former Runner,
// limits are per-call parameters read fresh from the caller's config each
// invocation, so dashboard live-tune of MaxIterations/Timeout/ToolTimeout
// propagates to the next call without mutating shared state.
type RunTaskDeps struct {
	LLM             llm.Client
	Tools           *tools.Registry
	Model           string
	ReasoningEffort string
	PhantomGuard    *PhantomToolGuard
	Logger          *slog.Logger
	// Per-call limits — read once at RunTask entry; runtime changes apply
	// to the NEXT call, never to a call already in flight.
	MaxIterations int
	Timeout       time.Duration
	ToolTimeout   time.Duration
}
