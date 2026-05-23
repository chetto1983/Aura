// loop_finalize.go holds the budget-exit finalization helpers used by runLoop.
// Extracted from loop.go to keep that file under the 600 LOC cap.
package agent

import (
	"context"
	"strings"

	governance "github.com/aura/aura/internal/agent/governance"
	"github.com/aura/aura/internal/llm"
)

// gracefulFinalize is the unified budget-exit handler used by all three
// budget paths (MaxElapsed, empty-LLM-response, MaxIterations). When
// AllowNoToolFinalization is true it attempts one extra LLM round via
// finalizeAnswerAfterBudget; on failure or when the flag is false it falls
// back to finalAnswerOnBudgetWithContext. It always adds the assistant message
// and fires emitStats before returning.
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
