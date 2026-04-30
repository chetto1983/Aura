package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/scheduler"
)

func newTestSchedStore(t *testing.T) *scheduler.Store {
	t.Helper()
	store, err := scheduler.OpenStore(filepath.Join(t.TempDir(), "sched.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestScheduleTaskTool_OneShotReminder(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewScheduleTaskTool(store, time.UTC)
	ctx := WithUserID(t.Context(), "12345")

	at := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	out, err := tool.Execute(ctx, map[string]any{
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

func TestScheduleTaskTool_RejectsReminderWithoutUser(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewScheduleTaskTool(store, time.UTC)

	at := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	_, err := tool.Execute(t.Context(), map[string]any{
		"name": "x", "kind": "reminder", "at": at,
	})
	if err == nil {
		t.Fatal("expected error: reminder without user context")
	}
	if !strings.Contains(err.Error(), "authenticated user context") {
		t.Errorf("error message = %q", err.Error())
	}
}

func TestScheduleTaskTool_DailyWikiMaintenance(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewScheduleTaskTool(store, time.UTC)

	out, err := tool.Execute(t.Context(), map[string]any{
		"name":  "nightly",
		"kind":  "wiki_maintenance",
		"daily": "03:00",
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
	// wiki_maintenance must not capture a recipient (it's autonomous).
	if got.RecipientID != "" {
		t.Errorf("RecipientID = %q, want empty for wiki_maintenance", got.RecipientID)
	}
}

func TestScheduleTaskTool_RejectsBadInputs(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewScheduleTaskTool(store, time.UTC)
	ctx := WithUserID(t.Context(), "u")

	cases := []struct {
		name string
		args map[string]any
		hint string
	}{
		{"missing kind", map[string]any{"name": "a"}, "kind"},
		{"unknown kind", map[string]any{"name": "a", "kind": "foo"}, "unknown kind"},
		{"missing schedule", map[string]any{"name": "a", "kind": "reminder"}, "either at"},
		{"both schedules", map[string]any{
			"name": "a", "kind": "reminder",
			"at":    time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
			"daily": "03:00",
		}, "mutually exclusive"},
		{"past at", map[string]any{
			"name": "a", "kind": "reminder",
			"at": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
		}, "not in the future"},
		{"bad daily", map[string]any{
			"name": "a", "kind": "wiki_maintenance", "daily": "3am",
		}, ""}, // ParseDailyTime emits its own message
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

func TestScheduleTaskTool_NilStore(t *testing.T) {
	tool := NewScheduleTaskTool(nil, time.UTC)
	if _, err := tool.Execute(t.Context(), map[string]any{"name": "a", "kind": "wiki_maintenance", "daily": "03:00"}); err == nil {
		t.Error("expected error on nil store")
	}
}

func TestListTasksTool_Empty(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewListTasksTool(store)

	out, err := tool.Execute(t.Context(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "No scheduled tasks") {
		t.Errorf("response = %q", out)
	}
}

func TestListTasksTool_GroupsByStatus(t *testing.T) {
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

	tool := NewListTasksTool(store)
	out, err := tool.Execute(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, want := range []string{
		"2 task(s)",
		"## active",
		"## done",
		"`active-1`",
		"`done-1`",
		"\"remember to test\"",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestListTasksTool_StatusFilter(t *testing.T) {
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

	tool := NewListTasksTool(store)
	out, err := tool.Execute(t.Context(), map[string]any{"status": "active"})
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

func TestCancelTaskTool(t *testing.T) {
	store := newTestSchedStore(t)
	at := time.Now().UTC().Add(time.Hour)
	mustSchedUpsert(t, store, &scheduler.Task{
		Name: "x", Kind: scheduler.KindReminder,
		ScheduleKind: scheduler.ScheduleAt, ScheduleAt: at, NextRunAt: at,
		Status: scheduler.StatusActive,
	})

	tool := NewCancelTaskTool(store)
	out, err := tool.Execute(t.Context(), map[string]any{"name": "x"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "Cancelled task \"x\"") {
		t.Errorf("response = %q", out)
	}

	// Re-cancel: returns "no active task" not an error.
	out, err = tool.Execute(t.Context(), map[string]any{"name": "x"})
	if err != nil {
		t.Fatalf("re-cancel Execute: %v", err)
	}
	if !strings.Contains(out, "No active task") {
		t.Errorf("re-cancel response = %q", out)
	}
}

func TestCancelTaskTool_MissingName(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewCancelTaskTool(store)
	if _, err := tool.Execute(t.Context(), map[string]any{}); err == nil {
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
	// WithUserID("") should be a no-op so we don't accidentally clobber
	// an existing id.
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
