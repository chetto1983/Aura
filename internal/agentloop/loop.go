package agentloop

import (
	"context"
	"fmt"
	"strings"

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

type ExecutionSummary struct {
	LastResult             string
	FatalResult            string
	HiddenRejected         bool
	SkillPreflightRejected bool
	ReadSkillNames         []string
	TerminalTool           string
}

type TerminalHandler func(ctx context.Context, terminalTool, lastToolResult string, stats *Stats) (text string, delivered bool, handled bool)

type Options struct {
	MaxIterations           int
	Tools                   []llm.ToolDefinition
	TerminalToolPolicy      bool
	AllowNoToolFinalization bool
	DuplicateToolResult     func(llm.ToolCall) string
	BeforeLLM               func() (message string, stop bool)
	RecordUsage             func(llm.TokenUsage)
	EstimateCost            func(llm.TokenUsage) float64
	OnStats                 func(Stats)
	TerminalHandler         TerminalHandler
	FallbackMessage         func(lastToolResult string) string
}

type Result struct {
	Text      string
	Delivered bool
	Stats     Stats
}

type Stats struct {
	LLMCalls               int
	ToolCalls              int
	LoopSteps              int
	ToolsCalled            []string
	ReadSkills             []string
	HiddenToolRejected     bool
	SkillPreflightRejected bool
	SkillsRead             bool
	SwarmUsed              bool
	SandboxUsed            bool
	TerminalTool           string
	DuplicateToolCall      bool
	TokensPrompt           int
	TokensCompletion       int
	TokensTotal            int
	CostUSD                float64
	MaxIterationsHit       bool
}

func Run(ctx context.Context, client ChatClient, executor ToolExecutor, state State, opts Options) (Result, error) {
	if opts.MaxIterations < 1 {
		opts.MaxIterations = 1
	}
	var lastToolResult string
	var stats Stats
	emitStats := func() {
		if opts.OnStats != nil {
			opts.OnStats(stats)
		}
	}
	emitStats()

	for iteration := 0; iteration < opts.MaxIterations; iteration++ {
		if opts.BeforeLLM != nil {
			if message, stop := opts.BeforeLLM(); stop {
				return Result{Text: message, Stats: stats}, nil
			}
		}

		stats.LLMCalls++
		stats.LoopSteps++
		resp, err := client.Chat(ctx, state.Messages(), opts.Tools)
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
				if lastToolResult != "" {
					response = lastToolResult
				} else {
					response = "I completed the request but do not have anything else to add."
				}
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
			case "read_skill":
				stats.SkillsRead = true
			case "run_aurabot_swarm":
				stats.SwarmUsed = true
			case "execute_code":
				stats.SandboxUsed = true
			}
		}
		emitStats()

		callsToExecute, duplicateToolCalls := DedupeToolCalls(resp.Response.ToolCalls)
		stats.DuplicateToolCall = stats.DuplicateToolCall || len(duplicateToolCalls) > 0
		execution := executor.ExecuteToolCalls(ctx, callsToExecute)
		lastToolResult = execution.LastResult
		stats.HiddenToolRejected = stats.HiddenToolRejected || execution.HiddenRejected
		stats.SkillPreflightRejected = stats.SkillPreflightRejected || execution.SkillPreflightRejected
		stats.ReadSkills = appendUniqueStrings(stats.ReadSkills, execution.ReadSkillNames...)
		stats.SkillsRead = stats.SkillsRead || len(stats.ReadSkills) > 0
		if execution.TerminalTool != "" {
			stats.TerminalTool = execution.TerminalTool
		}
		for _, duplicate := range duplicateToolCalls {
			state.AddToolResultMessage(duplicate.ID, duplicateToolResult(duplicate, opts))
		}
		emitStats()

		if execution.FatalResult != "" {
			state.AddAssistantMessage(execution.FatalResult)
			return Result{Text: execution.FatalResult, Stats: stats}, nil
		}
		if opts.TerminalToolPolicy && execution.TerminalTool != "" && execution.TerminalTool != "run_aurabot_swarm" && opts.AllowNoToolFinalization && opts.TerminalHandler != nil {
			response, delivered, handled := opts.TerminalHandler(ctx, execution.TerminalTool, lastToolResult, &stats)
			emitStats()
			if handled {
				return Result{Text: response, Delivered: delivered, Stats: stats}, nil
			}
		}
	}

	stats.MaxIterationsHit = true
	fallback := fallbackMessage(lastToolResult, opts)
	state.AddAssistantMessage(fallback)
	emitStats()
	return Result{Text: fallback, Stats: stats}, nil
}

func duplicateToolResult(call llm.ToolCall, opts Options) string {
	if opts.DuplicateToolResult != nil {
		return opts.DuplicateToolResult(call)
	}
	return fmt.Sprintf("duplicate tool call %q with identical arguments skipped; use the previous result already returned in this turn", call.Name)
}

func fallbackMessage(lastToolResult string, opts Options) string {
	if opts.FallbackMessage != nil {
		return opts.FallbackMessage(lastToolResult)
	}
	fallback := "Mi sono fermato prima di completare una risposta finale affidabile."
	if strings.TrimSpace(lastToolResult) != "" {
		fallback += "\n\nHo completato alcuni passaggi interni, ma mi sono fermato prima di generare una risposta finale pulita."
	}
	return fallback
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
