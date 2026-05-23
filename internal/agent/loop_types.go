// loop_types.go holds the public types, interfaces, and constants that
// define the agent loop's external API. Kept separate from loop.go so the
// main loop body stays under 600 LOC (per feedback_per_module_deep_refactor_mandatory).
package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
)

type ChatClient interface {
	Chat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (ChatResponse, error)
}

type ChatResponse struct {
	Response  llm.Response
	Delivered bool
}

type State interface {
	Messages() []llm.Message
	TrackTokens(llm.TokenUsage)
	AddAssistantMessage(content string)
	AddAssistantToolCallMessage(content string, calls []llm.ToolCall)
	AddToolResultMessage(id, content string)
}

type ToolExecutor interface {
	ExecuteToolCalls(ctx context.Context, calls []llm.ToolCall) ExecutionSummary
}

type ToolExecutorFunc func(ctx context.Context, calls []llm.ToolCall) ExecutionSummary

func (f ToolExecutorFunc) ExecuteToolCalls(ctx context.Context, calls []llm.ToolCall) ExecutionSummary {
	return f(ctx, calls)
}

// ExecutionSummary is the per-batch result returned by the tool executor.
type ExecutionSummary struct {
	LastResult     string
	FatalResult    string
	ReadSkillNames []string
	TerminalTool   string
	Results        map[string]string
	// AwaitingUserInput is non-nil when the ask_user tool was dispatched and
	// returned ErrAwaitingUserInput. The loop pauses and does not continue.
	AwaitingUserInput *tools.ErrAwaitingUserInput
}

type TerminalHandler func(ctx context.Context, terminalTool, lastToolResult string, stats *Stats) (text string, delivered bool, handled bool)

type ToolCallState struct {
	InBatchDuplicate bool
	PriorIdentical   bool
	CallsForTool     int
}

type ToolCallDecision struct {
	Skip   bool
	Result string
}

type BeforeToolCallback func(llm.ToolCall, ToolCallState) ToolCallDecision

// ToolBriefer summarises recent tool-call failures into a compact capsule for
// injection into the per-turn LLM context (US-J04).
type ToolBriefer interface {
	Brief(ctx context.Context, threadID string, toolNames []string, availableSet map[string]struct{}) string
}

type Options struct {
	MaxIterations int
	MaxElapsed    time.Duration
	// ParallelTools caps the number of tool calls dispatched concurrently per
	// LLM iteration. 0 defaults to 4. Set via AURA_AGENT_PARALLEL_TOOLS.
	ParallelTools int
	// DisableInBatchDedup skips the DedupeToolCalls pre-pass so every call
	// announced in a single LLM response enters the execution path, even
	// when multiple calls share identical (name, arguments). Defaults to
	// false (dedup ON). Background task bridges set this to true to enforce
	// budget via MaxToolCalls, not dedup-then-budget.
	DisableInBatchDedup bool
	// MaxToolCalls caps the TOTAL number of fresh tool calls dispatched across
	// all iterations of a single Run. Zero means unlimited.
	MaxToolCalls int
	// CompleteOnDeadline returns a partial assistant message instead of an
	// error when the LLM Chat call hits context.DeadlineExceeded AND at
	// least one tool call has already been executed.
	CompleteOnDeadline bool
	// FinalizationTimeout is the wall-clock cap for the LLM round issued
	// after the loop enters finalizing mode (MaxToolCalls hit). Zero leaves
	// the per-iteration remaining-budget ctx in place.
	FinalizationTimeout     time.Duration
	Tools                   []llm.ToolDefinition
	ToolsProvider           func() []llm.ToolDefinition
	TerminalToolPolicy      bool
	AllowNoToolFinalization bool
	DuplicateToolResult     func(llm.ToolCall) string
	MaxCallsPerTool         map[string]int
	BeforeTool              BeforeToolCallback
	BeforeLLM               func() (message string, stop bool)
	RecordUsage             func(llm.TokenUsage)
	EstimateCost            func(llm.TokenUsage) float64
	OnStats                 func(Stats)
	// OnLLMStart fires immediately before each client.Chat() call.
	OnLLMStart func(iteration, messagesIn, toolsIn int)
	// OnLLMDelta fires once per LLM call with the full assistant content.
	OnLLMDelta func(deltaText string)
	// OnToolStart fires once per fresh tool call just before executor dispatch.
	OnToolStart func(call llm.ToolCall, argKeys []string)
	// OnToolEnd fires once per fresh tool call after the executor batch returns.
	OnToolEnd       func(callID, toolName string, success bool, elapsed time.Duration, preview string)
	TerminalHandler TerminalHandler
	// MaxTokens caps cumulative prompt+completion tokens across all LLM calls
	// in this turn (US-OUT-08, OR-of-four guard, signal 2). 0 uses
	// governance.DefaultMaxTokens (500_000).
	MaxTokens int
	// MaxCostUSD caps estimated USD cost across all LLM calls in this turn
	// (US-OUT-08, OR-of-four guard, signal 4). 0 uses
	// governance.DefaultMaxCostUSD ($20). Skipped when EstimateCost is nil
	// (R6 safety: never block on cost alone when provider returns no usage data).
	MaxCostUSD float64
	// MaxToolResultChars caps each tool message size before going to the LLM.
	MaxToolResultChars int
	// MicrocompactKeepRecent / MicrocompactMinChars control the rolling
	// compaction of read_file/exec/web_* tool results. Zero values use the
	// governance package defaults (10 / 500).
	MicrocompactKeepRecent int
	MicrocompactMinChars   int
	// PhantomToolGuard, when non-nil, runs a post-LLM heuristic on
	// no-tool-call responses.
	PhantomToolGuard *PhantomToolGuard
	// ToolResolver is the on-demand schema lookup used by the per-turn pool.
	ToolResolver func(name string) (llm.ToolDefinition, bool)
	// OnQuestionRequested fires when ask_user pauses the loop, carrying the
	// full question payload so channel adapters can render it to the user.
	OnQuestionRequested func(*tools.ErrAwaitingUserInput)
	// Logger is an optional structured logger. When nil the loop falls back to
	// slog.Default().
	Logger *slog.Logger
	// Briefer, when non-nil, is called before each LLM round to inject a
	// one-turn tool-experience capsule into the message context (US-J04).
	// NEVER mutates state.Messages(); the capsule lives for one turn only.
	Briefer ToolBriefer
	// BrieferRunID is the run/thread identifier passed to the Briefer and to
	// the retry-budget checker (US-J05). It identifies the current run in the
	// tool_attempts table.
	BrieferRunID string
	// RetryBudgets caps how many prior failures of a given outcome class the
	// loop tolerates before refusing the next dispatch of the same tool.
	//
	// Defaults when this field is populated:
	//   OutcomeRecoverable: 2  — allow up to 2 recoverable errors, refuse 3rd
	//   OutcomeBlocked:     0  — refuse after the very first blocked failure
	//   OutcomeFatal:       0  — refuse after the very first fatal failure
	//   OutcomeCancelled:   0  — refuse after the very first cancelled result
	//
	// Outer-loop only — MCP server internal retries are invisible: a single
	// Aura-side dispatch may trigger multiple provider-side retries, but
	// AttemptN counts only Aura dispatches, keeping the budget semantics clean.
	//
	// When nil or empty, no budget check is performed.
	RetryBudgets map[tools.Outcome]int
	// RetryBudgetRepo is the attempts.Repo consulted by the retry-budget check.
	// When nil the check is skipped (feature-flag-ready: set to nil to disable).
	RetryBudgetRepo attempts.Repo
}

