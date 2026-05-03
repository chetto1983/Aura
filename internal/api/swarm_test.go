package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/swarm"
)

func TestSwarmRunListNilStore(t *testing.T) {
	router := NewRouter(Deps{})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/swarm/runs", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got []SwarmRunSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("runs = %+v", got)
	}
}

func TestSwarmRunListAndDetail(t *testing.T) {
	ctx := context.Background()
	store := newAPISwarmStore(t)
	run, task := seedCompletedSwarmRun(t, store)
	router := NewRouter(Deps{Swarm: store})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/swarm/runs?limit=10", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("list status %d, body %s", rr.Code, rr.Body)
	}
	var list []SwarmRunSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != run.ID {
		t.Fatalf("list = %+v", list)
	}
	if list[0].TaskCounts.Completed != 1 || list[0].Metrics.TokensTotal != 13 || list[0].Metrics.Speedup <= 0 {
		t.Fatalf("summary metrics = %+v", list[0])
	}

	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/swarm/runs/"+run.ID, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("detail status %d, body %s", rr.Code, rr.Body)
	}
	var detail SwarmRunDetail
	if err := json.Unmarshal(rr.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.ID != run.ID || len(detail.Tasks) != 1 || detail.Tasks[0].ID != task.ID {
		t.Fatalf("detail = %+v", detail)
	}
	if detail.Tasks[0].Result != "done" || detail.Tasks[0].ToolAllowlist[0] != "read_wiki" {
		t.Fatalf("task = %+v", detail.Tasks[0])
	}

	rr = httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/swarm/tasks/"+task.ID, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("task status %d, body %s", rr.Code, rr.Body)
	}
	var gotTask SwarmTask
	if err := json.Unmarshal(rr.Body.Bytes(), &gotTask); err != nil {
		t.Fatal(err)
	}
	if gotTask.ID != task.ID || gotTask.RunID != run.ID || gotTask.TokensPrompt != 5 {
		t.Fatalf("got task = %+v", gotTask)
	}

	if _, err := store.GetTask(ctx, task.ID); err != nil {
		t.Fatalf("seed task missing: %v", err)
	}
}

func TestSwarmInvalidIDs(t *testing.T) {
	router := NewRouter(Deps{Swarm: newAPISwarmStore(t)})
	for _, tc := range []struct {
		path string
	}{
		{"/swarm/runs/bad"},
		{"/swarm/tasks/swarm_1234567890abcdef"},
	} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest("GET", tc.path, nil))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, body %s", tc.path, rr.Code, rr.Body)
		}
	}
}

func newAPISwarmStore(t *testing.T) *swarm.Store {
	t.Helper()
	store, err := swarm.OpenStore(filepath.Join(t.TempDir(), "swarm.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func seedCompletedSwarmRun(t *testing.T, store *swarm.Store) (*swarm.Run, *swarm.Task) {
	t.Helper()
	ctx := context.Background()
	run, err := store.CreateRun(ctx, "observe", "user")
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := store.MarkRunRunning(ctx, run.ID); err != nil {
		t.Fatalf("MarkRunRunning: %v", err)
	}
	task, err := store.CreateTask(ctx, run.ID, swarm.Assignment{
		Role:          "librarian",
		Subject:       "read",
		Prompt:        "read",
		ToolAllowlist: []string{"read_wiki"},
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
		Tokens:    llm.TokenUsage{PromptTokens: 5, CompletionTokens: 8, TotalTokens: 13},
		Elapsed:   50 * time.Millisecond,
	}); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	time.Sleep(time.Millisecond)
	if err := store.CompleteRun(ctx, run.ID); err != nil {
		t.Fatalf("CompleteRun: %v", err)
	}
	gotRun, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	gotTask, err := store.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return gotRun, gotTask
}
