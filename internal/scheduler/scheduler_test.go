package scheduler

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "scheduler.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestParseDailyTime(t *testing.T) {
	cases := []struct {
		in        string
		wantHour  int
		wantMin   int
		shouldErr bool
	}{
		{"00:00", 0, 0, false},
		{"03:00", 3, 0, false},
		{"23:59", 23, 59, false},
		{"24:00", 0, 0, true},
		{"3:00", 0, 0, true}, // not zero-padded
		{"03:60", 0, 0, true},
		{"abc", 0, 0, true},
		{"", 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			h, m, err := ParseDailyTime(tc.in)
			if tc.shouldErr {
				if err == nil {
					t.Errorf("expected error for %q", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if h != tc.wantHour || m != tc.wantMin {
				t.Errorf("got (%d,%d), want (%d,%d)", h, m, tc.wantHour, tc.wantMin)
			}
		})
	}
}

func TestNextDailyRun(t *testing.T) {
	loc := time.UTC

	// Before today's 03:00 — next should be today at 03:00.
	after := time.Date(2026, 4, 30, 1, 0, 0, 0, loc)
	got, err := NextDailyRun("03:00", loc, after)
	if err != nil {
		t.Fatalf("NextDailyRun: %v", err)
	}
	want := time.Date(2026, 4, 30, 3, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("before-3am: got %v, want %v", got, want)
	}

	// At today's 03:00 — next must roll forward to tomorrow.
	after = time.Date(2026, 4, 30, 3, 0, 0, 0, loc)
	got, _ = NextDailyRun("03:00", loc, after)
	want = time.Date(2026, 5, 1, 3, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("at-3am: got %v, want %v", got, want)
	}

	// After today's 03:00 — next is tomorrow at 03:00.
	after = time.Date(2026, 4, 30, 12, 0, 0, 0, loc)
	got, _ = NextDailyRun("03:00", loc, after)
	want = time.Date(2026, 5, 1, 3, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("after-3am: got %v, want %v", got, want)
	}
}

func TestNormalizeWeekdays(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"fri,mon,wed", "mon,wed,fri"},
		{"weekdays", "mon,tue,wed,thu,fri"},
		{"feriali", "mon,tue,wed,thu,fri"},
		{"weekend", "sat,sun"},
		{"lun,ven", "mon,fri"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := NormalizeWeekdays(tc.in)
			if err != nil {
				t.Fatalf("NormalizeWeekdays: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	if _, err := NormalizeWeekdays("moonday"); err == nil {
		t.Fatal("expected invalid weekday error")
	}
}

func TestNextDailyRunOnWeekdays(t *testing.T) {
	loc := time.UTC

	// Friday after the scheduled time should skip the weekend.
	after := time.Date(2026, 5, 1, 11, 0, 0, 0, loc) // Friday
	got, err := NextDailyRunOnWeekdays("10:00", "mon,tue,wed,thu,fri", loc, after)
	if err != nil {
		t.Fatalf("NextDailyRunOnWeekdays: %v", err)
	}
	want := time.Date(2026, 5, 4, 10, 0, 0, 0, loc) // Monday
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Friday before the scheduled time is still allowed.
	after = time.Date(2026, 5, 1, 9, 0, 0, 0, loc)
	got, err = NextDailyRunOnWeekdays("10:00", "mon,tue,wed,thu,fri", loc, after)
	if err != nil {
		t.Fatalf("NextDailyRunOnWeekdays: %v", err)
	}
	want = time.Date(2026, 5, 1, 10, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestStore_UpsertAndGet(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	at := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	in := &Task{
		Name:         "remind-me",
		Kind:         KindReminder,
		Payload:      "Buy milk",
		ScheduleKind: ScheduleAt,
		ScheduleAt:   at,
		NextRunAt:    at,
	}
	out, err := store.Upsert(ctx, in)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if out.ID == 0 {
		t.Error("ID should be assigned")
	}
	if out.Name != "remind-me" {
		t.Errorf("name = %q", out.Name)
	}
	if out.Status != StatusActive {
		t.Errorf("status = %q, want active", out.Status)
	}
	if !out.ScheduleAt.Equal(at) {
		t.Errorf("schedule_at = %v, want %v", out.ScheduleAt, at)
	}

	// Upsert is idempotent on name — same name, new payload, must update.
	in.Payload = "Buy bread"
	in.NextRunAt = at.Add(time.Hour)
	if _, err := store.Upsert(ctx, in); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	got, err := store.GetByName(ctx, "remind-me")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.Payload != "Buy bread" {
		t.Errorf("payload = %q, want updated", got.Payload)
	}
	if got.ID != out.ID {
		t.Errorf("ID changed across upsert: %d → %d", out.ID, got.ID)
	}

	dailyAt := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	if _, err := store.Upsert(ctx, &Task{
		Name: "business-days", Kind: KindReminder,
		ScheduleKind: ScheduleDaily, ScheduleDaily: "10:00",
		ScheduleWeekdays: "fri,mon,tue,wed,thu",
		NextRunAt:        dailyAt,
	}); err != nil {
		t.Fatalf("daily weekdays Upsert: %v", err)
	}
	got, err = store.GetByName(ctx, "business-days")
	if err != nil {
		t.Fatalf("GetByName business-days: %v", err)
	}
	if got.ScheduleWeekdays != "mon,tue,wed,thu,fri" {
		t.Errorf("ScheduleWeekdays = %q, want canonical weekdays", got.ScheduleWeekdays)
	}
}

func TestStore_RejectsInvalidSchedule(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// daily kind without daily string
	if _, err := store.Upsert(ctx, &Task{
		Name: "bad", Kind: KindReminder,
		ScheduleKind: ScheduleDaily, NextRunAt: time.Now().UTC(),
	}); err == nil {
		t.Error("expected error: missing schedule_daily")
	}

	// at kind with both fields
	if _, err := store.Upsert(ctx, &Task{
		Name: "bad", Kind: KindReminder,
		ScheduleKind:  ScheduleAt,
		ScheduleAt:    time.Now().Add(time.Hour),
		ScheduleDaily: "03:00",
		NextRunAt:     time.Now().UTC(),
	}); err == nil {
		t.Error("expected error: both schedule fields populated")
	}

	// unknown kind
	if _, err := store.Upsert(ctx, &Task{
		Name: "bad", Kind: KindReminder,
		ScheduleKind: "weekly", NextRunAt: time.Now().UTC(),
	}); err == nil {
		t.Error("expected error: unknown schedule_kind")
	}

	// bad daily string
	if _, err := store.Upsert(ctx, &Task{
		Name: "bad", Kind: KindReminder,
		ScheduleKind: ScheduleDaily, ScheduleDaily: "3am",
		NextRunAt: time.Now().UTC(),
	}); err == nil {
		t.Error("expected error: bad daily string")
	}

	// weekdays are only valid with daily
	if _, err := store.Upsert(ctx, &Task{
		Name: "bad", Kind: KindReminder,
		ScheduleKind: ScheduleEvery, ScheduleEveryMinutes: 60,
		ScheduleWeekdays: "mon", NextRunAt: time.Now().UTC(),
	}); err == nil {
		t.Error("expected error: weekdays with every")
	}

	if _, err := store.Upsert(ctx, &Task{
		Name: "bad", Kind: KindReminder,
		ScheduleKind: ScheduleDaily, ScheduleDaily: "03:00",
		ScheduleWeekdays: "moonday", NextRunAt: time.Now().UTC(),
	}); err == nil {
		t.Error("expected error: bad weekday")
	}
}

func TestStore_DueTasks(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	mustUpsert(t, store, &Task{
		Name: "past", Kind: KindReminder,
		ScheduleKind: ScheduleAt, ScheduleAt: now.Add(-time.Minute),
		NextRunAt: now.Add(-time.Minute),
	})
	mustUpsert(t, store, &Task{
		Name: "future", Kind: KindReminder,
		ScheduleKind: ScheduleAt, ScheduleAt: now.Add(time.Hour),
		NextRunAt: now.Add(time.Hour),
	})

	due, err := store.DueTasks(ctx, now)
	if err != nil {
		t.Fatalf("DueTasks: %v", err)
	}
	if len(due) != 1 || due[0].Name != "past" {
		names := make([]string, len(due))
		for i, t := range due {
			names[i] = t.Name
		}
		t.Errorf("due = %v, want [past]", names)
	}
}

func TestStore_Cancel(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	mustUpsert(t, store, &Task{
		Name: "x", Kind: KindReminder,
		ScheduleKind: ScheduleAt, ScheduleAt: now.Add(time.Hour),
		NextRunAt: now.Add(time.Hour),
	})

	ok, err := store.Cancel(ctx, "x")
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if !ok {
		t.Error("Cancel returned false for active task")
	}

	got, err := store.GetByName(ctx, "x")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.Status != StatusCancelled {
		t.Errorf("status = %q, want cancelled", got.Status)
	}

	// Cancelling again is a no-op (status already cancelled).
	ok, _ = store.Cancel(ctx, "x")
	if ok {
		t.Error("re-cancel should report false")
	}

	// Unknown name returns false, not error.
	ok, err = store.Cancel(ctx, "missing")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if ok {
		t.Error("missing-name cancel should return false")
	}
}

func TestStore_RecordManualRunPreservesSchedule(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	next := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	task, err := store.Upsert(ctx, &Task{
		Name:                 "manual",
		Kind:                 KindAgentJob,
		Payload:              "check sources",
		ScheduleKind:         ScheduleEvery,
		ScheduleEveryMinutes: 60,
		NextRunAt:            next,
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	ranAt := time.Now().UTC().Truncate(time.Second)
	if err := store.RecordManualRun(ctx, task.ID, ranAt, ""); err != nil {
		t.Fatalf("RecordManualRun: %v", err)
	}
	got, err := store.GetByName(ctx, "manual")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.LastRunAt.IsZero() {
		t.Fatal("last_run_at not recorded")
	}
	if !got.NextRunAt.Equal(next) {
		t.Fatalf("next_run_at = %v, want preserved %v", got.NextRunAt, next)
	}
	if got.Status != StatusActive {
		t.Fatalf("status = %s, want active", got.Status)
	}
}

func TestStore_RecordAgentJobResultPreservesAcrossUpsert(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	next := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	task, err := store.Upsert(ctx, &Task{
		Name:                 "agent-output",
		Kind:                 KindAgentJob,
		Payload:              "check memory",
		ScheduleKind:         ScheduleEvery,
		ScheduleEveryMinutes: 60,
		NextRunAt:            next,
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := store.RecordAgentJobResult(ctx, task.ID, "short report", `{"llm_calls":1}`, "sig-1"); err != nil {
		t.Fatalf("RecordAgentJobResult: %v", err)
	}
	got, err := store.GetByName(ctx, "agent-output")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.LastOutput != "short report" || got.LastMetricsJSON != `{"llm_calls":1}` || got.WakeSignature != "sig-1" {
		t.Fatalf("agent job result fields = %+v", got)
	}

	if _, err := store.Upsert(ctx, &Task{
		Name:                 "agent-output",
		Kind:                 KindAgentJob,
		Payload:              "updated goal",
		ScheduleKind:         ScheduleEvery,
		ScheduleEveryMinutes: 30,
		NextRunAt:            next.Add(time.Hour),
	}); err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	got, err = store.GetByName(ctx, "agent-output")
	if err != nil {
		t.Fatalf("GetByName after upsert: %v", err)
	}
	if got.LastOutput != "short report" || got.LastMetricsJSON != `{"llm_calls":1}` || got.WakeSignature != "sig-1" {
		t.Fatalf("agent job result was not preserved across upsert: %+v", got)
	}
}

func TestStore_List(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	mustUpsert(t, store, &Task{
		Name: "later", Kind: KindReminder,
		ScheduleKind: ScheduleAt, ScheduleAt: now.Add(2 * time.Hour),
		NextRunAt: now.Add(2 * time.Hour),
	})
	mustUpsert(t, store, &Task{
		Name: "sooner", Kind: KindReminder,
		ScheduleKind: ScheduleAt, ScheduleAt: now.Add(time.Hour),
		NextRunAt: now.Add(time.Hour),
	})

	all, err := store.List(ctx, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 2 || all[0].Name != "sooner" || all[1].Name != "later" {
		names := make([]string, len(all))
		for i, t := range all {
			names[i] = t.Name
		}
		t.Errorf("List order = %v, want [sooner, later]", names)
	}
}

func TestScheduler_AdvanceForOneShotSuccess(t *testing.T) {
	store := newTestStore(t)
	s, _ := New(Config{Store: store, Dispatcher: noopDispatcher})
	at := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	task := &Task{Name: "x", Kind: KindReminder, ScheduleKind: ScheduleAt, ScheduleAt: at, NextRunAt: at}
	next, status, lastErr := s.advance(task, at, nil)
	if status != StatusDone {
		t.Errorf("status = %q, want done", status)
	}
	if !next.Equal(at) {
		t.Errorf("next = %v, want %v (frozen for audit)", next, at)
	}
	if lastErr != "" {
		t.Errorf("lastErr = %q, want empty", lastErr)
	}
}

func TestScheduler_AdvanceForOneShotFailure(t *testing.T) {
	store := newTestStore(t)
	s, _ := New(Config{Store: store, Dispatcher: noopDispatcher})
	at := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	task := &Task{Name: "x", Kind: KindReminder, ScheduleKind: ScheduleAt, ScheduleAt: at, NextRunAt: at}
	next, status, lastErr := s.advance(task, at, errBoom)
	if status != StatusFailed {
		t.Errorf("status = %q, want failed", status)
	}
	if !next.Equal(at) {
		t.Errorf("next = %v, want %v", next, at)
	}
	if lastErr != "boom" {
		t.Errorf("lastErr = %q, want boom", lastErr)
	}
}

func TestScheduler_AdvanceForDaily(t *testing.T) {
	store := newTestStore(t)
	s, _ := New(Config{Store: store, Dispatcher: noopDispatcher, Location: time.UTC})
	at := time.Date(2026, 5, 1, 3, 0, 0, 0, time.UTC)
	task := &Task{
		Name: "nightly", Kind: KindWikiMaintenance,
		ScheduleKind: ScheduleDaily, ScheduleDaily: "03:00",
		NextRunAt: at,
	}
	next, status, lastErr := s.advance(task, at, nil)
	if status != StatusActive {
		t.Errorf("status = %q, want active", status)
	}
	wantNext := time.Date(2026, 5, 2, 3, 0, 0, 0, time.UTC)
	if !next.Equal(wantNext) {
		t.Errorf("next = %v, want %v", next, wantNext)
	}
	if lastErr != "" {
		t.Errorf("lastErr = %q, want empty on success", lastErr)
	}

	// Daily + dispatch failure: still reschedules for tomorrow but
	// records lastErr (not status=failed — recurring tasks should keep
	// trying so a transient error doesn't kill the schedule).
	_, status, lastErr = s.advance(task, at, errBoom)
	if status != StatusActive {
		t.Errorf("daily failure status = %q, want active (keeps trying)", status)
	}
	if lastErr != "boom" {
		t.Errorf("lastErr = %q, want boom", lastErr)
	}
}

func TestScheduler_AdvanceForDailyWeekdays(t *testing.T) {
	store := newTestStore(t)
	s, _ := New(Config{Store: store, Dispatcher: noopDispatcher, Location: time.UTC})
	at := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC) // Friday
	task := &Task{
		Name: "market-watch", Kind: KindReminder,
		ScheduleKind: ScheduleDaily, ScheduleDaily: "10:00",
		ScheduleWeekdays: "mon,tue,wed,thu,fri",
		NextRunAt:        at,
	}
	next, status, _ := s.advance(task, at, nil)
	if status != StatusActive {
		t.Errorf("status = %q, want active", status)
	}
	want := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC) // Monday
	if !next.Equal(want) {
		t.Errorf("next = %v, want %v", next, want)
	}
}

// TestScheduler_Autonomous is the slice-8 autonomy proof. The goal is
// to schedule a task and then *do nothing* — the scheduler goroutine
// should fire it on its own.
func TestScheduler_Autonomous(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var fired atomic.Int32
	var firedTask *Task
	var firedMu sync.Mutex

	dispatcher := func(_ context.Context, task *Task) error {
		firedMu.Lock()
		firedTask = task
		firedMu.Unlock()
		fired.Add(1)
		return nil
	}

	s, err := New(Config{
		Store:        store,
		Dispatcher:   dispatcher,
		TickInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	at := time.Now().UTC().Add(500 * time.Millisecond)
	if _, err := store.Upsert(ctx, &Task{
		Name: "autonomous-test", Kind: KindReminder,
		Payload:      "the scheduler ran without me",
		ScheduleKind: ScheduleAt, ScheduleAt: at, NextRunAt: at,
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	s.Start(ctx)
	defer s.Stop()

	// Wait up to 3s for the dispatcher to be called. No polling other
	// than this short loop — the scheduler is supposed to do the work.
	deadline := time.Now().Add(3 * time.Second)
	for fired.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if fired.Load() == 0 {
		t.Fatal("scheduler never fired the task within 3s — autonomy broken")
	}
	if fired.Load() > 1 {
		t.Errorf("task fired %d times, want exactly 1 (one-shot)", fired.Load())
	}

	firedMu.Lock()
	if firedTask == nil || firedTask.Name != "autonomous-test" {
		t.Errorf("dispatcher saw wrong task: %+v", firedTask)
	}
	firedMu.Unlock()

	// One-shot must be StatusDone after firing — proves the post-fire
	// store update also runs autonomously.
	got, err := store.GetByName(ctx, "autonomous-test")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.Status != StatusDone {
		t.Errorf("status = %q, want done", got.Status)
	}
	if got.LastRunAt.IsZero() {
		t.Error("last_run_at should be set after firing")
	}
}

// TestScheduler_AutonomousDailyReschedules schedules a daily task and
// verifies that after firing it stays active and gets next_run_at
// pushed to tomorrow — the recurring path of the autonomy contract.
func TestScheduler_AutonomousDailyReschedules(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var fired atomic.Int32
	dispatcher := func(_ context.Context, task *Task) error {
		fired.Add(1)
		return nil
	}

	s, err := New(Config{
		Store:        store,
		Dispatcher:   dispatcher,
		Location:     time.UTC,
		TickInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Place next_run_at in the immediate past so the first tick fires
	// it. Schedule_daily is whatever — what matters is that advance()
	// rolls it forward.
	now := time.Now().UTC()
	wantDaily := "03:00"
	if _, err := store.Upsert(ctx, &Task{
		Name: "nightly-test", Kind: KindWikiMaintenance,
		ScheduleKind: ScheduleDaily, ScheduleDaily: wantDaily,
		NextRunAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	s.Start(ctx)
	defer s.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for fired.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if fired.Load() == 0 {
		t.Fatal("daily scheduler never fired within 2s")
	}

	got, err := store.GetByName(ctx, "nightly-test")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.Status != StatusActive {
		t.Errorf("daily task status = %q, want still active", got.Status)
	}
	if !got.NextRunAt.After(now) {
		t.Errorf("next_run_at not advanced: %v vs now %v", got.NextRunAt, now)
	}
	// Roughly: next run should be the next 03:00 UTC after now.
	wantNext, _ := NextDailyRun(wantDaily, time.UTC, now)
	if !got.NextRunAt.Equal(wantNext) {
		t.Errorf("next_run_at = %v, want %v", got.NextRunAt, wantNext)
	}
}

// TestScheduler_PicksUpStaleTaskAfterRestart simulates a process
// restart: store the task with next_run_at in the past, instantiate a
// fresh scheduler, prove it fires the missed task on its first tick.
func TestScheduler_PicksUpStaleTaskAfterRestart(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var fired atomic.Int32
	dispatcher := func(_ context.Context, _ *Task) error {
		fired.Add(1)
		return nil
	}

	// Task fires-due was 5 minutes ago — a restart should pick this up.
	staleAt := time.Now().UTC().Add(-5 * time.Minute)
	mustUpsert(t, store, &Task{
		Name: "missed-while-down", Kind: KindReminder,
		ScheduleKind: ScheduleAt, ScheduleAt: staleAt, NextRunAt: staleAt,
	})

	s, _ := New(Config{
		Store:        store,
		Dispatcher:   dispatcher,
		TickInterval: 100 * time.Millisecond,
	})
	s.Start(ctx)
	defer s.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for fired.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if fired.Load() == 0 {
		t.Fatal("scheduler did not pick up missed task on restart")
	}
}

func TestScheduler_StopIsIdempotent(t *testing.T) {
	store := newTestStore(t)
	s, _ := New(Config{Store: store, Dispatcher: noopDispatcher, TickInterval: 100 * time.Millisecond})
	s.Start(context.Background())
	s.Stop()
	s.Stop() // second call must not panic
}

func TestNew_RejectsMissingDeps(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Error("expected error: missing store + dispatcher")
	}
	if _, err := New(Config{Store: &Store{}}); err == nil {
		t.Error("expected error: missing dispatcher")
	}
}

// helpers

func mustUpsert(t *testing.T, store *Store, task *Task) {
	t.Helper()
	if _, err := store.Upsert(context.Background(), task); err != nil {
		t.Fatalf("Upsert(%s): %v", task.Name, err)
	}
}

func noopDispatcher(_ context.Context, _ *Task) error { return nil }

type stringErr string

func (e stringErr) Error() string { return string(e) }

const errBoom = stringErr("boom")
