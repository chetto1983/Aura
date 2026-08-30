package runner

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
)

func TestRunnerNewTurnUsesReplacementLLMRuntime(t *testing.T) {
	oldClient := agenttest.NewFakeClient(agenttest.TextChunks("stop", "old"))
	r, conv, _ := newTestRunner(t, oldClient)
	newClient := agenttest.NewFakeClient(agenttest.TextChunks("stop", "new"))
	hotConfig := llm.Config{
		Provider:        "llamacpp",
		BaseURL:         "http://aura-llm:8084/v1",
		Model:           "gemma-4-12b",
		ContextWindow:   1_000_000,
		MaxTokens:       4096,
		MaxOutputTokens: 32768,
		TotalTimeoutSec: 120,
		ShowReasoning:   true,
	}
	r.runtime.Replace(newClient, hotConfig)

	convID := newConvID(t)
	mustCreate(t, r, convID)
	if _, err := drain(r.Turn(context.Background(), convID, new("use the hot route"))); err != nil {
		t.Fatal(err)
	}

	if oldClient.CallCount() != 0 || newClient.CallCount() == 0 {
		t.Fatalf("stream calls = old %d new %d, want 0/>0", oldClient.CallCount(), newClient.CallCount())
	}
	created, err := conv.Get(context.Background(), convID)
	if err != nil {
		t.Fatal(err)
	}
	if created.Model != "gemma-4-12b" || newClient.LastRequest().Model != "gemma-4-12b" {
		t.Fatalf("models = conversation %q request %q", created.Model, newClient.LastRequest().Model)
	}
}
