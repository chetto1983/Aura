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
	"path/filepath"
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
type ExecutionSummary struct {
	LastResult     string
	FatalResult    string
	ReadSkillNames []string
	TerminalTool   string
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
}

func Run(ctx context.Context, client ChatClient, executor ToolExecutor, state State, opts Options) (Result, error) {
	if opts.MaxIterations < 1 {
		opts.MaxIterations = 1
	}
	start := time.Now()
	var lastToolResult string
	var stats Stats
	seenToolCalls := map[string]bool{}
	toolCallExecutions := map[string]int{}
	emitStats := func() {
		if opts.OnStats != nil {
			opts.OnStats(stats)
		}
	}
	emitStats()

	for iteration := 0; iteration < opts.MaxIterations; iteration++ {
		if opts.MaxElapsed > 0 && time.Since(start) >= opts.MaxElapsed {
			stats.MaxElapsedHit = true
			answer := finalAnswerOnBudget(lastToolResult)
			state.AddAssistantMessage(answer)
			emitStats()
			return Result{Text: answer, Stats: stats}, nil
		}
		if opts.BeforeLLM != nil {
			if message, stop := opts.BeforeLLM(); stop {
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
		resp, err := client.Chat(ctx, messagesForModel, tools)
		if err != nil {
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
			} else if seenToolCalls[key] {
				duplicateToolCalls = append(duplicateToolCalls, call)
				continue
			} else if maxCalls := opts.MaxCallsPerTool[call.Name]; maxCalls > 0 && toolCallExecutions[call.Name] >= maxCalls {
				duplicateToolCalls = append(duplicateToolCalls, call)
				continue
			}
			seenToolCalls[key] = true
			toolCallExecutions[call.Name]++
			freshCalls = append(freshCalls, call)
		}
		stats.DuplicateToolCall = stats.DuplicateToolCall || len(duplicateToolCalls) > 0
		var execution ExecutionSummary
		if len(freshCalls) > 0 {
			execution = executor.ExecuteToolCalls(ctx, freshCalls)
			lastToolResult = execution.LastResult
			stats.ReadSkills = appendUniqueStrings(stats.ReadSkills, execution.ReadSkillNames...)
			stats.SkillsRead = stats.SkillsRead || len(stats.ReadSkills) > 0
			if execution.TerminalTool != "" {
				stats.TerminalTool = execution.TerminalTool
			}
		}
		for _, duplicate := range duplicateToolCalls {
			if result := skippedToolResults[duplicate.ID]; result != "" {
				state.AddToolResultMessage(duplicate.ID, result)
				continue
			}
			state.AddToolResultMessage(duplicate.ID, duplicateToolResult(duplicate, opts))
		}
		emitStats()

		if execution.FatalResult != "" {
			state.AddAssistantMessage(execution.FatalResult)
			return Result{Text: execution.FatalResult, Stats: stats}, nil
		}
		if opts.TerminalToolPolicy && execution.TerminalTool != "" && opts.AllowNoToolFinalization && opts.TerminalHandler != nil {
			response, delivered, handled := opts.TerminalHandler(ctx, execution.TerminalTool, lastToolResult, &stats)
			emitStats()
			if handled {
				return Result{Text: response, Delivered: delivered, Stats: stats}, nil
			}
		}
	}

	stats.MaxIterationsHit = true
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
	messages := append([]llm.Message(nil), state.Messages()...)
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
	return fmt.Sprintf("duplicate tool call %q with identical arguments skipped; use the previous result already returned in this turn", call.Name)
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
			out = fmt.Sprintf("duplicate tool call %q skipped; use the previous result already returned in this turn", call.Name)
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
		if !stringSliceContains(values, addition) {
			values = append(values, addition)
		}
	}
	return values
}

func stringSliceContains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
