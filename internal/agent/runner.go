package agent

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/agentloop"
	"github.com/aura/aura/internal/agentruntime"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/tools"
)

const (
	defaultMaxIterations = 5
	defaultTimeout       = 60 * time.Second
	defaultToolTimeout   = 30 * time.Second
)

// Runner executes a bounded LLM/tool loop without Telegram coupling. It is the
// small reusable core future AuraBot workers can use inside SwarmManager.
//
// Streaming asymmetry (F-025): unlike the Telegram-facing conversation loop,
// Runner uses llm.Client.Send and never Stream. Background agents return a
// single Result struct rather than progressive output; there is no chat to
// progressively edit. If a future caller needs streamed tokens (e.g. dashboard
// live view of a swarm worker) it can plumb a streaming overload through Task.
//
// Tool-arg validation (F-017): per-call args (call.Arguments) are forwarded
// to tools.Registry.Execute without schema validation. Each tool is
// responsible for validating its own input — the contract is enforced at the
// tool boundary, not here. The clone in cloneToolArgs protects shared state;
// it does NOT sanitize.
type Runner struct {
	mu              sync.RWMutex
	llm             llm.Client
	tools           *tools.Registry
	model           string
	maxIterations   int
	timeout         time.Duration
	toolTimeout     time.Duration
	reasoningEffort string
	logger          *slog.Logger
	phantomGuard    *agentloop.PhantomToolGuard
}

// Config wires a Runner. ToolRegistry may be nil for text-only tasks.
type Config struct {
	LLM             llm.Client
	Tools           *tools.Registry
	Model           string
	MaxIterations   int
	Timeout         time.Duration
	ToolTimeout     time.Duration
	ReasoningEffort string // forwarded to every llm.Request the runner builds
	Logger          *slog.Logger
	// PhantomToolGuard, when non-nil, runs the agentloop phantom-tool
	// detection on every no-tool-call response and injects a user-side
	// correction when the model names a tool it didn't actually invoke
	// in this turn. Opt-in — when nil, behavior is unchanged.
	PhantomToolGuard *agentloop.PhantomToolGuard
}

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

// Result captures the final response and enough telemetry for SwarmManager to
// persist/audit the worker.
type Result struct {
	Content   string
	Messages  []llm.Message
	LLMCalls  int
	ToolCalls int
	Tokens    llm.TokenUsage
	Elapsed   time.Duration
}

// LimitController is the live runtime tuning surface for a runner.
type LimitController interface {
	UpdateLimits(maxIterations int, timeout time.Duration, toolTimeout time.Duration)
}

func NewRunner(cfg Config) (*Runner, error) {
	if cfg.LLM == nil {
		return nil, errors.New("agent runner: llm client required")
	}
	maxIterations := cfg.MaxIterations
	if maxIterations <= 0 {
		maxIterations = defaultMaxIterations
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	toolTimeout := cfg.ToolTimeout
	if toolTimeout <= 0 {
		toolTimeout = defaultToolTimeout
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		llm:             cfg.LLM,
		tools:           cfg.Tools,
		model:           cfg.Model,
		maxIterations:   maxIterations,
		timeout:         timeout,
		toolTimeout:     toolTimeout,
		reasoningEffort: cfg.ReasoningEffort,
		logger:          logger,
		phantomGuard:    cfg.PhantomToolGuard,
	}, nil
}

// Limits returns the runtime loop/deadline limits currently used for new runs.
func (r *Runner) Limits() (maxIterations int, timeout time.Duration, toolTimeout time.Duration) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.maxIterations, r.timeout, r.toolTimeout
}

