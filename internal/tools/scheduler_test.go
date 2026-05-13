package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/scheduler"
)

type fakeRunTaskNowRunner struct {
	name string
}

func (r *fakeRunTaskNowRunner) RunTaskNow(_ context.Context, name string) (RunTaskNowResult, error) {
	r.name = name
	return RunTaskNowResult{
		OK:        true,
		Name:      name,
		Kind:      string(scheduler.KindAgentJob),
		Status:    "completed",
		Summary:   "checked saved routine",
		LLMCalls:  1,
		ToolCalls: 2,
		ElapsedMS: 123,
		Notified:  true,
	}, nil
}

type fakeSchedulerRepository struct {
	tasks     map[string]*scheduler.Task
	cancelled []string
}

func (f *fakeSchedulerRepository) Upsert(_ context.Context, task *scheduler.Task) (*scheduler.Task, error) {
	if f.tasks == nil {
		f.tasks = make(map[string]*scheduler.Task)
	}
	cp := *task
	if cp.ID == 0 {
		cp.ID = int64(len(f.tasks) + 1)
	}
	cp.Status = scheduler.StatusActive
	f.tasks[cp.Name] = &cp
	return &cp, nil
}

func (f *fakeSchedulerRepository) GetByName(_ context.Context, name string) (*scheduler.Task, error) {
	if task, ok := f.tasks[name]; ok {
		cp := *task
		return &cp, nil
	}
	return nil, sql.ErrNoRows
}

func (f *fakeSchedulerRepository) List(_ context.Context, status scheduler.Status) ([]*scheduler.Task, error) {
	var out []*scheduler.Task
	for _, task := range f.tasks {
		if status != "" && task.Status != status {
			continue
		}
		cp := *task
		out = append(out, &cp)
	}
	return out, nil
}

func (f *fakeSchedulerRepository) Cancel(_ context.Context, name string) (bool, error) {
	if task, ok := f.tasks[name]; ok && task.Status == scheduler.StatusActive {
		task.Status = scheduler.StatusCancelled
		f.cancelled = append(f.cancelled, name)
		return true, nil
	}
	return false, nil
}

func (f *fakeSchedulerRepository) Delete(_ context.Context, name string) error {
	delete(f.tasks, name)
	return nil
}

func newTestSchedStore(t *testing.T) *scheduler.Store {
	t.Helper()
	store, err := scheduler.OpenStore(filepath.Join(t.TempDir(), "sched.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestTaskTool_NilStore(t *testing.T) {
	if NewTaskTool(nil, nil, time.UTC) != nil {
		t.Fatal("nil store should yield nil tool")
	}
}

func TestTaskTool_Schema(t *testing.T) {
	tool := NewTaskTool(&fakeSchedulerRepository{}, nil, time.UTC)
	if tool.Name() != "task" {
		t.Fatalf("name = %q, want task", tool.Name())
	}
	params := tool.Parameters()
	required, _ := params["required"].([]string)
	if len(required) != 1 || required[0] != "action" {
		t.Fatalf("required = %v, want [action]", required)
	}
	props, _ := params["properties"].(map[string]any)
	action, _ := props["action"].(map[string]any)
	enum, _ := action["enum"].([]string)
	if len(enum) != 4 {
		t.Fatalf("action enum = %v, want [schedule list cancel run_now]", enum)
	}
}

func TestTaskTool_MissingActionRejected(t *testing.T) {
	tool := NewTaskTool(&fakeSchedulerRepository{}, nil, time.UTC)
	_, err := tool.Execute(t.Context(), map[string]any{})
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Fatalf("missing action: err = %v, want ErrSchemaValidation", err)
	}
}

func TestTaskTool_UnknownActionRejected(t *testing.T) {
	tool := NewTaskTool(&fakeSchedulerRepository{}, nil, time.UTC)
	_, err := tool.Execute(t.Context(), map[string]any{"action": "destroy"})
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Fatalf("unknown action: err = %v, want ErrSchemaValidation", err)
	}
}

