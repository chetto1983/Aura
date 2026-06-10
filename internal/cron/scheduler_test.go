// Unit tests for the scheduler's pure logic (no DB): NewScheduler config
// resolution, the injectable clock, and the DuringQuietHours predicate (D-23).
// These ride the package goleak gate (main_test.go) with no background goroutines.
// The tick loop's due-selection + max-concurrent + graceful shutdown are exercised
// live in scheduler_integration_test.go (db_integration) where DueTasks needs PG.
package cron

import (
	"testing"
	"time"
)

func TestNewScheduler_DefaultsAndClock(t *testing.T) {
	frozen := time.Date(2026, 6, 4, 9, 30, 0, 0, time.UTC)
	s := NewScheduler(nil, nil, SchedulerConfig{Now: func() time.Time { return frozen }})

	if !s.Now().Equal(frozen) {
		t.Errorf("injected clock = %s, want %s", s.Now(), frozen)
	}
	if s.maxConcurrent != defaultMaxConcurrentRuns {
		t.Errorf("maxConcurrent default = %d, want %d", s.maxConcurrent, defaultMaxConcurrentRuns)
	}
	if s.tickInterval != defaultTickInterval {
		t.Errorf("tickInterval default = %s, want %s", s.tickInterval, defaultTickInterval)
	}
	if s.maxConcurrent >= 10 {
		t.Errorf("maxConcurrent %d must be < db pool MaxConns (10) for held-conn headroom (Pitfall 2)", s.maxConcurrent)
	}
}

func TestSchedulerLastTick(t *testing.T) {
	frozen := time.Date(2026, 6, 4, 9, 45, 0, 0, time.UTC)
	s := NewScheduler(nil, nil, SchedulerConfig{Now: func() time.Time { return frozen }})
	if !s.LastTick().IsZero() {
		t.Fatalf("new scheduler LastTick = %s, want zero", s.LastTick())
	}
	s.markTick()
	if got := s.LastTick(); !got.Equal(frozen) {
		t.Fatalf("LastTick = %s, want %s", got, frozen)
	}
}

func TestNewScheduler_ConfigOverridesEnv(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_MAX_CONCURRENT_RUNS", "7")
	t.Setenv("AURA_SCHEDULER_TICK_SECONDS", "15")

	// Explicit config wins over env.
	s := NewScheduler(nil, nil, SchedulerConfig{MaxConcurrent: 3, TickInterval: 5 * time.Second})
	if s.maxConcurrent != 3 || s.tickInterval != 5*time.Second {
		t.Errorf("explicit config not honored: cap=%d tick=%s", s.maxConcurrent, s.tickInterval)
	}

	// Env fills in when config is zero.
	s2 := NewScheduler(nil, nil, SchedulerConfig{})
	if s2.maxConcurrent != 7 {
		t.Errorf("env cap = %d, want 7", s2.maxConcurrent)
	}
	if s2.tickInterval != 15*time.Second {
		t.Errorf("env tick = %s, want 15s", s2.tickInterval)
	}
}

func TestEnvInt_MalformedFallsBack(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_MAX_CONCURRENT_RUNS", "not-a-number")
	s := NewScheduler(nil, nil, SchedulerConfig{})
	if s.maxConcurrent != defaultMaxConcurrentRuns {
		t.Errorf("malformed env cap = %d, want default %d", s.maxConcurrent, defaultMaxConcurrentRuns)
	}
}

func TestDuringQuietHours(t *testing.T) {
	mk := func(window string, at time.Time) bool {
		t.Setenv("AURA_SCHEDULER_QUIET_HOURS", window)
		s := NewScheduler(nil, nil, SchedulerConfig{Now: func() time.Time { return at }})
		return s.DuringQuietHours("UTC")
	}
	tAt := func(h, m int) time.Time { return time.Date(2026, 6, 4, h, m, 0, 0, time.UTC) }

	cases := []struct {
		name   string
		window string
		at     time.Time
		want   bool
	}{
		{"wrap-around inside late night", "23:00-07:30", tAt(23, 30), true},
		{"wrap-around inside early morning", "23:00-07:30", tAt(6, 0), true},
		{"wrap-around outside midday", "23:00-07:30", tAt(12, 0), false},
		{"wrap-around at end boundary excluded", "23:00-07:30", tAt(7, 30), false},
		{"wrap-around at start boundary included", "23:00-07:30", tAt(23, 0), true},
		{"same-day window inside", "09:00-17:00", tAt(12, 0), true},
		{"same-day window before", "09:00-17:00", tAt(8, 0), false},
		{"same-day end excluded", "09:00-17:00", tAt(17, 0), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mk(c.window, c.at); got != c.want {
				t.Errorf("DuringQuietHours(%q at %s) = %v, want %v", c.window, c.at.Format("15:04"), got, c.want)
			}
		})
	}
}

