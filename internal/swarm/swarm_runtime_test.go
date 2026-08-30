package swarm

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestSwarmWorkerUsesReplacementLLMRuntime(t *testing.T) {
	oldClient := newRouter().route("hot-route", outcome{kind: "ok", text: "old"})
	newClient := newRouter().route("hot-route", outcome{kind: "ok", text: "new"})
	rc := testRunConfig(t, oldClient, 25)
	rc.Runtime = llm.NewRuntime(oldClient, rc.LLM)
	rc.Runtime.Replace(newClient, llm.Config{Model: "gemma-4-12b", Provider: "llamacpp", TotalTimeoutSec: 30})

	out, err := Run(context.Background(), rc, []string{"hot-route task"})
	if err != nil {
		t.Fatal(err)
	}
	reports := parseReports(t, out)
	if len(reports) != 1 || reports[0].Summary != "new" {
		t.Fatalf("reports = %+v, want replacement-client summary", reports)
	}
	oldClient.mu.Lock()
	oldCalls := oldClient.calls["hot-route"]
	oldClient.mu.Unlock()
	newClient.mu.Lock()
	newCalls := newClient.calls["hot-route"]
	newClient.mu.Unlock()
	if oldCalls != 0 || newCalls == 0 {
		t.Fatalf("stream calls = old %d new %d, want 0/>0", oldCalls, newCalls)
	}
}