func TestTaskTool_RunNow(t *testing.T) {
	runner := &fakeRunTaskNowRunner{}
	tool := NewTaskTool(&fakeSchedulerRepository{}, runner, time.UTC)
	out, err := tool.Execute(t.Context(), map[string]any{"action": "run_now", "name": "morning-watch"})
	if err != nil {
		t.Fatalf("run_now: %v", err)
	}
	if runner.name != "morning-watch" {
		t.Fatalf("runner.name = %q", runner.name)
	}
	var result RunTaskNowResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if !result.OK || result.Name != "morning-watch" || result.Status != "completed" || !result.Notified {
		t.Fatalf("result = %+v", result)
	}
}

func TestTaskTool_RunNowMissingRunner(t *testing.T) {
	tool := NewTaskTool(&fakeSchedulerRepository{}, nil, time.UTC)
	_, err := tool.Execute(t.Context(), map[string]any{"action": "run_now", "name": "x"})
	if err == nil {
		t.Fatal("expected error when runner is nil")
	}
}

func TestTaskTool_AcceptsRepositoryInterface(t *testing.T) {
	repo := &fakeSchedulerRepository{}
	tool := NewTaskTool(repo, nil, time.UTC)
	out, err := tool.Execute(WithUserID(t.Context(), "12345"), map[string]any{
		"action":  "schedule",
		"name":    "fake-reminder",
		"kind":    "reminder",
		"payload": "check fake repo",
		"in":      "10m",
	})
	if err != nil {
		t.Fatalf("schedule Execute: %v", err)
	}
	if !strings.Contains(out, "fake-reminder") {
		t.Fatalf("schedule output = %q", out)
	}

	out, err = tool.Execute(t.Context(), map[string]any{"action": "list", "status": "active"})
	if err != nil {
		t.Fatalf("list Execute: %v", err)
	}
	if !strings.Contains(out, "`fake-reminder`") {
		t.Fatalf("list output = %q", out)
	}

	out, err = tool.Execute(t.Context(), map[string]any{"action": "cancel", "name": "fake-reminder"})
	if err != nil {
		t.Fatalf("cancel Execute: %v", err)
	}
	if !strings.Contains(out, "Cancelled task \"fake-reminder\"") || len(repo.cancelled) != 1 {
		t.Fatalf("cancel output=%q cancelled=%v", out, repo.cancelled)
	}
}

