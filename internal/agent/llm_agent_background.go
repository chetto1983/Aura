package agent

import (
	"context"
	"encoding/json"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// llm_agent_background.go hands a slow tool call to tools.BackgroundCalls (prd.md §15):
// a call still running after the budget's window leaves the turn instead of holding it.

type backgroundLimitsKey struct{}

type backgroundLimits struct {
	window  time.Duration
	ceiling time.Duration
}

// withBackgroundLimits records the run's background window and ceiling for execTool.
// Only runTool sets them, so a call that reaches execTool another way -- a reviewed
// message's dispatch -- stays in its turn.
func withBackgroundLimits(ctx context.Context, budget *Budget) context.Context {
	return context.WithValue(ctx, backgroundLimitsKey{}, backgroundLimits{
		window: budget.BackgroundWindow(), ceiling: budget.BackgroundCeiling(),
	})
}

// executeTool runs one attempt of tool: through the background registry when the agent
// has one, the dispatch carries the run's limits and the tool may leave its turn; in the
// turn otherwise.
func (a *LlmAgent) executeTool(ctx context.Context, tool tools.Tool, args json.RawMessage) (tools.ToolResult, error) {
	limits, ok := ctx.Value(backgroundLimitsKey{}).(backgroundLimits)
	if !ok || a.background == nil || tool.Spec().Foreground {
		return tool.Execute(ctx, args)
	}
	return a.background.Execute(ctx, tool, args, limits.window, limits.ceiling)
}
