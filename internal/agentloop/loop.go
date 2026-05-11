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
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
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
	MaxIterations           int
	MaxElapsed              time.Duration
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
	TerminalHandler         TerminalHandler
	// MaxToolResultChars caps each tool message size before going to the
	// LLM. Zero uses DefaultMaxToolResultChars (8 KB). Operators tune this
	// via the MAX_TOOL_RESULT_CHARS env var.
	MaxToolResultChars int
	// MicrocompactKeepRecent / MicrocompactMinChars control the rolling
	// compaction of read_file/exec/web_* tool results. Zero values use the
	// package defaults (10 / 500). Operators tune via env.
	MicrocompactKeepRecent int
	MicrocompactMinChars   int
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
	emitStats := func() {
		if opts.OnStats != nil {
			opts.OnStats(stats)
		}
	}
	emitStats()
	logger.Debug("agentloop: run start", "max_iterations", opts.MaxIterations, "max_elapsed_ms", opts.MaxElapsed.Milliseconds())

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
		if opts.BeforeLLM != nil {
			if message, stop := opts.BeforeLLM(); stop {
				stats.StopReason = "before_llm"
				logger.Info("agentloop: before_llm_stop", "iteration", iteration)
				iterCancel()
				return Result{Text: message, Stats: stats}, nil
			}
		}

		tools := opts.Tools
		if opts.ToolsProvider != nil {
			tools = opts.ToolsProvider()
		}

		stats.LLMCalls++
		stats.LoopSteps++
		messagesForModel := applyGovernance(state.Messages(), opts.MaxToolResultChars, opts.MicrocompactKeepRecent, opts.MicrocompactMinChars)
		resp, err := client.Chat(iterCtx, messagesForModel, tools)
		if err != nil {
			iterCancel()
			return Result{Text: "Sorry, I couldn't process your message. Please try again.", Stats: stats}, err
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
			state.AddAssistantMessage(response)
			emitStats()
			iterCancel()
			if resp.Delivered {
				return Result{Delivered: true, Stats: stats}, nil
			}
			return Result{Text: response, Stats: stats}, nil
		}

		state.AddAssistantToolCallMessage(resp.Response.Content, resp.Response.ToolCalls)
		stats.ToolCalls += len(resp.Response.ToolCalls)
		for _, call := range resp.Response.ToolCalls {
			stats.ToolsCalled = append(stats.ToolsCalled, call.Name)
			switch call.Name {
			case "read_file":
				if skill := skillNameFromReadFileArgs(call.Arguments); skill != "" {
					stats.ReadSkills = appendUniqueStrings(stats.ReadSkills, skill)
					stats.SkillsRead = true
				}
			case "run_aurabot_swarm":
				stats.SwarmUsed = true
			case "execute_code", "execute_shell":
				stats.SandboxUsed = true
			}
		}
		emitStats()

		callsToExecute, duplicateToolCalls := DedupeToolCalls(resp.Response.ToolCalls)
		inBatchDuplicate := map[string]bool{}
		for _, call := range duplicateToolCalls {
			inBatchDuplicate[duplicateToolCallKey(call)] = true
		}
		var freshCalls []llm.ToolCall
		skippedToolResults := map[string]string{}
		maxCallsHit := map[string]bool{}
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
			seenToolCalls[key] = true
			toolCallExecutions[call.Name]++
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
			execution = executor.ExecuteToolCalls(iterCtx, freshCalls)
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
			state.AddToolResultMessage(duplicate.ID, WrapUntrustedToolResult(duplicate.Name, stub))
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
