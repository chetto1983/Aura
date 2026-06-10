package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeTaskStore records calls so the action tests assert routing without a DB.
type fakeTaskStore struct {
	created    CreateTaskInput
	createErr  error
	createID   string
	listRows   []ScheduledTask
	listErr    error
	cancelled  string
	cancelErr  error
	ranNow     string
	runNowErr  error
	approved   string
	approveErr error
}

func (f *fakeTaskStore) CreateScheduledTask(_ context.Context, in CreateTaskInput) (ScheduledTask, error) {
	f.created = in
	if f.createErr != nil {
		return ScheduledTask{}, f.createErr
	}
	id := f.createID
	if id == "" {
		id = "task-1"
	}
	return ScheduledTask{ID: id, Kind: in.Kind, ScheduleKind: in.ScheduleKind, Status: in.Status, NextRunAt: in.NextRunAt}, nil
}
func (f *fakeTaskStore) ListScheduledTasks(context.Context) ([]ScheduledTask, error) {
	return f.listRows, f.listErr
}
func (f *fakeTaskStore) CancelScheduledTask(_ context.Context, id string) error {
	f.cancelled = id
	return f.cancelErr
}
func (f *fakeTaskStore) RunScheduledTaskNow(_ context.Context, id string) error {
	f.ranNow = id
	return f.runNowErr
}
func (f *fakeTaskStore) ApproveScheduledTask(_ context.Context, id string) error {
	f.approved = id
	return f.approveErr
}

// TestTaskSchema is the load-bearing D-10 gate (nanobot regression #3113): the
// task tool's Parameters root must have required==["action"] EXACTLY and carry NO
// root-level oneOf/anyOf/enum, so the schema never 400s an OpenAI-compat provider.
func TestTaskSchema(t *testing.T) {
	spec := (&TaskTool{}).Spec()

	if spec.Deferred {
		t.Error("task tool must be non-deferred (D-11, core verb)")
	}
	if spec.Name != "task" {
		t.Errorf("Name = %q, want %q", spec.Name, "task")
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(spec.Parameters, &root); err != nil {
		t.Fatalf("Parameters is not a JSON object: %v", err)
	}

	// required must be exactly ["action"].
	var required []string
	if err := json.Unmarshal(root["required"], &required); err != nil {
		t.Fatalf("required is not a string array: %v", err)
	}
	if len(required) != 1 || required[0] != "action" {
		t.Fatalf("root required = %v, want exactly [\"action\"] (D-10)", required)
	}

	// No root-level oneOf/anyOf/enum (any of these break OpenAI-wire).
	for _, banned := range []string{"oneOf", "anyOf", "enum"} {
		if _, present := root[banned]; present {
			t.Errorf("root schema must NOT contain %q (breaks DeepSeek/OpenAI-compat, D-10)", banned)
		}
	}

	var props map[string]json.RawMessage
	if err := json.Unmarshal(root["properties"], &props); err != nil {
		t.Fatalf("properties is not an object: %v", err)
	}
	var action struct {
		Enum []string `json:"enum"`
	}
	if err := json.Unmarshal(props["action"], &action); err != nil {
		t.Fatalf("action property is not an object: %v", err)
	}
	wantActions := []string{"schedule", "list", "cancel", "run_now"}
	if len(action.Enum) != len(wantActions) {
		t.Fatalf("action enum = %v, want %v", action.Enum, wantActions)
	}
	for i := range wantActions {
		if action.Enum[i] != wantActions[i] {
			t.Fatalf("action enum = %v, want %v", action.Enum, wantActions)
		}
	}
}

func TestTaskScheduleCronGoesActive(t *testing.T) {
	store := &fakeTaskStore{createID: "abc"}
	tool := &TaskTool{Store: store}

	args := json.RawMessage(`{"action":"schedule","schedule_kind":"cron","cron":"30 9 * * 1-5","kind":"reminder","payload":{"text":"standup"}}`)
	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("schedule cron: %v", err)
	}
	if store.created.Status != "active" {
		t.Errorf("a benign reminder must be active, got status %q", store.created.Status)
	}
	if store.created.CronExpr != "30 9 * * 1-5" {
		t.Errorf("CronExpr not persisted: %q", store.created.CronExpr)
	}
	if store.created.NextRunAt.IsZero() {
		t.Error("NextRunAt must be computed for a cron schedule")
	}
	if !strings.Contains(res.Preview, "abc") {
		t.Errorf("preview should name the created id, got %q", res.Preview)
	}
}

func TestTaskScheduleDestructivePayloadGated(t *testing.T) {
	store := &fakeTaskStore{}
	tool := &TaskTool{Store: store}

	// An agent_job whose payload matches a destructive keyword jumps to Destructive,
	// so scoring.GateRecommended routes it to pending_approval (T-10-12 / D-27).
	args := json.RawMessage(`{"action":"schedule","schedule_kind":"every","every_minutes":10,"kind":"agent_job","payload":{"goal":"drop the staging database"}}`)
	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("schedule destructive: %v", err)
	}
	if store.created.Status != "pending_approval" {
		t.Fatalf("destructive payload must be pending_approval, got %q", store.created.Status)
	}
	if !strings.Contains(res.Preview, "approval") {
		t.Errorf("preview must mention approval, got %q", res.Preview)
	}
}