func TestTaskTool_OneShotReminder(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewTaskTool(store, nil, time.UTC)
	ctx := WithUserID(t.Context(), "12345")

	at := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	out, err := tool.Execute(ctx, map[string]any{
		"action":  "schedule",
		"name":    "buy-bread",
		"kind":    "reminder",
		"payload": "buy bread",
		"at":      at,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Scheduled reminder task \"buy-bread\"") {
		t.Errorf("response = %q", out)
	}

	got, err := store.GetByName(ctx, "buy-bread")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.RecipientID != "12345" {
		t.Errorf("RecipientID = %q, want 12345", got.RecipientID)
	}
	if got.Payload != "buy bread" {
		t.Errorf("Payload = %q", got.Payload)
	}
	if got.ScheduleKind != scheduler.ScheduleAt {
		t.Errorf("ScheduleKind = %q", got.ScheduleKind)
	}
}

func TestTaskTool_RejectsReminderWithoutUser(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewTaskTool(store, nil, time.UTC)

	at := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	_, err := tool.Execute(t.Context(), map[string]any{
		"action": "schedule", "name": "x", "kind": "reminder", "at": at,
	})
	if err == nil {
		t.Fatal("expected error: reminder without user context")
	}
	if !strings.Contains(err.Error(), "authenticated user context") {
		t.Errorf("error message = %q", err.Error())
	}
}

func TestTaskTool_DailyWikiMaintenance(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewTaskTool(store, nil, time.UTC)

	out, err := tool.Execute(t.Context(), map[string]any{
		"action": "schedule",
		"name":   "nightly",
		"kind":   "wiki_maintenance",
		"daily":  "03:00",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "daily at 03:00") {
		t.Errorf("response should mention daily schedule: %q", out)
	}

	got, err := store.GetByName(t.Context(), "nightly")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.Kind != scheduler.KindWikiMaintenance {
		t.Errorf("Kind = %q", got.Kind)
	}
	if got.ScheduleKind != scheduler.ScheduleDaily {
		t.Errorf("ScheduleKind = %q, want daily", got.ScheduleKind)
	}
	if got.RecipientID != "" {
		t.Errorf("RecipientID = %q, want empty for wiki_maintenance", got.RecipientID)
	}
}

func TestTaskTool_DailyWeekdays(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewTaskTool(store, nil, time.UTC)
	ctx := WithUserID(t.Context(), "u")

	out, err := tool.Execute(ctx, map[string]any{
		"action":   "schedule",
		"name":     "weekday-briefing",
		"kind":     "reminder",
		"payload":  "briefing",
		"daily":    "10:00",
		"weekdays": []any{"mon", "tue", "wed", "thu", "fri"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "on mon,tue,wed,thu,fri") {
		t.Errorf("response should mention weekdays: %q", out)
	}
	got, err := store.GetByName(ctx, "weekday-briefing")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.ScheduleKind != scheduler.ScheduleDaily || got.ScheduleWeekdays != "mon,tue,wed,thu,fri" {
		t.Errorf("schedule = %s/%q, want daily weekdays", got.ScheduleKind, got.ScheduleWeekdays)
	}
}

func TestTaskTool_EveryMinutes(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewTaskTool(store, nil, time.UTC)

	before := time.Now().UTC()
	out, err := tool.Execute(t.Context(), map[string]any{
		"action":        "schedule",
		"name":          "hourly-maintenance",
		"kind":          "wiki_maintenance",
		"every_minutes": float64(60),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "every 60 minutes") {
		t.Errorf("response should mention interval: %q", out)
	}
	got, err := store.GetByName(t.Context(), "hourly-maintenance")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.ScheduleKind != scheduler.ScheduleEvery || got.ScheduleEveryMinutes != 60 {
		t.Errorf("schedule = %s/%d, want every 60", got.ScheduleKind, got.ScheduleEveryMinutes)
	}
	delta := got.NextRunAt.Sub(before)
	if delta < 55*time.Minute || delta > 65*time.Minute {
		t.Errorf("next_run_at delta = %v, want ~60m", delta)
	}
}

func TestTaskTool_AgentJob(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewTaskTool(store, nil, time.UTC)
	ctx := WithUserID(t.Context(), "12345")

	out, err := tool.Execute(ctx, map[string]any{
		"action":   "schedule",
		"name":     "morning-watch",
		"kind":     "agent_job",
		"payload":  "Check project news and propose useful wiki updates.",
		"daily":    "10:00",
		"weekdays": []any{"mon", "tue", "wed", "thu", "fri"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Scheduled agent_job task \"morning-watch\"") {
		t.Errorf("response = %q", out)
	}
	got, err := store.GetByName(ctx, "morning-watch")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.Kind != scheduler.KindAgentJob {
		t.Errorf("Kind = %q, want agent_job", got.Kind)
	}
	if got.RecipientID != "12345" {
		t.Errorf("RecipientID = %q, want caller id", got.RecipientID)
	}
	payload, err := scheduler.NormalizeAgentJobPayload(got.Payload)
	if err != nil {
		t.Fatalf("NormalizeAgentJobPayload: %v", err)
	}
	if payload.Goal != "Check project news and propose useful wiki updates." {
		t.Errorf("goal = %q", payload.Goal)
	}
	if payload.WritePolicy != scheduler.AgentJobWritePolicyProposeOnly {
		t.Errorf("write policy = %q", payload.WritePolicy)
	}
}

func TestTaskTool_AgentJobStructuredPayload(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewTaskTool(store, nil, time.UTC)
	ctx := WithUserID(t.Context(), "12345")

	payloadJSON := `{
		"goal":"Check daily project memory drift",
		"enabled_toolsets":["memory_read"],
		"skills":["aura-implementation"],
		"context_from":["[[memory-philosophy]]"],
		"wake_if_changed":["wiki:memory-philosophy"],
		"tool_allowlist":["search_memory","web"]
	}`
	_, err := tool.Execute(ctx, map[string]any{
		"action":        "schedule",
		"name":          "memory-drift-watch",
		"kind":          "agent_job",
		"payload":       payloadJSON,
		"every_minutes": float64(1440),
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, err := store.GetByName(ctx, "memory-drift-watch")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	payload, err := scheduler.NormalizeAgentJobPayload(got.Payload)
	if err != nil {
		t.Fatalf("NormalizeAgentJobPayload: %v", err)
	}
	if payload.Goal != "Check daily project memory drift" {
		t.Fatalf("goal = %q", payload.Goal)
	}
	if strings.Contains(strings.Join(payload.ToolAllowlist, ","), "web") {
		t.Fatalf("web should be filtered by memory_read toolset: %+v", payload.ToolAllowlist)
	}
	for _, want := range []string{"search_memory", "file"} {
		if !containsSchedTestString(payload.ToolAllowlist, want) {
			t.Fatalf("missing %q in allowlist: %+v", want, payload.ToolAllowlist)
		}
	}
	if !containsSchedTestString(payload.EnabledToolsets, "skills_read") {
		t.Fatalf("skills_read should be auto-enabled for skill-backed jobs: %+v", payload.EnabledToolsets)
	}
}

func TestTaskTool_RejectsBadScheduleInputs(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewTaskTool(store, nil, time.UTC)
	ctx := WithUserID(t.Context(), "u")

	cases := []struct {
		name string
		args map[string]any
		hint string
	}{
		{"missing kind", map[string]any{"action": "schedule", "name": "a"}, "kind"},
		{"unknown kind", map[string]any{"action": "schedule", "name": "a", "kind": "foo"}, "unknown kind"},
		{"missing schedule", map[string]any{"action": "schedule", "name": "a", "kind": "reminder"}, "provide one of"},
		{"both schedules", map[string]any{
			"action": "schedule", "name": "a", "kind": "reminder",
			"at":    time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
			"daily": "03:00",
		}, "mutually exclusive"},
		{"in plus at_local", map[string]any{
			"action": "schedule", "name": "a", "kind": "reminder",
			"in":       "5m",
			"at_local": "2026-04-30T17:00:00",
		}, "mutually exclusive"},
		{"past at", map[string]any{
			"action": "schedule", "name": "a", "kind": "reminder",
			"at": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
		}, "not in the future"},
		{"bad daily", map[string]any{
			"action": "schedule", "name": "a", "kind": "wiki_maintenance", "daily": "3am",
		}, ""},
		{"agent job missing goal", map[string]any{
			"action": "schedule", "name": "a", "kind": "agent_job", "daily": "03:00",
		}, "agent_job payload goal required"},
		{"bad every", map[string]any{
			"action": "schedule", "name": "a", "kind": "wiki_maintenance", "every_minutes": float64(0),
		}, "every_minutes must be >= 1"},
		{"fractional every", map[string]any{
			"action": "schedule", "name": "a", "kind": "wiki_maintenance", "every_minutes": 1.5,
		}, "every_minutes must be an integer"},
		{"daily plus every", map[string]any{
			"action": "schedule", "name": "a", "kind": "wiki_maintenance", "daily": "03:00", "every_minutes": float64(60),
		}, "mutually exclusive"},
		{"weekdays without daily", map[string]any{
			"action": "schedule", "name": "a", "kind": "wiki_maintenance", "every_minutes": float64(60), "weekdays": []any{"mon"},
		}, "weekdays can only be used with daily"},
		{"bad weekday", map[string]any{
			"action": "schedule", "name": "a", "kind": "wiki_maintenance", "daily": "03:00", "weekdays": []any{"moonday"},
		}, "invalid weekday"},
		{"bad in", map[string]any{
			"action": "schedule", "name": "a", "kind": "wiki_maintenance", "in": "soon",
		}, "parse in"},
		{"non-positive in", map[string]any{
			"action": "schedule", "name": "a", "kind": "wiki_maintenance", "in": "-5m",
		}, "must be positive"},
		{"bad at_local format", map[string]any{
			"action": "schedule", "name": "a", "kind": "wiki_maintenance", "at_local": "domani alle 5",
		}, "parse at_local"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(ctx, tc.args)
			if err == nil {
				t.Fatal("expected error")
			}
			if tc.hint != "" && !strings.Contains(err.Error(), tc.hint) {
				t.Errorf("error %q should contain %q", err.Error(), tc.hint)
			}
		})
	}
}

func containsSchedTestString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func TestTaskTool_RelativeIn(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewTaskTool(store, nil, time.UTC)
	ctx := WithUserID(t.Context(), "u")

	before := time.Now().UTC()
	out, err := tool.Execute(ctx, map[string]any{
		"action":  "schedule",
		"name":    "ciao-mondo",
		"kind":    "reminder",
		"payload": "ciao mondo",
		"in":      "60s",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Scheduled reminder task") {
		t.Errorf("response = %q", out)
	}

	got, err := store.GetByName(ctx, "ciao-mondo")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	delta := got.NextRunAt.Sub(before)
	if delta < 55*time.Second || delta > 75*time.Second {
		t.Errorf("next_run_at delta = %v, want ~60s", delta)
	}
	if got.ScheduleKind != scheduler.ScheduleAt {
		t.Errorf("ScheduleKind = %q, want at (in resolves to absolute)", got.ScheduleKind)
	}
}

func TestTaskTool_AtLocal(t *testing.T) {
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skipf("Europe/Rome tzdata unavailable: %v", err)
	}
	store := newTestSchedStore(t)
	tool := NewTaskTool(store, nil, rome)
	ctx := WithUserID(t.Context(), "u")

	tomorrow := time.Now().In(rome).AddDate(0, 0, 1)
	atLocal := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 17, 0, 0, 0, rome).
		Format("2006-01-02T15:04:05")

	out, err := tool.Execute(ctx, map[string]any{
		"action":   "schedule",
		"name":     "compra-pane",
		"kind":     "reminder",
		"payload":  "compra il pane",
		"at_local": atLocal,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Scheduled reminder task") {
		t.Errorf("response = %q", out)
	}

	got, err := store.GetByName(ctx, "compra-pane")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	want := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 17, 0, 0, 0, rome).UTC()
	if !got.NextRunAt.Equal(want) {
		t.Errorf("next_run_at = %v, want %v (17:00 Rome → UTC)", got.NextRunAt, want)
	}
}

func TestTaskTool_AtLocalRejectsPast(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewTaskTool(store, nil, time.UTC)
	ctx := WithUserID(t.Context(), "u")

	_, err := tool.Execute(ctx, map[string]any{
		"action":   "schedule",
		"name":     "x",
		"kind":     "reminder",
		"at_local": "1970-01-01T00:00:00",
	})
	if err == nil || !strings.Contains(err.Error(), "not in the future") {
		t.Errorf("expected past-time error, got: %v", err)
	}
}

func TestParseLocalWallClock_AcceptsCommonShapes(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skipf("Europe/Rome tzdata unavailable: %v", err)
	}
	for _, in := range []string{
		"2026-04-30T17:00:00",
		"2026-04-30T17:00",
		"2026-04-30 17:00:00",
		"2026-04-30 17:00",
	} {
		t.Run(in, func(t *testing.T) {
			ts, err := parseLocalWallClock(in, loc)
			if err != nil {
				t.Fatalf("parseLocalWallClock(%q): %v", in, err)
			}
			zone, _ := ts.Zone()
			if !strings.HasPrefix(zone, "CE") {
				t.Errorf("zone = %q, want CET or CEST", zone)
			}
			if ts.Hour() != 17 || ts.Minute() != 0 {
				t.Errorf("got %v, want 17:00 in Rome", ts)
			}
		})
	}
}

func TestParseLocalWallClock_RejectsTimezoneSuffixes(t *testing.T) {
	loc := time.UTC
	for _, in := range []string{
		"2026-04-30T17:00:00Z",
		"2026-04-30T17:00:00+02:00",
		"2026-04-30",
		"domani alle 5",
	} {
		t.Run(in, func(t *testing.T) {
			if _, err := parseLocalWallClock(in, loc); err == nil {
				t.Errorf("expected error for %q", in)
			}
		})
	}
}

func TestTaskTool_ListEmpty(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewTaskTool(store, nil, time.UTC)
	out, err := tool.Execute(t.Context(), map[string]any{"action": "list"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "No scheduled tasks") {
		t.Errorf("response = %q", out)
	}
}

func TestTaskTool_ListGroupsByStatus(t *testing.T) {
	store := newTestSchedStore(t)
	ctx := t.Context()

	at := time.Now().UTC().Add(time.Hour)
	mustSchedUpsert(t, store, &scheduler.Task{
		Name: "active-1", Kind: scheduler.KindReminder,
		ScheduleKind: scheduler.ScheduleAt, ScheduleAt: at, NextRunAt: at,
		Payload: "remember to test", Status: scheduler.StatusActive,
	})
	mustSchedUpsert(t, store, &scheduler.Task{
		Name: "done-1", Kind: scheduler.KindReminder,
		ScheduleKind: scheduler.ScheduleAt, ScheduleAt: at, NextRunAt: at,
		Status: scheduler.StatusDone,
	})
	mustSchedUpsert(t, store, &scheduler.Task{
		Name: "weekday-1", Kind: scheduler.KindReminder,
		ScheduleKind: scheduler.ScheduleDaily, ScheduleDaily: "10:00",
		ScheduleWeekdays: "mon,tue,wed,thu,fri", NextRunAt: at,
		Status: scheduler.StatusActive,
	})
	mustSchedUpsert(t, store, &scheduler.Task{
		Name: "every-1", Kind: scheduler.KindWikiMaintenance,
		ScheduleKind: scheduler.ScheduleEvery, ScheduleEveryMinutes: 60,
		NextRunAt: at, Status: scheduler.StatusActive,
	})

	tool := NewTaskTool(store, nil, time.UTC)
	out, err := tool.Execute(ctx, map[string]any{"action": "list"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		"4 task(s)",
		"## active",
		"## done",
		"`active-1`",
		"`done-1`",
		"`weekday-1`",
		"daily at 10:00 on mon,tue,wed,thu,fri",
		"`every-1`",
		"every 60 minutes",
		"\"remember to test\"",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestTaskTool_ListStatusFilter(t *testing.T) {
	store := newTestSchedStore(t)
	at := time.Now().UTC().Add(time.Hour)
	mustSchedUpsert(t, store, &scheduler.Task{
		Name: "a", Kind: scheduler.KindReminder,
		ScheduleKind: scheduler.ScheduleAt, ScheduleAt: at, NextRunAt: at,
		Status: scheduler.StatusActive,
	})
	mustSchedUpsert(t, store, &scheduler.Task{
		Name: "b", Kind: scheduler.KindReminder,
		ScheduleKind: scheduler.ScheduleAt, ScheduleAt: at, NextRunAt: at,
		Status: scheduler.StatusCancelled,
	})

	tool := NewTaskTool(store, nil, time.UTC)
	out, err := tool.Execute(t.Context(), map[string]any{"action": "list", "status": "active"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "`a`") {
		t.Errorf("active filter should include a: %s", out)
	}
	if strings.Contains(out, "`b`") {
		t.Errorf("active filter should drop b: %s", out)
	}
}

func TestTaskTool_Cancel(t *testing.T) {
	store := newTestSchedStore(t)
	at := time.Now().UTC().Add(time.Hour)
	mustSchedUpsert(t, store, &scheduler.Task{
		Name: "x", Kind: scheduler.KindReminder,
		ScheduleKind: scheduler.ScheduleAt, ScheduleAt: at, NextRunAt: at,
		Status: scheduler.StatusActive,
	})

	tool := NewTaskTool(store, nil, time.UTC)
	out, err := tool.Execute(t.Context(), map[string]any{"action": "cancel", "name": "x"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Cancelled task \"x\"") {
		t.Errorf("response = %q", out)
	}

	out, err = tool.Execute(t.Context(), map[string]any{"action": "cancel", "name": "x"})
	if err != nil {
		t.Fatalf("re-cancel Execute: %v", err)
	}
	if !strings.Contains(out, "No active task") {
		t.Errorf("re-cancel response = %q", out)
	}
}

func TestTaskTool_CancelMissingName(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewTaskTool(store, nil, time.UTC)
	if _, err := tool.Execute(t.Context(), map[string]any{"action": "cancel"}); err == nil {
		t.Error("expected error on missing name")
	}
}

func TestUserIDFromContext(t *testing.T) {
	ctx := context.Background()
	if got := UserIDFromContext(ctx); got != "" {
		t.Errorf("empty ctx = %q, want empty", got)
	}
	ctx = WithUserID(ctx, "42")
	if got := UserIDFromContext(ctx); got != "42" {
		t.Errorf("with-id ctx = %q, want 42", got)
	}
	ctx = WithUserID(ctx, "")
	if got := UserIDFromContext(ctx); got != "42" {
		t.Errorf("after empty WithUserID = %q, want 42", got)
	}
}

func mustSchedUpsert(t *testing.T, store *scheduler.Store, task *scheduler.Task) {
	t.Helper()
	if _, err := store.Upsert(context.Background(), task); err != nil {
		t.Fatalf("Upsert(%s): %v", task.Name, err)
	}
}