func TestQuietHoursEnd(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_QUIET_HOURS", "23:00-07:30")
	late := time.Date(2026, 6, 4, 23, 30, 0, 0, time.UTC)
	s := NewScheduler(nil, nil, SchedulerConfig{Now: func() time.Time { return late }})
	end, ok := s.QuietHoursEnd("UTC")
	if !ok {
		t.Fatal("late-night wrap-around quiet window should return an end instant")
	}
	want := time.Date(2026, 6, 5, 7, 30, 0, 0, time.UTC)
	if !end.Equal(want) {
		t.Fatalf("quiet-hours end = %s, want %s", end, want)
	}

	midday := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return midday }
	if end, ok := s.QuietHoursEnd("UTC"); ok {
		t.Fatalf("outside quiet hours returned end=%s", end)
	}
}

// TestReschedulesOnRecoverySeam proves the M-g catch-up gate (firesOnRecovery): the
// injected ReschedulesOnRecovery lookup decides whether an overdue task is re-fired at
// boot, and a nil lookup fails SAFE to the historical always-fire behavior. Before the
// fix the flag was never read; this asserts the seam is wired from SchedulerConfig
// through to firesOnRecovery.
func TestReschedulesOnRecoverySeam(t *testing.T) {
	// Nil lookup (tests / pre-wired callers): always fire (fail-safe, never drop).
	nilSeam := NewScheduler(nil, nil, SchedulerConfig{})
	for _, kind := range []TaskKind{KindReminder, KindAgentJob, KindSkillTTLSweep} {
		if !nilSeam.firesOnRecovery(kind) {
			t.Errorf("nil ReschedulesOnRecovery lookup must fail-safe to fire for %q", kind)
		}
	}

	// An injected lookup mirroring the real handler flags: agent_job/backup reschedule
	// (re-fire on recovery), reminder/skill_ttl_sweep do not.
	reschedules := map[TaskKind]bool{
		KindReminder:       false,
		KindAgentJob:       true,
		KindBackupPostgres: true,
		KindBackupNeo4j:    true,
		KindSkillTTLSweep:  false,
	}
	s := NewScheduler(nil, nil, SchedulerConfig{
		ReschedulesOnRecovery: func(kind TaskKind) bool { return reschedules[kind] },
	})
	for kind, want := range reschedules {
		if got := s.firesOnRecovery(kind); got != want {
			t.Errorf("firesOnRecovery(%q) = %v, want %v", kind, got, want)
		}
	}
	// An unknown kind reports the lookup's value (here false, the map zero value) — the
	// dispatcher would fail an unroutable kind loud anyway.
	if s.firesOnRecovery(TaskKind("unknown")) {
		t.Error("an unknown kind must not be re-fired at catch-up")
	}
}

// TestDispatchReschedulesOnRecoveryLookup proves the *Dispatch seam the composition
// root passes into the scheduler: it reports a kind's HandlerMeta.ReschedulesOnRecovery
// and false for an unroutable kind.
func TestDispatchReschedulesOnRecoveryLookup(t *testing.T) {
	d := NewDispatch(map[TaskKind]Handler{
		KindReminder: &fakeHandler{meta: HandlerMeta{Kind: KindReminder, ReschedulesOnRecovery: false}},
		KindAgentJob: &fakeHandler{meta: HandlerMeta{Kind: KindAgentJob, ReschedulesOnRecovery: true}},
	}, DispatchDeps{Store: &fakeCompleter{}})

	if d.ReschedulesOnRecovery(KindReminder) {
		t.Error("a reminder handler reports ReschedulesOnRecovery=false")
	}
	if !d.ReschedulesOnRecovery(KindAgentJob) {
		t.Error("an agent_job handler reports ReschedulesOnRecovery=true")
	}
	if d.ReschedulesOnRecovery(TaskKind("unmapped")) {
		t.Error("an unroutable kind must report false (the dispatcher fails it loud)")
	}
}

func TestDuringQuietHours_UnsetOrMalformedIsNeverQuiet(t *testing.T) {
	at := time.Date(2026, 6, 4, 2, 0, 0, 0, time.UTC)
	// Unset.
	t.Setenv("AURA_SCHEDULER_QUIET_HOURS", "")
	s := NewScheduler(nil, nil, SchedulerConfig{Now: func() time.Time { return at }})
	if s.DuringQuietHours("UTC") {
		t.Error("unset quiet hours must be never-quiet (fail-open)")
	}
	// Malformed.
	for _, bad := range []string{"garbage", "25:00-07:30", "23:00", "23:00-99:99"} {
		t.Setenv("AURA_SCHEDULER_QUIET_HOURS", bad)
		if s.DuringQuietHours("UTC") {
			t.Errorf("malformed quiet hours %q must be never-quiet (fail-open)", bad)
		}
	}
}
