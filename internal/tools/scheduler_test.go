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

func TestScheduleTaskTool_DailyWeekdays(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewScheduleTaskTool(store, time.UTC)
	ctx := WithUserID(t.Context(), "u")

	out, err := tool.Execute(ctx, map[string]any{
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

func TestScheduleTaskTool_EveryMinutes(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewScheduleTaskTool(store, time.UTC)

	before := time.Now().UTC()
	out, err := tool.Execute(t.Context(), map[string]any{
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

func TestScheduleTaskTool_AgentJob(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewScheduleTaskTool(store, time.UTC)
	ctx := WithUserID(t.Context(), "12345")

	out, err := tool.Execute(ctx, map[string]any{
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
		{"missing schedule", map[string]any{"name": "a", "kind": "reminder"}, "provide one of"},
		{"both schedules", map[string]any{
			"name": "a", "kind": "reminder",
			"at":    time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
			"daily": "03:00",
		}, "mutually exclusive"},
		{"in plus at_local", map[string]any{
			"name": "a", "kind": "reminder",
			"in":       "5m",
			"at_local": "2026-04-30T17:00:00",
		}, "mutually exclusive"},
		{"past at", map[string]any{
			"name": "a", "kind": "reminder",
			"at": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
		}, "not in the future"},
		{"bad daily", map[string]any{
			"name": "a", "kind": "wiki_maintenance", "daily": "3am",
		}, ""}, // ParseDailyTime emits its own message
		{"agent job missing goal", map[string]any{
			"name": "a", "kind": "agent_job", "daily": "03:00",
		}, "agent_job payload goal required"},
		{"bad every", map[string]any{
			"name": "a", "kind": "wiki_maintenance", "every_minutes": float64(0),
		}, "every_minutes must be >= 1"},
		{"fractional every", map[string]any{
			"name": "a", "kind": "wiki_maintenance", "every_minutes": 1.5,
		}, "every_minutes must be an integer"},
		{"daily plus every", map[string]any{
			"name": "a", "kind": "wiki_maintenance", "daily": "03:00", "every_minutes": float64(60),
		}, "mutually exclusive"},
		{"weekdays without daily", map[string]any{
			"name": "a", "kind": "wiki_maintenance", "every_minutes": float64(60), "weekdays": []any{"mon"},
		}, "weekdays can only be used with daily"},
		{"bad weekday", map[string]any{
			"name": "a", "kind": "wiki_maintenance", "daily": "03:00", "weekdays": []any{"moonday"},
		}, "invalid weekday"},
		{"bad in", map[string]any{
			"name": "a", "kind": "wiki_maintenance", "in": "soon",
		}, "parse in"},
		{"non-positive in", map[string]any{
			"name": "a", "kind": "wiki_maintenance", "in": "-5m",
		}, "must be positive"},
		{"bad at_local format", map[string]any{
			"name": "a", "kind": "wiki_maintenance", "at_local": "domani alle 5",
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

func TestScheduleTaskTool_RelativeIn(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewScheduleTaskTool(store, time.UTC)
	ctx := WithUserID(t.Context(), "u")

	before := time.Now().UTC()
	out, err := tool.Execute(ctx, map[string]any{
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
	// next_run_at must be ~60s after the call. Allow a 5s window for
	// scheduling + db round-trip.
	delta := got.NextRunAt.Sub(before)
	if delta < 55*time.Second || delta > 75*time.Second {
		t.Errorf("next_run_at delta = %v, want ~60s", delta)
	}
	if got.ScheduleKind != scheduler.ScheduleAt {
		t.Errorf("ScheduleKind = %q, want at (in resolves to absolute)", got.ScheduleKind)
	}
}

func TestScheduleTaskTool_AtLocal(t *testing.T) {
	// Pin the location so the test is deterministic regardless of
	// where the test runs.
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Skipf("Europe/Rome tzdata unavailable: %v", err)
	}
	store := newTestSchedStore(t)
	tool := NewScheduleTaskTool(store, rome)
	ctx := WithUserID(t.Context(), "u")

	// Pick "tomorrow at 17:00 local" so we know it's strictly in the
	// future in Rome regardless of CET/CEST.
	tomorrow := time.Now().In(rome).AddDate(0, 0, 1)
	atLocal := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 17, 0, 0, 0, rome).
		Format("2006-01-02T15:04:05")

	out, err := tool.Execute(ctx, map[string]any{
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

func TestScheduleTaskTool_AtLocalRejectsPast(t *testing.T) {
	store := newTestSchedStore(t)
	tool := NewScheduleTaskTool(store, time.UTC)
	ctx := WithUserID(t.Context(), "u")

	// 1970 is comfortably in the past for any test environment.
	_, err := tool.Execute(ctx, map[string]any{
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
				// Rome uses CET/CEST depending on DST; either is fine.
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
		"2026-04-30T17:00:00Z",      // explicit UTC — at_local must not accept
		"2026-04-30T17:00:00+02:00", // explicit offset — same
		"2026-04-30",                // missing time
		"domani alle 5",             // not a timestamp at all
	} {
		t.Run(in, func(t *testing.T) {
			if _, err := parseLocalWallClock(in, loc); err == nil {
				t.Errorf("expected error for %q", in)
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

	tool := NewListTasksTool(store)
	out, err := tool.Execute(ctx, map[string]any{})
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
