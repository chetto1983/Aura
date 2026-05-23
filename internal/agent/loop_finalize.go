// loop_finalize.go holds the budget-exit finalization helpers used by runLoop.
// Extracted from loop.go to keep that file under the 600 LOC cap.
package agent

import (
	"context"
	"log/slog"
	"strings"

	governance "github.com/aura/aura/internal/agent/governance"
	"github.com/aura/aura/internal/llm"
)

// gracefulFinalize is the unified budget-exit handler used by all five
// stop-reason paths (US-OUT-08/09). When AllowNoToolFinalization is true it
// attempts one extra LLM round via finalizeAnswerAfterBudget using the Hermes
// synthesis prompt; on failure or when the flag is false it falls back to
// finalAnswerOnBudgetWithContext. It always adds the assistant message and
// fires emitStats before returning.
func gracefulFinalize(ctx context.Context, client ChatClient, state State, opts Options, stats *Stats, lastToolResult string, emitStats func()) (loopResult, error) {
	lastToolName := ""
	if len(stats.ToolsCalled) > 0 {
		lastToolName = stats.ToolsCalled[len(stats.ToolsCalled)-1]
	}
	var answer string
	if opts.AllowNoToolFinalization {
		if text, ok := finalizeAnswerAfterBudget(ctx, client, state, opts, stats); ok {
			answer = text
		} else {
			answer = finalAnswerOnBudgetWithContext(lastToolResult, lastToolName, stats.StopReason, opts)
		}
	} else {
		answer = finalAnswerOnBudgetWithContext(lastToolResult, lastToolName, stats.StopReason, opts)
	}
	state.AddAssistantMessage(answer)
	emitStats()
	return loopResult{Text: answer, Stats: *stats}, nil
}

// finalizeAnswerAfterBudget fires ONE extra LLM call with no tools available
// (tool_choice='none' equivalent: nil tools slice) and the Hermes synthesis
// prompt that names the stop_reason (US-OUT-09). The synthesis turn is counted
// in wall-clock + cost but NOT in the iteration counter. Returns ("", false)
// on any error or empty response — R7 mitigation: the caller falls back to
// finalAnswerOnBudgetWithContext rather than recursing.
func finalizeAnswerAfterBudget(ctx context.Context, client ChatClient, state State, opts Options, stats *Stats) (string, bool) {
	if client == nil || state == nil || stats == nil {
		return "", false
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	messages := governance.Apply(state.Messages(), opts.MaxToolResultChars, opts.MicrocompactKeepRecent, opts.MicrocompactMinChars)
	messages = append(messages, llm.Message{
		Role:    "user",
		Content: governance.FormatSynthesisPrompt(stats.StopReason),
	})
	// Log synthesis attempt as a structured run event so /api/runs/<id> and
	// container logs reflect which signal fired and when synthesis was attempted.
	logger.Info("agent: synthesis_turn",
		"stop_reason", stats.StopReason,
		"llm_calls_so_far", stats.LLMCalls,
		"tokens_total", stats.TokensTotal,
		"cost_usd", stats.CostUSD,
	)
	stats.LLMCalls++
	stats.LoopSteps++
	// nil tools = tool_choice='none': no tools dispatched in the synthesis round.
	resp, err := client.Chat(ctx, messages, nil)
	if err != nil {
		// R7: synthesis error → return partial work; no recursive finalize.
		logger.Warn("agent: synthesis_turn_failed",
			"stop_reason", stats.StopReason,
			"error", err,
		)
		return "", false
	}
	state.TrackTokens(resp.Response.Usage)
	stats.TokensPrompt += resp.Response.Usage.PromptTokens
	stats.TokensCompletion += resp.Response.Usage.CompletionTokens
	stats.TokensTotal += resp.Response.Usage.TotalTokens
	stats.CacheReadTokens += resp.Response.Usage.CacheReadTokens
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
