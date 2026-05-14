// Package agentloop runs one Telegram-style assistant turn: alternating LLM
// calls and tool execution until the model produces a final answer or the
// per-turn budget is exhausted.
//
// History note: this loop used to carry a small defensive sub-system —
// tiered iteration budgets (3/4/6 based on detected workload), a
// "SpiralBreaker" that bailed when the registry rejected a hidden tool,
// per-turn retry-nudge counters, and a regex (`looksLikeRawToolEvidence`)
// that scrubbed raw tool output from the user-visible answer. Each of those
// existed because tools used to fail in confusing ways (JSON envelopes,
// "is not available in this runtime"). Once the registry became the single
// source of tool truth and errors became plain text, the defenses became
// noise: they hid problems instead of fixing them. The loop is now small.
package agentloop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

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
// Fields are best-effort observations; nothing here changes control flow
// beyond FatalResult (which short-circuits the turn) and TerminalTool
// (which lets a TerminalHandler take over the final answer).
//
// Results maps each freshly-executed call's ID to its result content. The
// loop uses this to allow legitimate retries of tool calls whose previous
// result was empty or a FormatToolError sentinel (F-006). Executors that
// leave Results nil keep the legacy sticky-dedupe behaviour.
type ExecutionSummary struct {
	LastResult     string
	FatalResult    string
	ReadSkillNames []string
	TerminalTool   string
	Results        map[string]string
}

