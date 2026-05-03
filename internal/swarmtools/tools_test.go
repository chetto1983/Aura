package swarmtools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/swarm"
	"github.com/aura/aura/internal/tools"
)

type fakeRunner struct {
	last agent.Task
}

func (r *fakeRunner) Run(_ context.Context, task agent.Task) (agent.Result, error) {
	r.last = task
	return agent.Result{
		Content:   "worker result",
		LLMCalls:  2,
		ToolCalls: len(task.ToolAllowlist),
		Tokens:    llm.TokenUsage{PromptTokens: 3, CompletionTokens: 5, TotalTokens: 8},
		Elapsed:   12 * time.Millisecond,
	}, nil
}

func newToolTest(t *testing.T) (*swarm.Store, *fakeRunner, *swarm.Manager) {
	t.Helper()
	store, err := swarm.OpenStore(filepath.Join(t.TempDir(), "swarm.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	runner := &fakeRunner{}
	manager, err := swarm.NewManager(swarm.ManagerConfig{Runner: runner, Store: store, MaxActive: 2, MaxDepth: 1})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return store, runner, manager
}

func TestSpawnAuraBotTool(t *testing.T) {
	store, runner, manager := newToolTest(t)
	tool := NewSpawnAuraBotTool(manager)
	ctx := tools.WithUserID(context.Background(), "user-123")
	out, err := tool.Execute(ctx, map[string]any{
		"name":  "read context",
		"role":  "librarian",
		"task":  "read the wiki index",
		"tools": []any{"list_wiki", "read_wiki"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp spawnResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.OK || resp.RunID == "" || resp.TaskID == "" {
		t.Fatalf("response = %+v", resp)
	}
	if resp.LLMCalls != 2 || resp.ToolCalls != 2 || resp.TokensTotal != 8 {
		t.Fatalf("metrics = %+v", resp)
	}
	if runner.last.UserID != "user-123" {
		t.Fatalf("runner user id = %q", runner.last.UserID)
	}
	if len(runner.last.ToolAllowlist) != 2 || runner.last.ToolAllowlist[0] != "list_wiki" || runner.last.ToolAllowlist[1] != "read_wiki" {
		t.Fatalf("allowlist = %+v", runner.last.ToolAllowlist)
	}
	task, err := store.GetTask(context.Background(), resp.TaskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != swarm.TaskCompleted || task.Result != "worker result" {
		t.Fatalf("task = %+v", task)
	}
}

func TestSpawnAuraBotRejectsDisallowedTool(t *testing.T) {
	_, _, manager := newToolTest(t)
	tool := NewSpawnAuraBotTool(manager)
	_, err := tool.Execute(context.Background(), map[string]any{
		"role":  "librarian",
		"task":  "try a write",
		"tools": []any{"write_wiki"},
	})
	if err == nil {
		t.Fatal("expected disallowed tool error")
	}
}

func TestListAndReadSwarmTools(t *testing.T) {
	store, _, manager := newToolTest(t)
	spawn := NewSpawnAuraBotTool(manager)
	out, err := spawn.Execute(context.Background(), map[string]any{
		"role": "critic",
		"task": "lint",
	})
	if err != nil {
		t.Fatalf("spawn Execute: %v", err)
	}
	var resp spawnResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("unmarshal spawn: %v", err)
	}

	listOut, err := NewListSwarmTasksTool(store).Execute(context.Background(), map[string]any{"run_id": resp.RunID})
	if err != nil {
		t.Fatalf("list Execute: %v", err)
	}
	if listOut == "" || !json.Valid([]byte(listOut)) {
		t.Fatalf("list output not JSON: %q", listOut)
	}

	readOut, err := NewReadSwarmResultTool(store).Execute(context.Background(), map[string]any{"task_id": resp.TaskID})
	if err != nil {
		t.Fatalf("read Execute: %v", err)
	}
	var task taskSummary
	if err := json.Unmarshal([]byte(readOut), &task); err != nil {
		t.Fatalf("unmarshal read: %v", err)
	}
	if task.Result != "worker result" || task.Status != string(swarm.TaskCompleted) {
		t.Fatalf("task summary = %+v", task)
	}
}