// loopResult is the internal result of one runLoop invocation.
type loopResult struct {
	Text      string
	Delivered bool
	Stats     Stats
}

type Stats struct {
	LLMCalls          int
	ToolCalls         int
	LoopSteps         int
	ToolsCalled       []string
	ReadSkills        []string
	SkillsRead        bool
	SwarmUsed         bool
	SandboxUsed       bool
	TerminalTool      string
	DuplicateToolCall bool
	TokensPrompt      int
	TokensCompletion  int
	TokensTotal       int
	CostUSD           float64
	MaxIterationsHit  bool
	MaxElapsedHit     bool
	StopReason        string
	// PhantomToolDetections counts how many times the phantom-tool guard fired.
	PhantomToolDetections int
	// PhantomToolCorrected counts how many detections successfully recovered.
	PhantomToolCorrected int
	// ToolCallsExecuted counts fresh (post-dedupe, post-budget-cap) tool calls
	// actually dispatched. Distinct from ToolCalls which counts all announced.
	ToolCallsExecuted int
	// CacheReadTokens accumulates prompt tokens served from the provider's KV
	// cache across all LLM calls in this run. Non-zero only when the provider
	// confirmed a cache hit (OpenAI: prompt_tokens_details.cached_tokens;
	// Anthropic: cache_read_input_tokens).
	CacheReadTokens int
	// TurnActions accumulates per-tool-call entries for the "## Already done
	// this turn" block injected into the system prompt on subsequent iterations
	// (US-OUT-04). One entry per fresh executed tool call; resets per Run.
	TurnActions []conversation.TaskEntry
}

// maxLengthRecoveries caps the number of finish_reason='length' text-recovery
// injections per turn (US-OUT-05). After this many recoveries the loop returns
// the partial text rather than injecting another prompt.
const maxLengthRecoveries = 3

// lengthRecoveryPrompt is the literal user-side message injected when the LLM
// hits the context window mid-response (finish_reason='length', text only).
const lengthRecoveryPrompt = "Output limit reached. Continue exactly where you left off — no recap, no apology."

// MaxIterationsCeiling is a hard upper bound on opts.MaxIterations.
// Raised from 50 in Phase-F: per docs/aura-main-loop-limits-audit.md §3.5,
// Aura caps LATENCY (MaxElapsed = 5 min) and COST (HardBudget = $20), not
// CAPABILITY. The wall clock + budget already bound runaway loops; iteration
// count just needs enough headroom for legitimately wide multi-step
// research/debug workflows.
const MaxIterationsCeiling = 100

// DefaultMaxElapsed is the implicit per-turn wall-clock cap when callers
// leave opts.MaxElapsed at zero.
const DefaultMaxElapsed = 5 * time.Minute
