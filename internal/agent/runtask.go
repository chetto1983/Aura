package agent

import (
	"context"
	"log/slog"
	"strings"
	"time"

	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/llm"
)

// RunTaskDeps groups the wiring needed by RunTask. Unlike Runner, the limits
// are per-call parameters read fresh from the caller's config each invocation,
// so dashboard live-tune of MaxIterations/Timeout/ToolTimeout propagates to
// the next call without mutating shared state.
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

// RunTask executes one bounded agent turn using the supplied deps and task.
// It is the stateless counterpart to Runner.Run: callers read limits fresh
// from their config and pass them in deps, so there is no shared mutable
// state and no Runner construction required.
func RunTask(ctx context.Context, deps RunTaskDeps, task Task) (Result, error) {
	start := time.Now()

	if deps.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, deps.Timeout)
		defer cancel()
	}

	messages, err := initialMessages(task)
	if err != nil {
		return Result{}, err
	}

	maxIterations := deps.MaxIterations
	if maxIterations <= 0 {
		maxIterations = defaultMaxIterations
	}
	toolTimeout := deps.ToolTimeout
	if toolTimeout <= 0 {
		toolTimeout = defaultToolTimeout
	}

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	allowlist := cleanToolList(task.ToolAllowlist)
	state := newAgentState(messages)
	exec := newAgentExecutor(deps.Tools, state, logger, allowlist, task.UserID, task.MaxToolResultChars, toolTimeout)
	client := NewNoStreamClient(deps.LLM, deps.Model, task.Temperature, deps.ReasoningEffort)

	inv := Invocation{
		Client:   client,
		Executor: exec,
		State:    state,
		Tools:    runTaskToolDefs(deps.Tools, allowlist),
		Options: Options{
			MaxIterations:       maxIterations,
			MaxToolCalls:        task.MaxToolCalls,
			CompleteOnDeadline:  task.CompleteOnDeadline,
			FinalizationTimeout: task.FinalizationTimeout,
			MaxToolResultChars:  task.MaxToolResultChars,
			PhantomToolGuard:    deps.PhantomGuard,
			Logger:              logger,
			// Preserve Runner semantics: enforce budget via MaxToolCalls, not
			// sticky dedup. Background agents may intentionally call the same
			// tool multiple times (multi-source research).
			DisableInBatchDedup: true,
			BeforeTool: BeforeToolCallback(func(_ llm.ToolCall, _ ToolCallState) ToolCallDecision {
				return ToolCallDecision{}
			}),
		},
		Logger: logger,
	}

	rtResult, err := Run(ctx, inv)

	content := rtResult.Text
	// Preserve the Runner fallback message when the iteration budget is
	// exhausted — callers expect "maximum iteration" in the reply.
	if rtResult.Stats.MaxIterationsHit {
		content = "Agent loop stopped after reaching the maximum iteration limit."
		if text := strings.TrimSpace(rtResult.Text); text != "" {
			content += "\n\nLast tool result (truncated):\n" + limitToolContent(text, 400)
		}
	}

	out := Result{
		Content:  content,
		Messages: state.Messages(),
		LLMCalls: rtResult.Stats.LLMCalls,
		ToolCalls: rtResult.Stats.ToolCallsExecuted,
		Tokens: llm.TokenUsage{
			PromptTokens:     rtResult.Stats.TokensPrompt,
			CompletionTokens: rtResult.Stats.TokensCompletion,
			TotalTokens:      rtResult.Stats.TokensTotal,
		},
		Elapsed: time.Since(start).Round(time.Millisecond),
	}
	return out, err
}