// IsRetryableToolResult reports whether a prior tool-result text leaves the
// loop willing to re-execute the same call. FormatToolError / FormatFatalToolError
// sentinels ("Error: ...") signal a transient failure the LLM should be allowed
// to retry on a subsequent turn. Empty content is treated as "unknown" —
// callers that did not populate ExecutionSummary.Results keep the legacy
// sticky-dedupe behaviour.
func IsRetryableToolResult(prev string) bool {
	trimmed := strings.TrimSpace(prev)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(trimmed, "Error:")
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

type Options struct {
	MaxIterations int
	MaxElapsed    time.Duration
	// DisableInBatchDedup skips the DedupeToolCalls pre-pass so every call
	// announced in a single LLM response enters the execution path, even
	// when multiple calls share identical (name, arguments). Defaults to
	// false (dedup ON). Background agents such as agent.Runner set this to
	// true to preserve the old Runner semantics: budget enforcement via
	// MaxToolCalls, not dedup-then-budget.
	DisableInBatchDedup bool
	// MaxToolCalls caps the TOTAL number of fresh (post-dedupe, non-skipped)
	// tool calls dispatched across all iterations of a single Run. Zero
	// means unlimited. Distinct from MaxCallsPerTool which caps per tool
	// name. When the cap is reached, the loop enters "finalizing" mode:
	// subsequent LLM rounds are issued with tools=nil and (if the State
	// implements PhantomCorrector) a user-side instruction is injected so
	// the model produces a natural-language answer instead of looping on
	// budget-capped skip stubs.
	MaxToolCalls int
	// CompleteOnDeadline returns a partial assistant message instead of an
	// error when the LLM Chat call hits context.DeadlineExceeded AND at
	// least one tool call has already been executed. The final message is
	// synthesized from the last tool result. Mirrors agent.Runner semantics
	// for swarm workers that prefer a partial answer over a hard failure.
	CompleteOnDeadline bool
	// FinalizationTimeout is the wall-clock cap for the LLM round issued
	// after the loop enters finalizing mode (MaxToolCalls hit). Applied
	// only when CompleteOnDeadline is also true. Zero leaves the per-
	// iteration remaining-budget ctx in place.
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
	// OnLLMStart fires immediately before each client.Chat() call in the
	// per-turn loop. iteration is 0-based; messagesIn is the count handed
	// to the model after governance trimming; toolsIn is the size of the
	// per-turn tool pool the model will see. Wave 3.0 Step 2: lets a
	// chathub adapter emit a message_delta lifecycle around each round.
	OnLLMStart func(iteration, messagesIn, toolsIn int)
	// OnLLMDelta fires once per LLM call with the full assistant content
	// after client.Chat() returns. Current ChatClient.Chat() is one-shot
	// (the Telegram path internally consumes a stream and hands the
	// completed Response back), so callers see "one delta per call". When
	// the streaming ChatClient lands the same callback will fire N times
	// per call without breaking existing consumers.
	OnLLMDelta func(deltaText string)
	// OnToolStart fires once per fresh tool call (post-dedupe, post-policy)
	// just before the executor batch dispatches. argKeys is the SORTED set
	// of top-level argument key NAMES — never values, never nested data.
	// Per the CLAUDE.md value-leakage policy.
	OnToolStart func(call llm.ToolCall, argKeys []string)
	// OnToolEnd fires once per fresh tool call after the executor batch
	// returns. preview is the first ~200 chars of the result string, just
	// enough to distinguish "tool returned" from "tool returned Error: ..."
	// in logs and adapters. success reflects whether the result starts
	// with the FormatToolError "Error:" sentinel (false) or not (true).
	// elapsed measures the WHOLE batch duration — calls in the same batch
	// share a timestamp because ExecuteToolCalls is batch-atomic.
	OnToolEnd       func(callID, toolName string, success bool, elapsed time.Duration, preview string)
	TerminalHandler TerminalHandler
	// MaxToolResultChars caps each tool message size before going to the
	// LLM. Zero uses DefaultMaxToolResultChars (8 KB). Operators tune this
	// via the MAX_TOOL_RESULT_CHARS env var.
	MaxToolResultChars int
	// MicrocompactKeepRecent / MicrocompactMinChars control the rolling
	// compaction of read_file/exec/web_* tool results. Zero values use the
	// package defaults (10 / 500). Operators tune via env.
	MicrocompactKeepRecent int
	MicrocompactMinChars   int
	// PhantomToolGuard, when non-nil, runs a post-LLM heuristic on
	// no-tool-call responses. When it detects content that narrates
	// tool actions without an actual tool_use, the loop injects a
	// corrective user-side message (via the optional PhantomCorrector
	// interface on State) and re-prompts. Capped at the guard's
	// MaxRetries per turn. See phantom_guard.go for the heuristic.
	PhantomToolGuard *PhantomToolGuard
	// ToolResolver is the on-demand schema lookup used by the per-turn
	// tool pool for two purposes:
	//
	//  1. Permissive load: when the model emits a tool_call whose name
	//     isn't in the current pool (typically because it saw the name
	//     in the system-prompt manifest), the loop asks ToolResolver
	//     and adds the definition before dispatching. Fails cleanly
	//     when the name is unknown — the executor surfaces the error.
	//
	//  2. tool_search absorption: when the executor returns a result
	//     for the "tool_search" tool, the loop parses the JSON envelope
	//     and pre-loads each returned name so the next LLM step sees
	//     them in its tools array.
	//
	// Optional. When nil, the pool stays fixed at whatever the seed
	// (Tools / ToolsProvider) provided and the loop behaves as before
	// the deferred-tools rollout.
	ToolResolver func(name string) (llm.ToolDefinition, bool)
	// Logger is an optional structured logger. When nil the loop falls back
	// to slog.Default() so a stuck conversation is debuggable without the
	// caller having to wire OnStats. Only tool names and argument key sets
	// are logged, never values — see the CLAUDE.md value-leakage policy.
	Logger *slog.Logger
}

type Result struct {
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
	// StopReason names the early-exit branch taken by Run, when applicable:
	//   "before_llm"          — opts.BeforeLLM returned stop=true (cost/budget gate)
	//   "max_iterations_hit"  — loop exhausted opts.MaxIterations
	//   "max_elapsed_hit"     — wall-clock exceeded opts.MaxElapsed
	//   ""                    — natural completion (LLM returned no tool calls)
	// (F-030)
	StopReason string
	// PhantomToolDetections counts how many times the phantom-tool guard
	// fired in this turn (LLM described a tool action without invoking
	// one). Always 0 when Options.PhantomToolGuard is nil.
	PhantomToolDetections int
	// PhantomToolCorrected counts how many of the detections successfully
	// recovered (the subsequent retry produced a real tool_use). Useful
	// for measuring guard ROI: detections / corrected > 1 means the
	// correction is the *right* lever; otherwise the model keeps phantoming.
	PhantomToolCorrected int
	// ToolCallsExecuted counts the fresh (post-dedupe, post-budget-cap) tool
	// calls that the executor actually dispatched across all iterations.
	// Distinct from ToolCalls, which counts all announced calls (including
	// deduplicated and budget-capped ones). Background agents map this to
	// agent.Result.ToolCalls so swarm telemetry reflects executed work.
	ToolCallsExecuted int
}

// MaxIterationsCeiling is a hard upper bound the loop applies to whatever
// the caller passed in opts.MaxIterations. A misconfigured caller (or a
// future bug that propagates an unbounded value) cannot ask for arbitrary
// iteration counts: 50 is well above any realistic workload (Telegram's
// default is 8) and well below a per-turn cost-bomb (F-008).
const MaxIterationsCeiling = 50

// DefaultMaxElapsed is the implicit per-turn wall-clock cap when callers
// leave opts.MaxElapsed at zero. Five minutes is comfortable for normal
// chat + tool turns and well below the SLA an operator would want on a
// background agent (F-008).
const DefaultMaxElapsed = 5 * time.Minute

func Run(ctx context.Context, client ChatClient, executor ToolExecutor, state State, opts Options) (Result, error) {
	if opts.MaxIterations < 1 {
		opts.MaxIterations = 1
	}
	if opts.MaxIterations > MaxIterationsCeiling {
		opts.MaxIterations = MaxIterationsCeiling
	}
	if opts.MaxElapsed <= 0 {
		opts.MaxElapsed = DefaultMaxElapsed
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	start := time.Now()
	var lastToolResult string
	var stats Stats
	seenToolCalls := map[string]bool{}
	seenToolCallsResult := map[string]string{}
	toolCallExecutions := map[string]int{}
	// globalToolCallsExecuted counts FRESH (post-dedupe, post-policy) tool
	// calls that the executor actually ran across the whole turn. Used to
	// gate opts.MaxToolCalls and to trigger the finalizing transition.
	// Distinct from stats.ToolCalls which counts ANNOUNCED calls and so
	// overcounts when the loop suppresses duplicates.
	globalToolCallsExecuted := 0
	// finalizing flips on when MaxToolCalls is hit. Subsequent iterations
	// send tools=nil to the LLM (forcing a natural-language answer) and,
	// when CompleteOnDeadline + FinalizationTimeout are set, run under a
	// tightened ctx so a stuck final round still yields a partial answer.
	finalizing := false
	// calledThisTurn tracks which tool names fired in any round of this
	// turn. The phantom guard consults it to distinguish "model
	// legitimately summarizes a tool result it just received" from
	// "model claims an action that never happened."
	calledThisTurn := map[string]bool{}
	emitStats := func() {
		if opts.OnStats != nil {
			opts.OnStats(stats)
		}
	}
	emitStats()
	logger.Debug("agentloop: run start", "max_iterations", opts.MaxIterations, "max_elapsed_ms", opts.MaxElapsed.Milliseconds())

	// Per-turn tool pool. Initialized lazily on first iteration so a
	// before-LLM short-circuit doesn't pay the cost of building it.
	// Grows monotonically across iterations via permissive load and
	// tool_search absorption — see Options.ToolResolver doc.
	var pool *toolPool

	// iterCancel reclaims the per-iteration deadline. It is replaced on each
	// iteration; the outer defer catches the final one so no context.Timeout
	// goroutine survives Run (F-009).
	var iterCancel context.CancelFunc
	defer func() {
		if iterCancel != nil {
			iterCancel()
		}
	}()

	for iteration := 0; iteration < opts.MaxIterations; iteration++ {
		remaining := opts.MaxElapsed - time.Since(start)
		if remaining <= 0 {
			stats.MaxElapsedHit = true
			stats.StopReason = "max_elapsed_hit"
			logger.Warn("agentloop: max_elapsed_hit", "iteration", iteration, "elapsed_ms", time.Since(start).Milliseconds(), "max_elapsed_ms", opts.MaxElapsed.Milliseconds())
			answer := finalAnswerOnBudget(lastToolResult)
			state.AddAssistantMessage(answer)
			emitStats()
			if iterCancel != nil {
				iterCancel()
			}
			return Result{Text: answer, Stats: stats}, nil
		}
		// Bound every blocking call below by the remaining wall-clock budget.
		// Without this a single slow LLM call or tool run could blow the
		// MaxElapsed limit by minutes — the original loop only checked the
		// budget between iterations (F-009).
		// Replaces the previous iteration's deadline (if any) with one bounded
		// by the remaining budget. iterCancel is fired at the top of the
		// next iteration (or at the outer defer on function exit) so every
		// allocated timer is reclaimed without leaking a goroutine (F-009).
		if iterCancel != nil {
			iterCancel()
		}
		var iterCtx context.Context
		iterCtx, iterCancel = context.WithTimeout(ctx, remaining)
		// Finalizing-round ctx tightening: when MaxToolCalls has been hit
		// and the caller opted into CompleteOnDeadline + FinalizationTimeout,
		// rebind iterCtx to the shorter of the two. The previous timer is
		// cancelled to avoid stacking deadlines (F-009).
		if finalizing && opts.CompleteOnDeadline && opts.FinalizationTimeout > 0 {
			iterCancel()
			iterCtx, iterCancel = context.WithTimeout(ctx, opts.FinalizationTimeout)
		}
		if opts.BeforeLLM != nil {
			if message, stop := opts.BeforeLLM(); stop {
				stats.StopReason = "before_llm"
				logger.Info("agentloop: before_llm_stop", "iteration", iteration)
				iterCancel()
				return Result{Text: message, Stats: stats}, nil
			}
		}

		// Initialize the per-turn tool pool on first iteration. Seed from
		// ToolsProvider (preferred — typically deferred-tools always-on set)
		// or Tools. The pool grows monotonically across iterations as
		// tool_search results are absorbed or permissive loads fire.
		if pool == nil {
			seed := opts.Tools
			if opts.ToolsProvider != nil {
				seed = opts.ToolsProvider()
			}
			pool = newToolPool(seed, opts.ToolResolver)
		}
		tools := pool.Defs()
		// Finalizing-round: hide tools so the model is forced to produce a
		// natural-language answer from the evidence already collected
		// instead of issuing another budget-capped tool call.
		if finalizing {
			tools = nil
		}

		stats.LLMCalls++
		stats.LoopSteps++
		messagesForModel := applyGovernance(state.Messages(), opts.MaxToolResultChars, opts.MicrocompactKeepRecent, opts.MicrocompactMinChars)
		if opts.OnLLMStart != nil {
			opts.OnLLMStart(iteration, len(messagesForModel), len(tools))
		}
		resp, err := client.Chat(iterCtx, messagesForModel, tools)
		if err != nil {
			iterCancel()
			// CompleteOnDeadline: when the caller prefers a partial result
			// over a hard failure (swarm workers, scheduled jobs) and the
			// Chat call was cancelled by the deadline, synthesize a final
			// assistant message from the last tool result so downstream
			// telemetry still sees a usable Result. Mirrors agent.Runner
			// runner.go:218-222.
			if opts.CompleteOnDeadline && errors.Is(err, context.DeadlineExceeded) && globalToolCallsExecuted > 0 {
				content := interruptedAssistantContent(err, lastToolResult)
				state.AddAssistantMessage(content)
				emitStats()
				logger.Warn("agentloop: complete_on_deadline",
					"iteration", iteration,
					"finalizing", finalizing,
					"tool_calls_executed", globalToolCallsExecuted,
				)
				return Result{Text: content, Stats: stats}, nil
			}
			return Result{Text: "Sorry, I couldn't process your message. Please try again.", Stats: stats}, err
		}
		if opts.OnLLMDelta != nil && resp.Response.Content != "" {
			opts.OnLLMDelta(resp.Response.Content)
		}

		state.TrackTokens(resp.Response.Usage)
		stats.TokensPrompt += resp.Response.Usage.PromptTokens
		stats.TokensCompletion += resp.Response.Usage.CompletionTokens
		stats.TokensTotal += resp.Response.Usage.TotalTokens
		if opts.EstimateCost != nil {
			stats.CostUSD += opts.EstimateCost(resp.Response.Usage)
		}
		if opts.RecordUsage != nil {
			opts.RecordUsage(resp.Response.Usage)
		}

		if !resp.Response.HasToolCalls {
			response := strings.TrimSpace(resp.Response.Content)
			if response == "" {
				response = finalAnswerOnBudget(lastToolResult)
			}
			// Phantom-tool guard: when the response narrates tool actions
			// but emitted no tool_use, give the model exactly one chance
			// per turn to either invoke the tools or correct itself.
			// Requires both an enabled guard and a State that implements
			// PhantomCorrector (conversation.Context does).
			if opts.PhantomToolGuard != nil &&
				stats.PhantomToolDetections < opts.PhantomToolGuard.RetriesAllowed() &&
				opts.PhantomToolGuard.LooksPhantom(response, false, calledThisTurn) {
				if corrector, ok := state.(PhantomCorrector); ok {
					stats.PhantomToolDetections++
					logger.Warn("agentloop: phantom_tool_detected",
						"iteration", iteration,
						"detections_so_far", stats.PhantomToolDetections,
						"retries_allowed", opts.PhantomToolGuard.RetriesAllowed(),
					)
					state.AddAssistantMessage(response)
					corrector.AddUserMessage(opts.PhantomToolGuard.CorrectionText())
					emitStats()
					continue
				}
				// Guard wired but state can't accept correction → log
				// and fall through (commit the phantom reply as-is).
				logger.Warn("agentloop: phantom_tool_detected_uncorrectable",
					"iteration", iteration,
					"reason", "state_lacks_AddUserMessage",
				)
			}
			state.AddAssistantMessage(response)
			emitStats()
			iterCancel()
			if resp.Delivered {
				return Result{Delivered: true, Stats: stats}, nil
			}
			return Result{Text: response, Stats: stats}, nil
		}

		// Phantom guard ROI metric: if the previous iteration tripped the
		// guard and THIS iteration actually emitted tool calls, the
		// correction worked. Count once per detection that landed a
		// recovery — capped at PhantomToolDetections so a single retry
		// can't inflate the metric.
		if opts.PhantomToolGuard != nil &&
			stats.PhantomToolDetections > 0 &&
			stats.PhantomToolCorrected < stats.PhantomToolDetections {
			stats.PhantomToolCorrected++
			logger.Info("agentloop: phantom_tool_corrected",
				"iteration", iteration,
				"corrected_total", stats.PhantomToolCorrected,
			)
		}
		state.AddAssistantToolCallMessage(resp.Response.Content, resp.Response.ToolCalls)
		stats.ToolCalls += len(resp.Response.ToolCalls)
		for _, call := range resp.Response.ToolCalls {
			stats.ToolsCalled = append(stats.ToolsCalled, call.Name)
			calledThisTurn[call.Name] = true
			switch call.Name {
			case "file":
				// Wave 2.7e: read_file consolidated under file(action=read).
				// Detect a SKILL.md read for the SkillsRead stat.
				if action, _ := call.Arguments["action"].(string); action == "read" {
					if skill := skillNameFromReadFileArgs(call.Arguments); skill != "" {
						stats.ReadSkills = appendUniqueStrings(stats.ReadSkills, skill)
						stats.SkillsRead = true
					}
				}
			case "run_aurabot_swarm":
				stats.SwarmUsed = true
			case "execute_code", "execute_shell":
				stats.SandboxUsed = true
			}
		}
		emitStats()

		// Permissive load: the model may call a tool by name that isn't in
		// the current pool because it saw the name in the system-prompt
		// manifest. Resolve and add to the pool before dispatch so the
		// executor sees a normal tool call. Unknown names fall through to
		// the executor where the registry surfaces a clean error.
		if pool != nil && pool.resolver != nil {
			for _, call := range resp.Response.ToolCalls {
				pool.EnsureLoaded(call.Name)
			}
		}

		var callsToExecute, duplicateToolCalls []llm.ToolCall
		if opts.DisableInBatchDedup {
			callsToExecute = resp.Response.ToolCalls
		} else {
			callsToExecute, duplicateToolCalls = DedupeToolCalls(resp.Response.ToolCalls)
		}
		inBatchDuplicate := map[string]bool{}
		for _, call := range duplicateToolCalls {
			inBatchDuplicate[duplicateToolCallKey(call)] = true
		}
		var freshCalls []llm.ToolCall
		skippedToolResults := map[string]string{}
		maxCallsHit := map[string]bool{}
		budgetCapHit := map[string]bool{}
		for _, call := range callsToExecute {
			key := duplicateToolCallKey(call)
			stateForCall := ToolCallState{
				InBatchDuplicate: inBatchDuplicate[key],
				PriorIdentical:   seenToolCalls[key],
				CallsForTool:     toolCallExecutions[call.Name],
			}
			if opts.BeforeTool != nil {
				if decision := opts.BeforeTool(call, stateForCall); decision.Skip {
					if decision.Result != "" {
						skippedToolResults[call.ID] = decision.Result
					}
					seenToolCalls[key] = true
					toolCallExecutions[call.Name]++
					duplicateToolCalls = append(duplicateToolCalls, call)
					continue
				}
			} else if seenToolCalls[key] && !IsRetryableToolResult(seenToolCallsResult[key]) {
				// Sticky dedupe only blocks when the previous identical call
				// produced a real result. An empty or "Error: ..." result is
				// treated as transient so the LLM can retry (F-006).
				duplicateToolCalls = append(duplicateToolCalls, call)
				continue
			} else if maxCalls := opts.MaxCallsPerTool[call.Name]; maxCalls > 0 && toolCallExecutions[call.Name] >= maxCalls {
				duplicateToolCalls = append(duplicateToolCalls, call)
				maxCallsHit[call.ID] = true
				continue
			}
			// Per-Run tool budget check runs regardless of BeforeTool — a
			// caller that provides BeforeTool (e.g. agent.Runner) still
			// wants MaxToolCalls enforced. Separated from the else-if chain
			// so it applies both when BeforeTool returned allow AND when it
			// was nil and neither sticky-dedup nor MaxCallsPerTool fired.
			if opts.MaxToolCalls > 0 && globalToolCallsExecuted >= opts.MaxToolCalls {
				duplicateToolCalls = append(duplicateToolCalls, call)
				budgetCapHit[call.ID] = true
				continue
			}
			seenToolCalls[key] = true
			toolCallExecutions[call.Name]++
			globalToolCallsExecuted++
			freshCalls = append(freshCalls, call)
		}
		stats.DuplicateToolCall = stats.DuplicateToolCall || len(duplicateToolCalls) > 0
		var execution ExecutionSummary
		if len(freshCalls) > 0 {
			toolNames := make([]string, 0, len(freshCalls))
			for _, call := range freshCalls {
				toolNames = append(toolNames, call.Name)
			}
			logger.Debug("agentloop: dispatch_tools",
				"iteration", iteration,
				"tools", toolNames,
				"duplicates", len(duplicateToolCalls),
			)
			// Fire OnToolStart for every fresh call BEFORE dispatch. The
			// batch may execute calls in parallel inside the executor, so
			// the start callbacks come out as a single ordered run of
			// (start, start, ...) followed by (end, end, ...) — adapters
			// that want strict 1:1 pairing should match by callID.
			if opts.OnToolStart != nil {
				for _, call := range freshCalls {
					opts.OnToolStart(call, argKeysFromCall(call))
				}
			}
			toolBatchStart := time.Now()
			execution = executor.ExecuteToolCalls(iterCtx, freshCalls)
			stats.ToolCallsExecuted += len(freshCalls)
			toolBatchElapsed := time.Since(toolBatchStart)
			if opts.OnToolEnd != nil {
				for _, call := range freshCalls {
					result := execution.Results[call.ID]
					success := !strings.HasPrefix(strings.TrimSpace(result), "Error:")
					opts.OnToolEnd(call.ID, call.Name, success, toolBatchElapsed, toolResultPreview(result))
				}
			}
			lastToolResult = execution.LastResult
			stats.ReadSkills = appendUniqueStrings(stats.ReadSkills, execution.ReadSkillNames...)
			stats.SkillsRead = stats.SkillsRead || len(stats.ReadSkills) > 0
			if execution.TerminalTool != "" {
				stats.TerminalTool = execution.TerminalTool
			}
			// Cache last-result-per-key for the next iteration's dedupe gate
			// (F-006). Executors that don't populate Results fall through to
			// the original sticky behaviour because the map lookup returns
			// "" and IsRetryableToolResult("") is true — which means a
			// retry-tolerant default. Operators wanting strict dedupe must
			// either populate Results or set MaxCallsPerTool.
			for _, call := range freshCalls {
				key := duplicateToolCallKey(call)
				if result, ok := execution.Results[call.ID]; ok {
					seenToolCallsResult[key] = result
				}
				// Deferred-tools absorption: when the executor returns a
				// tool_search result, parse the hits and pre-load each
				// named tool into the pool so the next iteration's LLM
				// step sees them in its tools array. No-op when pool or
				// resolver is nil — falls back to legacy behaviour.
				if call.Name == "tool_search" {
					if result, ok := execution.Results[call.ID]; ok {
						loaded := pool.AbsorbToolSearchResult(result)
						if loaded > 0 {
							logger.Debug("agentloop: tool_search_absorbed", "iteration", iteration, "loaded", loaded)
						}
					}
				}
			}
		}
		for _, duplicate := range duplicateToolCalls {
			if result := skippedToolResults[duplicate.ID]; result != "" {
				// BeforeTool-supplied skip messages are Aura-authored — no
				// envelope. Wrap only the canned duplicate stub if the source
				// tool is untrusted.
				state.AddToolResultMessage(duplicate.ID, result)
				continue
			}
			stub := duplicateToolResult(duplicate, opts)
			if maxCallsHit[duplicate.ID] {
				stub = maxCallsToolResult(duplicate)
			}
			if budgetCapHit[duplicate.ID] {
				stub = budgetCapToolResult(opts.MaxToolCalls)
			}
			state.AddToolResultMessage(duplicate.ID, WrapUntrustedToolResult(duplicate.Name, stub))
		}
		// Per-Run tool budget transition: when the cap has just been reached,
		// flip into finalizing mode and (when State supports it) inject a
		// user-side instruction telling the model to finish from existing
		// evidence. Mirrors agent.Runner runner.go:289-292. Fires once per
		// Run — subsequent iterations already see finalizing=true.
		if !finalizing && opts.MaxToolCalls > 0 && globalToolCallsExecuted >= opts.MaxToolCalls {
			finalizing = true
			if corrector, ok := state.(PhantomCorrector); ok {
				corrector.AddUserMessage(toolBudgetFinalInstruction(opts.MaxToolCalls))
			} else {
				logger.Warn("agentloop: tool_budget_finalize_no_corrector",
					"iteration", iteration,
					"max_tool_calls", opts.MaxToolCalls,
					"reason", "state_lacks_AddUserMessage",
				)
			}
		}
		emitStats()

		if execution.FatalResult != "" {
			state.AddAssistantMessage(execution.FatalResult)
			iterCancel()
			return Result{Text: execution.FatalResult, Stats: stats}, nil
		}
		if opts.TerminalToolPolicy && execution.TerminalTool != "" && opts.AllowNoToolFinalization && opts.TerminalHandler != nil {
			logger.Debug("agentloop: terminal_handler", "iteration", iteration, "tool", execution.TerminalTool)
			response, delivered, handled := opts.TerminalHandler(iterCtx, execution.TerminalTool, lastToolResult, &stats)
			emitStats()
			if handled {
				iterCancel()
				return Result{Text: response, Delivered: delivered, Stats: stats}, nil
			}
		}
	}

	stats.MaxIterationsHit = true
	stats.StopReason = "max_iterations_hit"
	logger.Warn("agentloop: max_iterations_hit", "iterations", opts.MaxIterations, "elapsed_ms", time.Since(start).Milliseconds(), "tools_called", len(stats.ToolsCalled))
	if iterCancel != nil {
		iterCancel()
		iterCancel = nil
	}
	if opts.AllowNoToolFinalization {
		if answer, ok := finalizeAnswerAfterBudget(ctx, client, state, opts, &stats); ok {
			state.AddAssistantMessage(answer)
			emitStats()
			return Result{Text: answer, Stats: stats}, nil
		}
	}
	answer := finalAnswerOnBudget(lastToolResult)
	state.AddAssistantMessage(answer)
	emitStats()
	return Result{Text: answer, Stats: stats}, nil
}

// finalizeAnswerAfterBudget asks the model to produce a natural-language
// answer from the context already collected, without any new tool calls.
// Used when MaxIterations is reached so the user gets a summary instead of
// the raw tail of a tool result.
func finalizeAnswerAfterBudget(ctx context.Context, client ChatClient, state State, opts Options, stats *Stats) (string, bool) {
	if client == nil || state == nil || stats == nil {
		return "", false
	}
	// Apply the same governance the main loop uses — microcompact long tool
	// results, truncate oversized payloads, drop orphan tool messages. Without
	// this the finalize call sends the entire accumulated context to the LLM
	// exactly at the moment that context is largest, often blowing the token
	// budget or burning cost for no extra signal (F-010).
	messages := applyGovernance(state.Messages(), opts.MaxToolResultChars, opts.MicrocompactKeepRecent, opts.MicrocompactMinChars)
	messages = append(messages, llm.Message{
		Role:    "user",
		Content: "You reached the per-turn tool budget. Do not call any more tools. Answer the user naturally using the evidence above. Do not paste raw JSON, tool names, scores, or internal IDs. If the evidence is insufficient, say so plainly in one sentence.",
	})
	stats.LLMCalls++
	stats.LoopSteps++
	resp, err := client.Chat(ctx, messages, nil)
	if err != nil {
		return "", false
	}
	state.TrackTokens(resp.Response.Usage)
	stats.TokensPrompt += resp.Response.Usage.PromptTokens
	stats.TokensCompletion += resp.Response.Usage.CompletionTokens
	stats.TokensTotal += resp.Response.Usage.TotalTokens
	if opts.EstimateCost != nil {
		stats.CostUSD += opts.EstimateCost(resp.Response.Usage)
	}
	if opts.RecordUsage != nil {
		opts.RecordUsage(resp.Response.Usage)
	}
	text := strings.TrimSpace(resp.Response.Content)
	if text == "" || resp.Response.HasToolCalls {
		return "", false
	}
	return text, true
}

// duplicateToolResult returns the synthetic message the model sees when a
// tool call is suppressed. The phrasing matters: the previous "duplicate
// tool call X skipped" wording was opaque enough that the model often
// just retried the same call again, producing the 4-5-stub loop the
// user flagged on 2026-05-11 ("duplicate stub poco chiaro fa loopare il
// modello"). Rephrase as a direct instruction with the next action the
// model is supposed to take.
func duplicateToolResult(call llm.ToolCall, opts Options) string {
	if opts.DuplicateToolResult != nil {
		return opts.DuplicateToolResult(call)
	}
	return fmt.Sprintf("You already called %s with these exact arguments earlier this turn. The result is above — read it instead of calling again. If you need different data, call with different arguments or pick another tool.", call.Name)
}

// maxCallsToolResult is the stub emitted when MaxCallsPerTool gates a fresh
// call. The arguments are different from any earlier call — what triggered
// the skip is the per-turn budget for the tool, not duplication — so the
// model needs a different message than duplicateToolResult.
func maxCallsToolResult(call llm.ToolCall) string {
	return fmt.Sprintf("You have hit the per-turn call budget for %s. Move on with what you already have, or call a different tool.", call.Name)
}

// budgetCapToolResult is the stub emitted when the per-Run tool budget
// (Options.MaxToolCalls) is exhausted. Distinct from maxCallsToolResult
// because the gate is the TOTAL count, not per-tool: pointing the model
// at a different tool would not help. Mirrors agent.Runner
// skippedToolOutcomes phrasing for swarm-worker semantics parity.
func budgetCapToolResult(maxToolCalls int) string {
	return fmt.Sprintf("Tool call skipped: per-Run tool budget (%d) reached. Return a concise partial result from the evidence already gathered.", maxToolCalls)
}

// toolBudgetFinalInstruction is the user-side prod injected when the per-Run
// tool budget is hit. Tells the model to stop calling tools and finalize
// the answer from the evidence above. Mirrors agent.Runner
// toolBudgetFinalInstruction so PHASE B can swap in without behavior drift.
func toolBudgetFinalInstruction(maxToolCalls int) string {
	return fmt.Sprintf("Tool budget reached (%d calls). Do not call tools again. Finish the assigned work now with a concise final report from the evidence above: answer, evidence/URLs when available, gaps, and next action.", maxToolCalls)
}

// interruptedAssistantContent renders the final assistant text returned by
// CompleteOnDeadline when the LLM Chat call dies on context.DeadlineExceeded
// AFTER at least one tool call has run. The error is included verbatim so
// downstream telemetry can distinguish "deadline mid-turn" from "natural
// completion". Mirrors agent.Runner interruptedContent so PHASE B swaps in
// without user-visible diff.
func interruptedAssistantContent(err error, lastToolResult string) string {
	content := fmt.Sprintf("Agent interrupted before a final answer: %v.", err)
	if trimmed := strings.TrimSpace(lastToolResult); trimmed != "" {
		content += "\n\nLast tool result:\n" + trimmed
	}
	return content
}

func DuplicateOrMaxCallsPolicy(maxCallsPerTool map[string]int, result func(llm.ToolCall, ToolCallState) string) BeforeToolCallback {
	return func(call llm.ToolCall, state ToolCallState) ToolCallDecision {
		limitReached := false
		if maxCalls := maxCallsPerTool[call.Name]; maxCalls > 0 && state.CallsForTool >= maxCalls {
			limitReached = true
		}
		if !state.PriorIdentical && !limitReached {
			return ToolCallDecision{}
		}
		out := ""
		if result != nil {
			out = result(call, state)
		}
		if out == "" {
			if limitReached {
				out = fmt.Sprintf("You have hit the per-turn call budget for %s. Move on with what you already have, or call a different tool.", call.Name)
			} else {
				out = fmt.Sprintf("You already called %s with these exact arguments earlier this turn. Re-read the previous result above. If you need different data, change the arguments or pick another tool.", call.Name)
			}
		}
		return ToolCallDecision{Skip: true, Result: out}
	}
}

func skillNameFromReadFileArgs(args map[string]any) string {
	value, ok := args["path"]
	if !ok {
		return ""
	}
	path := strings.TrimSpace(fmt.Sprint(value))
	if path == "" {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) < 2 || parts[len(parts)-1] != "SKILL.md" {
		return ""
	}
	name := strings.TrimSpace(parts[len(parts)-2])
	if name == "" || strings.EqualFold(name, "skills") {
		return ""
	}
	return name
}

// finalAnswerOnBudget produces a fallback answer when the model returned no
// content of its own. If the last tool result is non-empty we surface it; the
// previous regex-based "this looks like raw tool evidence" scrubber is gone —
// it tried to detect leaks of JSON/score/exit_code formatting in the assistant
// reply, but the right fix is for tools to return rendered text (Step 1) and
// for the prompt to instruct the model not to echo raw output.
func finalAnswerOnBudget(lastToolResult string) string {
	if result := strings.TrimSpace(lastToolResult); result != "" {
		return result
	}
	return "I reached the per-turn budget without a usable result."
}

// argKeys returns the sorted set of top-level argument key names from a
// tool call's Arguments map. Values are intentionally dropped — the
// CLAUDE.md value-leakage policy lets us log/emit key names but never
// the data behind them. Returns an empty slice (never nil) so callers
// can append safely.
//
// Note: this is the agentloop-local helper. internal/tools has a sibling
// argKeys() that additionally redacts credential-shaped names (api_key,
// password, etc.). The agentloop layer keeps the names verbatim because
// outbound chathub adapters may want to render them and downstream
// redaction is the right layer for that policy. Plain key names alone
// never leak the data inside the value.
func argKeysFromCall(call llm.ToolCall) []string {
	keys := make([]string, 0, len(call.Arguments))
	for k := range call.Arguments {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// toolResultPreview caps a tool result string at MaxToolResultPreviewChars
// runes (NOT bytes, to avoid splitting UTF-8 mid-codepoint). The point is
// "enough to disambiguate success/error in logs", not "round-trip the
// result"; full results live in the conversation state.
func toolResultPreview(result string) string {
	if len(result) <= MaxToolResultPreviewChars {
		return result
	}
	runes := []rune(result)
	if len(runes) <= MaxToolResultPreviewChars {
		return result
	}
	return string(runes[:MaxToolResultPreviewChars])
}

// MaxToolResultPreviewChars caps the preview string emitted on OnToolEnd
// (and downstream EventToolEnd). Tuned for log readability rather than
// faithful reproduction of the tool output — the full result lives in
// the state's tool-result message and is governed by MaxToolResultChars.
const MaxToolResultPreviewChars = 200

func appendUniqueStrings(values []string, additions ...string) []string {
	for _, addition := range additions {
		addition = strings.TrimSpace(addition)
		if addition == "" {
			continue
		}
		if !slices.Contains(values, addition) {
			values = append(values, addition)
		}
	}
	return values
}
