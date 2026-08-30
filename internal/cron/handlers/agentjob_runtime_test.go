package handlers

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
)

func TestAgentJobUsesReplacementLLMRuntimeAtRunStart(t *testing.T) {
	oldClient := agenttest.NewFakeClient(agenttest.TextChunks("stop", "old"))
	newClient := agenttest.NewFakeClient(agenttest.TextChunks("stop", "new"))
	runtime := llm.NewRuntime(oldClient, llm.Config{Model: "old-model"})
	runtime.Replace(newClient, llm.Config{Model: "gemma-4-12b"})
	h := AgentJobHandler{Deps: AgentDeps{
		Client:   oldClient,
		LLM:      llm.Config{Model: "old-model"},
		Runtime:  runtime,
		Registry: jobRegistry(),
	}}

	if _, err := h.Run(context.Background(), Job{Payload: []byte(`{"goal":"use replacement"}`), RunID: "run-hot"}); err != nil {
		t.Fatal(err)
	}
	if oldClient.CallCount() != 0 || newClient.CallCount() == 0 {
		t.Fatalf("stream calls = old %d new %d, want 0/>0", oldClient.CallCount(), newClient.CallCount())
	}
	if got := newClient.LastRequest().Model; got != "gemma-4-12b" {
		t.Fatalf("request model = %q, want gemma-4-12b", got)
	}
}
