package agent_test

import (
	"expvar"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/panicobs"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// sumToolErrors totals the published aura_agent_tool_errors_total expvar map across
// all tool labels, so a before/after delta isolates this test's contribution.
func sumToolErrors(t *testing.T) int64 {
	t.Helper()
	m, ok := expvar.Get("aura_agent_tool_errors_total").(*expvar.Map)
	if !ok || m == nil {
		t.Fatal("aura_agent_tool_errors_total expvar map is not published")
	}
	var total int64
	m.Do(func(kv expvar.KeyValue) {
		if iv, ok := kv.Value.(*expvar.Int); ok {
			total += iv.Value()
		}
	})
	return total
}

// TestExecuteBatch_PanicRecordsObservabilityMetrics asserts the runToolRecovering
// recover path records BOTH observability signals once per panicking call: the
// recovered-panic counter (site execute_batch) and the per-tool error counter. The
// model-visible panic result is covered elsewhere; this locks the metrics so a turn
// of panics is never silent in production dashboards.
func TestExecuteBatch_PanicRecordsObservabilityMetrics(t *testing.T) {
	recordingProvider(t)

	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(panicTool{})

	const panics = 2
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			agenttest.MakeToolCall("p1", "panic_tool", `{}`),
			agenttest.MakeToolCall("p2", "panic_tool", `{}`),
		),
		agenttest.ToolCallTurn(textResponseCall("done", "done")),
	)
	a := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     fc,
		LLM:        llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry:   reg,
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "go"}},
	})

	panicBefore := panicobs.Count("execute_batch")
	toolErrBefore := sumToolErrors(t)

	if _, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: ptr(10)}))); err != nil {
		t.Fatalf("run errored: %v", err)
	}

	if got := panicobs.Count("execute_batch") - panicBefore; got != panics {
		t.Fatalf("recovered-panic metric delta = %d, want %d (recordRecoveredPanic dropped?)", got, panics)
	}
	if got := sumToolErrors(t) - toolErrBefore; got != panics {
		t.Fatalf("tool-error metric delta = %d, want %d (recordToolError dropped?)", got, panics)
	}
}
