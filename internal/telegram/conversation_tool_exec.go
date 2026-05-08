package telegram

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/aura/aura/internal/agentloop"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/orchestration"
	"github.com/aura/aura/internal/tools"

	tele "gopkg.in/telebot.v4"
)

// executeToolCalls runs an assistant turn's tool calls concurrently and
// appends results in original order. The LLM batches independent calls into
// one assistant turn (e.g. search_files + web_search side-by-side); running
// them sequentially serialized N round-trips of latency for no reason.
//
// Concurrency safety: Registry.Execute is RWMutex-guarded for lookup, and
// individual tools run outside the lock. Wiki/source writes serialize on
// SQLite at the storage layer. Tool activity stays in logs/orchestration
// telemetry; Telegram receives only the final user-facing synthesis so normal
// users are not exposed to tool names, JSON payloads, or raw execution traces.
//
// Returns the last result content (in original order), used by the caller
// as a fallback when the model returns an empty final response.
type toolExecutionSummary struct {
	lastResult     string
	fatalResult    string
	hiddenRejected bool
	readSkillNames []string
	terminalTool   string
}

func (b *Bot) executeToolCalls(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, calls []llm.ToolCall, toolsExposed []string, toolset orchestration.Toolset, readSkills []string, afterTool agentloop.AfterToolCallback) toolExecutionSummary {
	if len(calls) == 0 {
		return toolExecutionSummary{}
	}

	type outcome struct {
		id             string
		tool           string
		content        string
		elapsed        time.Duration
		errorClass     string
		hiddenRejected bool
		fatal          bool
		readSkillName  string
		terminalTool   string
	}
	results := make([]outcome, len(calls))

	var wg sync.WaitGroup
	var summaryMu sync.Mutex
	summary := toolExecutionSummary{}
	loopPolicy, _ := orchestration.LoopPolicyForToolset(toolset)
	for i, tc := range calls {
		wg.Add(1)
		go func(i int, tc llm.ToolCall) {
			defer wg.Done()
			hooks := orchestration.EnsureHooks(b.orchHooks)
			event := orchestration.TraceEvent{
				ToolName:     tc.Name,
				ToolsExposed: toolsExposed,
				Toolset:      string(toolset),
			}
			start := time.Now()
			if !toolAllowed(tc.Name, toolsExposed) {
				event.HiddenToolRejected = true
				event.DurationMS = time.Since(start).Milliseconds()
				event.ErrorClass = "hidden_tool"
				event.ResultSizeBytes = 0
				results[i] = outcome{
					id:             tc.ID,
					tool:           tc.Name,
					content:        tools.FormatToolError(fmt.Errorf("tool %q is not available in this runtime; choose another exposed tool or answer from current context", tc.Name)),
					elapsed:        time.Since(start),
					errorClass:     "hidden_tool",
					hiddenRejected: true,
				}
				summaryMu.Lock()
				summary.hiddenRejected = true
				summaryMu.Unlock()
				hooks.AfterToolCall(event)
				return
			}
			if err := hooks.BeforeToolCall(event); err != nil {
				errorClass := "policy_error"
				content := tools.FormatFatalToolError(err)
				hidden := errors.Is(err, orchestration.ErrHiddenTool)
				if hidden {
					errorClass = "hidden_tool"
					content = tools.FormatFatalToolError(fmt.Errorf("tool %q is not exposed in the active toolset", tc.Name))
				}
				event.HiddenToolRejected = hidden
				event.DurationMS = time.Since(start).Milliseconds()
				event.ErrorClass = errorClass
				results[i] = outcome{
					id:             tc.ID,
					tool:           tc.Name,
					content:        content,
					elapsed:        time.Since(start),
					errorClass:     errorClass,
					hiddenRejected: hidden,
					fatal:          true,
				}
				summaryMu.Lock()
				summary.hiddenRejected = summary.hiddenRejected || hidden
				summaryMu.Unlock()
				hooks.AfterToolCall(event)
				return
			}
			toolCtx := tools.WithUserID(ctx, userID)
			args := toolArgumentsForToolset(tc.Name, tc.Arguments, toolset)
			result, err := b.tools.Execute(toolCtx, tc.Name, args)
			if err != nil {
				result = tools.FormatToolError(err)
				b.logger.Warn("tool call failed", "user_id", userID, "tool", tc.Name, "error", err)
			}
			if afterTool != nil {
				result = afterTool(tc, result, err)
			}
			errorClass := ""
			if err != nil {
				errorClass = "tool_error"
			}
			event.DurationMS = time.Since(start).Milliseconds()
			event.ResultSizeBytes = len(result)
			event.ErrorClass = errorClass
			usage, cost := toolResultUsage(result, b.cfg)
			event.TokensPrompt = usage.PromptTokens
			event.TokensCompletion = usage.CompletionTokens
			event.TokensTotal = usage.TotalTokens
			event.CostUSD = cost
			readSkillName := ""
			if err == nil {
				switch tc.Name {
				case "read_file":
					readSkillName = skillNameFromReadFileArgs(tc.Arguments)
				}
			}
			terminalTool := ""
			if b.terminalToolPolicyEnabled() && toolAllowed(tc.Name, loopPolicy.TerminalTools) {
				terminalTool = tc.Name
			}
			results[i] = outcome{
				id:            tc.ID,
				tool:          tc.Name,
				content:       result,
				elapsed:       time.Since(start),
				errorClass:    errorClass,
				readSkillName: readSkillName,
				terminalTool:  terminalTool,
			}
			hooks.AfterToolCall(event)
		}(i, tc)
	}
	wg.Wait()

	for _, r := range results {
		convCtx.AddToolResultMessage(r.id, r.content)
		summary.lastResult = r.content
		if r.hiddenRejected {
			summary.hiddenRejected = true
		}
		if r.fatal && summary.fatalResult == "" {
			summary.fatalResult = r.content
		}
		if r.readSkillName != "" {
			summary.readSkillNames = appendUniqueStrings(summary.readSkillNames, r.readSkillName)
		}
		if summary.terminalTool == "" && r.terminalTool != "" {
			summary.terminalTool = r.terminalTool
		}
	}
	return summary
}

func toolArgumentsForToolset(name string, args map[string]any, toolset orchestration.Toolset) map[string]any {
	if toolset != orchestration.ToolsetDocument || name != "search_memory" {
		return args
	}
	out := make(map[string]any, len(args)+1)
	for k, v := range args {
		out[k] = v
	}
	if value, ok := out["limit"]; !ok || numericToolArg(value) > 3 {
		out["limit"] = float64(3)
	}
	return out
}

func numericToolArg(value any) float64 {
	switch v := value.(type) {
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case float64:
		return v
	case float32:
		return float64(v)
	default:
		return 0
	}
}
