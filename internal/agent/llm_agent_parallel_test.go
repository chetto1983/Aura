package agent_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// barrierTool proves the dispatch batch runs CONCURRENTLY without relying on timing:
// every call blocks until all n have entered Execute. Sequential dispatch would
// block the first call forever (the others never start) and time out to
// "barrier-timeout"; only true concurrency releases every call with "barrier-ok-<id>".
type barrierTool struct {
	n       int
	started *atomic.Int32
	release chan struct{}
}

func (barrierTool) Spec() tools.Spec {
	return tools.Spec{Name: "barrier", Summary: "test concurrency barrier", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (b barrierTool) Execute(ctx context.Context, raw json.RawMessage) (tools.ToolResult, error) {
	var a struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &a)
	if b.started.Add(1) == int32(b.n) {
		close(b.release)
	}
	select {
	case <-b.release:
		return tools.ToolResult{Preview: "barrier-ok-" + a.ID}, nil
	case <-ctx.Done():
		return tools.ToolResult{Preview: "barrier-ctx-" + a.ID}, nil
	case <-time.After(3 * time.Second):
		return tools.ToolResult{Preview: "barrier-timeout-" + a.ID}, nil
	}
}

// TestDispatch_ParallelConcurrentAndOrdered: three tool calls in one assistant
// message run concurrently (the barrier releases only if all three entered Execute
// together) AND their RoleTool results land in original call order (c1,c2,c3 →
// barrier-ok-1,2,3). A "barrier-timeout" anywhere means the batch ran sequentially.
func TestDispatch_ParallelConcurrentAndOrdered(t *testing.T) {
	recordingProvider(t)
	const n = 3
	bar := barrierTool{n: n, started: &atomic.Int32{}, release: make(chan struct{})}

	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(bar)

	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			agenttest.MakeToolCall("c1", "barrier", `{"id":"1"}`),
			agenttest.MakeToolCall("c2", "barrier", `{"id":"2"}`),
			agenttest.MakeToolCall("c3", "barrier", `{"id":"3"}`),
		),
		agenttest.ToolCallTurn(textResponseCall("c4", "done")),
	)
	a := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     fc,
		LLM:        llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry:   reg,
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "vai"}},
	})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(25)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("run errored: %v", err)
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content != "done" {
		t.Fatalf("loop did not reach the terminal answer: %+v", last.LLMResponse)
	}

	if len(fc.Requests) < 2 {
		t.Fatalf("want >=2 LLM requests, got %d", len(fc.Requests))
	}
	var got []string
	for _, m := range fc.Requests[1].Messages {
		if m.Role == llm.RoleTool && strings.HasPrefix(m.Content, "barrier-") {
			got = append(got, m.Content)
		}
	}
	want := []string{"barrier-ok-1", "barrier-ok-2", "barrier-ok-3"}
	if len(got) != len(want) {
		t.Fatalf("got %d barrier results %v, want %v", len(got), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("result[%d] = %q, want %q (a timeout = not concurrent; mismatch = wrong order). full: %v", i, got[i], want[i], got)
		}
	}
}
