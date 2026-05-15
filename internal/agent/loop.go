// loop.go runs one Telegram-style assistant turn: alternating LLM calls and
// tool execution until the model produces a final answer or the per-turn
// budget is exhausted. Merged from the former internal/agentloop package.
package agent

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

	governance "github.com/aura/aura/internal/agent/governance"
	tools "github.com/aura/aura/internal/agent/tools/registry"
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

// IsRetryableToolResult reports whether a prior tool-result text leaves the
// loop willing to re-execute the same call.
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
	// false (dedup ON). Background agents such as Runner set this to true to
	// preserve the old Runner semantics: budget enforcement via MaxToolCalls,
	// not dedup-then-budget.
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
	// Logger is an optional structured logger. When nil the loop falls back to
	// slog.Default().
	Logger *slog.Logger
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
}

// MaxIterationsCeiling is a hard upper bound on opts.MaxIterations.
const MaxIterationsCeiling = 50

// DefaultMaxElapsed is the implicit per-turn wall-clock cap when callers
// leave opts.MaxElapsed at zero.
const DefaultMaxElapsed = 5 * time.Minute

func runLoop(ctx context.Context, client ChatClient, executor ToolExecutor, state State, opts Options) (loopResult, error) {
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
	globalToolCallsExecuted := 0
	finalizing := false
	calledThisTurn := map[string]bool{}
	emitStats := func() {
		if opts.OnStats != nil {
			opts.OnStats(stats)
		}
	}
	emitStats()
	logger.Debug("agentloop: run start", "max_iterations", opts.MaxIterations, "max_elapsed_ms", opts.MaxElapsed.Milliseconds())

	var pool *toolPool

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
			return loopResult{Text: answer, Stats: stats}, nil
		}
		if iterCancel != nil {
			iterCancel()
		}
		var iterCtx context.Context
		iterCtx, iterCancel = context.WithTimeout(ctx, remaining)
		if finalizing && opts.CompleteOnDeadline && opts.FinalizationTimeout > 0 {
			iterCancel()
			iterCtx, iterCancel = context.WithTimeout(ctx, opts.FinalizationTimeout)
		}
		if opts.BeforeLLM != nil {
			if message, stop := opts.BeforeLLM(); stop {
				stats.StopReason = "before_llm"
				logger.Info("agentloop: before_llm_stop", "iteration", iteration)
				iterCancel()
				return loopResult{Text: message, Stats: stats}, nil
			}
		}

		if pool == nil {
			seed := opts.Tools
			if opts.ToolsProvider != nil {
				seed = opts.ToolsProvider()
			}
			pool = newToolPool(seed, opts.ToolResolver)
		}
		tools := pool.Defs()
		if finalizing {
			tools = nil
		}

		stats.LLMCalls++
		stats.LoopSteps++
		messagesForModel := governance.Apply(state.Messages(), opts.MaxToolResultChars, opts.MicrocompactKeepRecent, opts.MicrocompactMinChars)
		if opts.OnLLMStart != nil {
			opts.OnLLMStart(iteration, len(messagesForModel), len(tools))
		}
		resp, err := client.Chat(iterCtx, messagesForModel, tools)
		if err != nil {
			iterCancel()
			if opts.CompleteOnDeadline && errors.Is(err, context.DeadlineExceeded) && globalToolCallsExecuted > 0 {
				content := interruptedAssistantContent(err, lastToolResult)
				state.AddAssistantMessage(content)
				emitStats()
				logger.Warn("agentloop: complete_on_deadline",
					"iteration", iteration,
					"finalizing", finalizing,
					"tool_calls_executed", globalToolCallsExecuted,
				)
				return loopResult{Text: content, Stats: stats}, nil
			}
			return loopResult{Text: "Sorry, I couldn't process your message. Please try again.", Stats: stats}, err
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
				logger.Warn("agentloop: phantom_tool_detected_uncorrectable",
					"iteration", iteration,
					"reason", "state_lacks_AddUserMessage",
				)
			}
			state.AddAssistantMessage(response)
			emitStats()
			iterCancel()
			if resp.Delivered {
				return loopResult{Delivered: true, Stats: stats}, nil
			}
			return loopResult{Text: response, Stats: stats}, nil
		}

		if opts.PhantomToolGuard != nil &&
			stats.PhantomToolDetections > 0 &&
			stats.PhantomToolCorrected < stats.PhantomToolDetections {
			stats.PhantomToolCorrected++
			logger.Info("agentloop: phantom_tool_corrected",
				"iteration", iteration,
				"corrected_total", stats.PhantomToolCorrected,
			)
		}
		// ask_user exclusive semantics: when ask_user is in the batch, keep
		// only that call in the state so the resume sees a clean single
		// pending tool_call without orphaned unresolved stubs for other calls.
		toolCallsForState := resp.Response.ToolCalls
		if askUserIdx := findAskUserCall(resp.Response.ToolCalls); askUserIdx >= 0 {
			toolCallsForState = resp.Response.ToolCalls[askUserIdx : askUserIdx+1]
		}
		state.AddAssistantToolCallMessage(resp.Response.Content, toolCallsForState)
		stats.ToolCalls += len(resp.Response.ToolCalls)
		for _, call := range resp.Response.ToolCalls {
			stats.ToolsCalled = append(stats.ToolsCalled, call.Name)
			calledThisTurn[call.Name] = true
			switch call.Name {
			case "file":
				if action, _ := call.Arguments["action"].(string); action == "read" {
					if skill := SkillNameFromReadFileArgs(call.Arguments); skill != "" {
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
				duplicateToolCalls = append(duplicateToolCalls, call)
				continue
			} else if maxCalls := opts.MaxCallsPerTool[call.Name]; maxCalls > 0 && toolCallExecutions[call.Name] >= maxCalls {
				duplicateToolCalls = append(duplicateToolCalls, call)
				maxCallsHit[call.ID] = true
				continue
			}
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

		// ask_user exclusive semantics: if ask_user is among the fresh calls,
		// dispatch only it and discard the rest of the batch silently (they
		// will re-emit on the next LLM turn after resume).
		if idx := findAskUserCall(freshCalls); idx >= 0 {
			freshCalls = freshCalls[idx : idx+1]
		}

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
			for _, call := range freshCalls {
				key := duplicateToolCallKey(call)
				if result, ok := execution.Results[call.ID]; ok {
					seenToolCallsResult[key] = result
				}
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

		// ask_user pause: stop the loop and signal the caller to wait for user.
		if execution.AwaitingUserInput != nil {
			stats.StopReason = "waiting_for_user"
			emitStats()
			logger.Info("agentloop: ask_user_pause",
				"iteration", iteration,
				"kind", execution.AwaitingUserInput.Kind,
				"options_count", len(execution.AwaitingUserInput.Options),
			)
			iterCancel()
			return loopResult{Stats: stats}, nil
		}

		if execution.FatalResult != "" {
			state.AddAssistantMessage(execution.FatalResult)
			iterCancel()
			return loopResult{Text: execution.FatalResult, Stats: stats}, nil
		}
		if opts.TerminalToolPolicy && execution.TerminalTool != "" && opts.AllowNoToolFinalization && opts.TerminalHandler != nil {
			logger.Debug("agentloop: terminal_handler", "iteration", iteration, "tool", execution.TerminalTool)
			response, delivered, handled := opts.TerminalHandler(iterCtx, execution.TerminalTool, lastToolResult, &stats)
			emitStats()
			if handled {
				iterCancel()
				return loopResult{Text: response, Delivered: delivered, Stats: stats}, nil
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
			return loopResult{Text: answer, Stats: stats}, nil
		}
	}
	answer := finalAnswerOnBudget(lastToolResult)
	state.AddAssistantMessage(answer)
	emitStats()
	return loopResult{Text: answer, Stats: stats}, nil
}

func finalizeAnswerAfterBudget(ctx context.Context, client ChatClient, state State, opts Options, stats *Stats) (string, bool) {
	if client == nil || state == nil || stats == nil {
		return "", false
	}
	messages := governance.Apply(state.Messages(), opts.MaxToolResultChars, opts.MicrocompactKeepRecent, opts.MicrocompactMinChars)
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

func duplicateToolResult(call llm.ToolCall, opts Options) string {
	if opts.DuplicateToolResult != nil {
		return opts.DuplicateToolResult(call)
	}
	return fmt.Sprintf("You already called %s with these exact arguments earlier this turn. The result is above — read it instead of calling again. If you need different data, call with different arguments or pick another tool.", call.Name)
}

func maxCallsToolResult(call llm.ToolCall) string {
	return fmt.Sprintf("You have hit the per-turn call budget for %s. Move on with what you already have, or call a different tool.", call.Name)
}

func budgetCapToolResult(maxToolCalls int) string {
	return fmt.Sprintf("Tool call skipped: per-Run tool budget (%d) reached. Return a concise partial result from the evidence already gathered.", maxToolCalls)
}

func toolBudgetFinalInstruction(maxToolCalls int) string {
	return fmt.Sprintf("Tool budget reached (%d calls). Do not call tools again. Finish the assigned work now with a concise final report from the evidence above: answer, evidence/URLs when available, gaps, and next action.", maxToolCalls)
}

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

func SkillNameFromReadFileArgs(args map[string]any) string {
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

func finalAnswerOnBudget(lastToolResult string) string {
	if result := strings.TrimSpace(lastToolResult); result != "" {
		return result
	}
	return "I reached the per-turn budget without a usable result."
}

func argKeysFromCall(call llm.ToolCall) []string {
	keys := make([]string, 0, len(call.Arguments))
	for k := range call.Arguments {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

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

// MaxToolResultPreviewChars caps the preview string emitted on OnToolEnd.
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

// findAskUserCall returns the index of the first ask_user call in calls, or -1.
func findAskUserCall(calls []llm.ToolCall) int {
	for i, call := range calls {
		if call.Name == "ask_user" {
			return i
		}
	}
	return -1
}