// UpdateLimits changes the loop/deadline limits used by subsequent runs.
// Non-positive inputs fall back to the same defaults used by NewRunner.
func (r *Runner) UpdateLimits(maxIterations int, timeout time.Duration, toolTimeout time.Duration) {
	if maxIterations <= 0 {
		maxIterations = defaultMaxIterations
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if toolTimeout <= 0 {
		toolTimeout = defaultToolTimeout
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.maxIterations = maxIterations
	r.timeout = timeout
	r.toolTimeout = toolTimeout
}

// Run executes one bounded agent turn via agentruntime.Run.
//
// Timeout precedence (F-005):
//   - r.timeout (default 60s) caps the whole Run via context.WithTimeout.
//     Once fired, every in-flight tool's ctx is cancelled and partial outcomes
//     are stamped with FormatToolError(ctx.Err()) per F-001.
//   - r.toolTimeout (default 30s) caps each individual tool call. Multiple
//     tools fan out in parallel within one turn, so the effective wall-clock
//     for a turn is bounded by max(toolTimeouts) + LLM RTT, not the sum.
//   - A misbehaving tool that ignores its ctx still cannot pin Run open beyond
//     r.timeout because the parallel-fanout wait races ctx.Done() (F-001).
func (r *Runner) Run(ctx context.Context, task Task) (Result, error) {
	start := time.Now()
	maxIterations, timeout, toolTimeout := r.Limits()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	messages, err := initialMessages(task)
	if err != nil {
		return Result{}, err
	}

	allowlist := cleanToolList(task.ToolAllowlist)
	state := newAgentState(messages)
	exec := newAgentExecutor(r.tools, state, r.logger, allowlist, task.UserID, task.MaxToolResultChars, toolTimeout)
	client := agentruntime.NewNoStreamClient(r.llm, r.model, task.Temperature, r.reasoningEffort)

	inv := agentruntime.Invocation{
		Client:   client,
		Executor: exec,
		State:    state,
		Tools:    r.toolDefinitions(allowlist),
		Options: agentloop.Options{
			MaxIterations:  maxIterations,
			MaxToolCalls:   task.MaxToolCalls,
			CompleteOnDeadline:  task.CompleteOnDeadline,
			FinalizationTimeout: task.FinalizationTimeout,
			MaxToolResultChars:  task.MaxToolResultChars,
			PhantomToolGuard:    r.phantomGuard,
			Logger:              r.logger,
			// The old Runner had no dedup — it enforced budget purely via
			// MaxToolCalls, not sticky-dedup or in-batch dedup. Restore
			// that semantics so background agents calling the same tool
			// multiple times (intentionally, e.g. multi-source research)
			// are not silently short-changed on their allowed tool budget.
			DisableInBatchDedup: true,
			BeforeTool: agentloop.BeforeToolCallback(func(_ llm.ToolCall, _ agentloop.ToolCallState) agentloop.ToolCallDecision {
				return agentloop.ToolCallDecision{}
			}),
		},
		Logger: r.logger,
	}

	rtResult, err := agentruntime.Run(ctx, inv)

	content := rtResult.Text
	// Preserve the old Runner fallback message when the iteration budget is
	// exhausted — tests (and callers) expect "maximum iteration" in the
	// reply. agentloop returns the raw last-tool-result; we wrap it here
	// so the API surface stays stable across the migration.
	if rtResult.Stats.MaxIterationsHit {
		content = "Agent loop stopped after reaching the maximum iteration limit."
		if text := strings.TrimSpace(rtResult.Text); text != "" {
			content += "\n\nLast tool result (truncated):\n" + limitToolContent(text, 400)
		}
	}

	out := Result{
		Content:   content,
		Messages:  state.Messages(),
		LLMCalls:  rtResult.Stats.LLMCalls,
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

func initialMessages(task Task) ([]llm.Message, error) {
	if len(task.Messages) > 0 {
		cp := make([]llm.Message, len(task.Messages))
		copy(cp, task.Messages)
		return cp, nil
	}
	prompt := strings.TrimSpace(task.Prompt)
	if prompt == "" {
		return nil, errors.New("agent runner: prompt or messages required")
	}
	messages := make([]llm.Message, 0, 2)
	if system := strings.TrimSpace(task.SystemPrompt); system != "" {
		messages = append(messages, llm.Message{Role: "system", Content: system})
	}
	messages = append(messages, llm.Message{Role: "user", Content: prompt})
	return messages, nil
}

func (r *Runner) toolDefinitions(allowlist []string) []llm.ToolDefinition {
	if r.tools == nil || len(allowlist) == 0 {
		return nil
	}
	defs := r.tools.Definitions()
	out := make([]llm.ToolDefinition, 0, len(defs))
	for _, def := range defs {
		// allowlist is already lowercased by cleanToolList; compare in the
		// same case so canonical lowercase tool names match unconditionally
		// (F-018).
		if slices.Contains(allowlist, strings.ToLower(def.Name)) {
			out = append(out, def)
		}
	}
	return out
}

// cleanToolList trims, lowercases, and dedupes the LLM-or-config-supplied
// tool allowlist. Lowercasing is defense-in-depth: tool names in the registry
// are canonical snake_case, but if a future schema mismatch or operator typo
// produces "Web_Fetch" the allowlist would otherwise silently miss
// "web_fetch" emitted by the LLM (F-018).
func cleanToolList(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