func TestTaskScheduleInvalidCronRejectedBeforePersist(t *testing.T) {
	store := &fakeTaskStore{}
	tool := &TaskTool{Store: store}

	args := json.RawMessage(`{"action":"schedule","schedule_kind":"cron","cron":"not a cron","kind":"reminder"}`)
	if _, err := tool.Execute(context.Background(), args); err == nil {
		t.Fatal("an invalid cron expression must be rejected (gronx.IsValid, T-10-14)")
	}
	if store.created.Kind != "" {
		t.Error("nothing must be persisted when the cron expr is invalid (validate-before-persist)")
	}
}

func TestTaskList(t *testing.T) {
	store := &fakeTaskStore{listRows: []ScheduledTask{
		{ID: "t1", Kind: "reminder", ScheduleKind: "cron", Status: "active", NextRunAt: time.Date(2030, 1, 1, 9, 30, 0, 0, time.UTC)},
		{ID: "t2", Kind: "agent_job", ScheduleKind: "every", Status: "pending_approval"},
		// active with a zero (NULL) next_run_at → unschedulable (WR-01).
		{ID: "t3", Kind: "reminder", ScheduleKind: "at", Status: "active"},
	}}
	tool := &TaskTool{Store: store}

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(res.Preview, "t1") || !strings.Contains(res.Preview, "t2") {
		t.Errorf("list must render both tasks, got %q", res.Preview)
	}
	if !strings.Contains(res.Preview, "[awaiting approval]") {
		t.Errorf("list must flag the pending_approval row, got %q", res.Preview)
	}
	if !strings.Contains(res.Preview, "[unschedulable]") {
		t.Errorf("list must flag the active NULL-next_run_at row as unschedulable, got %q", res.Preview)
	}
	if !strings.Contains(res.Preview, "2030-01-01T09:30:00Z") {
		t.Errorf("list must show next_run_at, got %q", res.Preview)
	}
}

func TestTaskCancelRunNow(t *testing.T) {
	store := &fakeTaskStore{}
	tool := &TaskTool{Store: store}

	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"cancel","task_id":"c1"}`)); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if store.cancelled != "c1" {
		t.Errorf("cancel routed id %q, want c1", store.cancelled)
	}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"run_now","task_id":"r1"}`)); err != nil {
		t.Fatalf("run_now: %v", err)
	}
	if store.ranNow != "r1" {
		t.Errorf("run_now routed id %q, want r1", store.ranNow)
	}
}

func TestTaskMissingTaskID(t *testing.T) {
	tool := &TaskTool{Store: &fakeTaskStore{}}
	for _, action := range []string{"cancel", "run_now"} {
		raw := json.RawMessage(`{"action":"` + action + `"}`)
		if _, err := tool.Execute(context.Background(), raw); err == nil {
			t.Errorf("action=%s without task_id must error", action)
		}
	}
}

func TestTaskApproveIsNotModelRoutable(t *testing.T) {
	store := &fakeTaskStore{}
	tool := &TaskTool{Store: store}

	_, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"approve","task_id":"a1"}`))
	if err == nil || !strings.Contains(err.Error(), "unknown action") {
		t.Fatalf("approve must be rejected by the model-facing router, got %v", err)
	}
	if store.approved != "" {
		t.Fatalf("approve routed to store despite model-facing removal: %q", store.approved)
	}
}

func TestTaskUnknownActionStructuredError(t *testing.T) {
	tool := &TaskTool{Store: &fakeTaskStore{}}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"explode"}`))
	if err == nil {
		t.Fatal("unknown action must return an error")
	}
	if !strings.Contains(err.Error(), "explode") {
		t.Errorf("error must name the bad action, got %q", err.Error())
	}
}

func TestTaskToolConcurrentExecuteInitializesRouterOnce(t *testing.T) {
	tool := &TaskTool{Store: &fakeTaskStore{}}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = tool.Execute(context.Background(), json.RawMessage(`{"action":"explode"}`))
		}()
	}
	wg.Wait()
}

func TestTaskMissingAction(t *testing.T) {
	tool := &TaskTool{Store: &fakeTaskStore{}}
	if _, err := tool.Execute(context.Background(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing action must error")
	}
}

func TestTaskStoreErrorPropagates(t *testing.T) {
	sentinel := errors.New("db down")
	store := &fakeTaskStore{createErr: sentinel}
	tool := &TaskTool{Store: store}
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"schedule","schedule_kind":"every","every_minutes":5,"kind":"reminder"}`))
	if !errors.Is(err, sentinel) {
		t.Errorf("store error must propagate, got %v", err)
	}
}

// TestRegistryValidatesWithTaskTool mirrors the Phase 9 ValidatesWithSwarmSpawn
// precedent: a registry holding the non-deferred task tool passes Validate().
func TestRegistryValidatesWithTaskTool(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&TaskTool{Store: &fakeTaskStore{}})
	reg.Register(&ToolSearch{Registry: reg})
	if err := reg.Validate(); err != nil {
		t.Fatalf("registry with the non-deferred task tool must validate: %v", err)
	}
}
