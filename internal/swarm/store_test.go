package swarm

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"

	_ "modernc.org/sqlite"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "swarm.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestStoreRunAndTaskLifecycle(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	run, err := store.CreateRun(ctx, "grow wiki", "user-1")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := store.MarkRunRunning(ctx, run.ID); err != nil {
		t.Fatalf("MarkRunRunning: %v", err)
	}
	task, err := store.CreateTask(ctx, run.ID, Assignment{
		Role:          "librarian",
		Subject:       "read index",
		Prompt:        "read",
		ToolAllowlist: []string{"read_wiki", "read_wiki", "list_wiki"},
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := store.MarkTaskRunning(ctx, task.ID); err != nil {
		t.Fatalf("MarkTaskRunning: %v", err)
	}
	if err := store.CompleteTask(ctx, task.ID, agent.Result{
		Content:   "done",
		LLMCalls:  2,
		ToolCalls: 1,
		Tokens:    agentTokenUsage(7, 11, 18),
		Elapsed:   42 * time.Millisecond,
	}); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if err := store.CompleteRun(ctx, run.ID); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}

	gotRun, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if gotRun.Status != RunCompleted || gotRun.CompletedAt == nil {
		t.Fatalf("run = %+v", gotRun)
	}

	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if gotTask.Status != TaskCompleted || gotTask.Result != "done" {
		t.Fatalf("task status/result = %+v", gotTask)
	}
	if gotTask.LLMCalls != 2 || gotTask.ToolCalls != 1 || gotTask.ElapsedMS != 42 {
		t.Fatalf("task telemetry = llm:%d tools:%d elapsed:%d", gotTask.LLMCalls, gotTask.ToolCalls, gotTask.ElapsedMS)
	}
	if gotTask.TokensPrompt != 7 || gotTask.TokensCompletion != 11 || gotTask.TokensTotal != 18 {
		t.Fatalf("task tokens = prompt:%d completion:%d total:%d", gotTask.TokensPrompt, gotTask.TokensCompletion, gotTask.TokensTotal)
	}
	if len(gotTask.ToolAllowlist) != 2 || gotTask.ToolAllowlist[0] != "read_wiki" || gotTask.ToolAllowlist[1] != "list_wiki" {
		t.Fatalf("allowlist = %+v", gotTask.ToolAllowlist)
	}
}

func agentTokenUsage(prompt, completion, total int) llm.TokenUsage {
	return llm.TokenUsage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total}
}

func TestStoreReopenPersistsRows(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "swarm.db")
	store, err := OpenStore(path)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	run, err := store.CreateRun(ctx, "persist", "user")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	task, err := store.CreateTask(ctx, run.ID, Assignment{Role: "critic", Prompt: "lint"})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	store, err = OpenStore(path)
	if err != nil {
		t.Fatalf("reopen OpenStore: %v", err)
	}
	defer store.Close()
	if _, err := store.GetRun(ctx, run.ID); err != nil {
		t.Fatalf("GetRun after reopen: %v", err)
	}
	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask after reopen: %v", err)
	}
	if gotTask.Role != "critic" {
		t.Fatalf("role = %q", gotTask.Role)
	}
}

func TestNewStoreWithDBDoesNotOwnDB(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "shared.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	store, err := NewStoreWithDB(db)
	if err != nil {
		t.Fatalf("NewStoreWithDB: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("store close: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("shared db was closed: %v", err)
	}
}
