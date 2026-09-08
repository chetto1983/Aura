package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

type mutatingFakeTool struct{}

func (mutatingFakeTool) Spec() tools.Spec {
	return tools.Spec{Name: "fake_write", Summary: "Fake mutating tool.", Description: "Fake mutating tool.",
		Parameters: json.RawMessage(`{"type":"object","properties":{"v":{"type":"string"}}}`), Mutating: true}
}
func (mutatingFakeTool) Execute(ctx context.Context, raw json.RawMessage) (tools.ToolResult, error) {
	return tools.NewResult(ctx, "wrote:"+string(raw))
}
func newTightBudgetIC(t *testing.T, steps int) agent.InvocationContext {
	t.Helper()
	return newIC(t, agent.BudgetOptions{MaxSteps: &steps})
}
func newGateAgent(t *testing.T, fc *agenttest.FakeClient, gate bool) *agent.LlmAgent {
	t.Helper()
	r := tools.NewRegistry()
	r.Register(tools.TextResponse{})
	r.Register(&echoTool{})
	r.Register(&mutatingFakeTool{})
	return agent.NewLlmAgent(agent.LlmAgentConfig{
		Client: fc, LLM: llm.Config{Model: "test-model", Provider: "test-provider", TotalTimeoutSec: 30, CompletionGate: gate},
		Registry: r, PreviewCap: 2048, RunDir: t.TempDir(), SessionID: uuid.Must(uuid.NewV7()).String(),
		UserTurns: []llm.Message{{Role: llm.RoleUser, Content: "make me a file"}},
	})
}
func mutatingCall(id string) llm.ToolCall {
	return agenttest.MakeToolCall(id, "fake_write", `{"v":"x"}`)
}
func echoCall(id string) llm.ToolCall { return agenttest.MakeToolCall(id, "echo", `{"v":"x"}`) }
func lastFinal(evs []*agent.Event) *agent.Event {
	for _, ev := range slices.Backward(evs) {
		if ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "" {
			return ev
		}
	}
	return nil
}
func finalContent(t *testing.T, evs []*agent.Event) string {
	t.Helper()
	ev := lastFinal(evs)
	if ev == nil {
		t.Fatal("no final event")
	}
	return ev.LLMResponse.Content
}

func TestCompletionGate_NoAuditCallAtBudgetEnd(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprintf("terminal_tool=%t", terminal), func(t *testing.T) {
			answer := agenttest.TextChunks("stop", "Eseguito.")
			if terminal {
				answer = agenttest.ToolCallTurn(textResponseCall("end", "Eseguito."))
			}
			fc := agenttest.NewFakeClient(agenttest.ToolCallTurn(mutatingCall("c1")), answer,
				agenttest.TextChunks("stop", "DONE"))
			a := newGateAgent(t, fc, true)
			evs, err := collect(a.Run(newTightBudgetIC(t, 2)))
			if err != nil {
				t.Fatal(err)
			}
			if fc.CallCount() != 2 || finalContent(t, evs) != "Eseguito." {
				t.Fatalf("calls=%d final=%q; want exactly two task calls and the original answer", fc.CallCount(), finalContent(t, evs))
			}
		})
	}
}
func TestCompletionGate_DeterministicHygieneStillRejectsDrafts(t *testing.T) {
	for _, gate := range []bool{false, true} {
		t.Run(fmt.Sprintf("enabled=%t", gate), func(t *testing.T) {
			const draft = "Hmm, wait. I am drafting the answer."
			fc := agenttest.NewFakeClient(agenttest.TextChunks("stop", draft), agenttest.TextChunks("stop", "Risultato confermato."))
			a := newGateAgent(t, fc, gate)
			evs, err := collect(a.Run(newTightBudgetIC(t, 2)))
			if err != nil {
				t.Fatal(err)
			}
			want, calls := draft, 1
			if gate {
				want, calls = "Risultato confermato.", 2
			}
			if finalContent(t, evs) != want || fc.CallCount() != calls {
				t.Fatalf("final=%q calls=%d", finalContent(t, evs), fc.CallCount())
			}
		})
	}
}
