// Unit tests for the scheduler's pure logic (no DB): NewScheduler config
// resolution, the injectable clock, and the DuringQuietHours predicate (D-23).
// These ride the package goleak gate (main_test.go) with no background goroutines.
// The tick loop's due-selection + max-concurrent + graceful shutdown are exercised
// live in scheduler_integration_test.go (db_integration) where DueTasks needs PG.
package cron

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/obs"
	"github.com/chetto1983/aura/internal/readiness"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"
)

type observedDoneContext struct {
	context.Context
	once     sync.Once
	observed chan struct{}
}

func (c *observedDoneContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

func installSchedulerLifecycleBoundary(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	boundary, err := obs.NewBoundary(
		provider.Meter("aura.scheduler.lifecycle.test"),
		noop.NewTracerProvider().Tracer("aura.scheduler.lifecycle.test"),
		obs.BoundaryConfig{Operation: "scheduler_tick", State: "running", Count: obs.SchedulerTransitionsID},
	)
	if err != nil {
		t.Fatalf("NewBoundary: %v", err)
	}
	previous := schedulerLifecycleBoundary
	schedulerLifecycleBoundary = boundary
	t.Cleanup(func() {
		schedulerLifecycleBoundary = previous
		_ = provider.Shutdown(context.Background())
	})
	return reader
}

func schedulerLifecycleInFlight(t *testing.T, reader *sdkmetric.ManualReader) (int64, bool) {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("collect scheduler lifecycle metrics: %v", err)
	}
	for _, scope := range collected.ScopeMetrics {
		for _, instrument := range scope.Metrics {
			if instrument.Name != "aura.boundary.in_flight" {
				continue
			}
			sum, ok := instrument.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("aura.boundary.in_flight data = %T, want int64 sum", instrument.Data)
			}
			for _, point := range sum.DataPoints {
				for _, attr := range point.Attributes.ToSlice() {
					if string(attr.Key) == string(obs.AttributeOperation) && attr.Value.AsString() == "scheduler_tick" {
						return point.Value, true
					}
				}
			}
		}
	}
	return 0, false
}

func TestSchedulerLifecycleMetricRepresentsEnabledRunningOnly(t *testing.T) {
	t.Run("disabled does not advertise running", func(t *testing.T) {
		reader := installSchedulerLifecycleBoundary(t)
		base, cancel := context.WithCancel(context.Background())
		ctx := &observedDoneContext{Context: base, observed: make(chan struct{})}
		s := NewScheduler(nil, nil, SchedulerConfig{Disabled: true})
		done := make(chan error, 1)
		go func() { done <- s.Start(ctx) }()
		select {
		case <-ctx.observed:
		case <-time.After(time.Second):
			t.Fatal("disabled scheduler did not enter its cancellation wait")
		}
		if value, exists := schedulerLifecycleInFlight(t, reader); exists {
			t.Fatalf("disabled scheduler exported scheduler_tick in-flight=%d, want no series", value)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("disabled scheduler returned %v", err)
		}
	})

	t.Run("enabled advertises running until cancellation", func(t *testing.T) {
		reader := installSchedulerLifecycleBoundary(t)
		store := storeWithFake(&cronFakeDBTX{queryErr: errors.New("boot scan unavailable")})
		s := NewScheduler(nil, store, SchedulerConfig{TickInterval: time.Hour})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- s.Start(ctx) }()

		deadline := time.Now().Add(time.Second)
		for {
			if value, exists := schedulerLifecycleInFlight(t, reader); exists && value == 1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("enabled scheduler never exported scheduler_tick in-flight=1")
			}
			time.Sleep(time.Millisecond)
		}

		cancel()
		if err := <-done; err != nil {
			t.Fatalf("enabled scheduler returned %v", err)
		}
		if value, exists := schedulerLifecycleInFlight(t, reader); !exists || value != 0 {
			t.Fatalf("stopped scheduler in-flight = %d, exists=%v; want zero series", value, exists)
		}
	})
}

func TestSchedulerHeartbeatAdvancesOnlyAfterSuccessfulScan(t *testing.T) {
	base := time.Date(2026, 7, 21, 11, 0, 0, 0, time.UTC)
	now := base
	snapshot := readiness.NewSnapshot(readiness.Config{
		Now:                 func() time.Time { return now },
		SchedulerMaxAge:     time.Minute,
		MigrationCompatible: true,
		SchedulerEnabled:    true,
	})
	snapshot.MarkListenerRunning()
	s := NewScheduler(nil, nil, SchedulerConfig{
		Now:       func() time.Time { return now },
		Readiness: snapshot,
	})
	s.scan = func(context.Context) error {
		if !slices.Contains(snapshot.Reasons(), readiness.CodeSchedulerStalled) {
			t.Fatal("scheduler must remain stalled until the scan completion point")
		}
		return nil // successful scan with zero due work
	}

	if err := s.runScheduledScan(context.Background()); err != nil {
		t.Fatalf("successful scan: %v", err)
	}
	if got := snapshot.Reasons(); len(got) != 0 {
		t.Fatalf("successful empty scan reasons = %v, want ready", got)
	}
	if got := s.LastTick(); !got.Equal(base) {
		t.Fatalf("LastTick = %s, want completion time %s", got, base)
	}

	previous := s.LastTick()
	now = now.Add(2 * time.Minute)
	s.scan = func(context.Context) error { return errors.New("claim path unavailable") }
	if err := s.runScheduledScan(context.Background()); err == nil {
		t.Fatal("failed scan returned nil")
	}
	if got := s.LastTick(); !got.Equal(previous) {
		t.Fatalf("failed scan advanced progress to %s; want %s", got, previous)
	}
	s.markTerminalFailure(errors.New("terminal loop failure"))
	if !slices.Contains(snapshot.Reasons(), readiness.CodeSchedulerStalled) {
		t.Fatalf("terminal failure reasons = %v, want scheduler_stalled", snapshot.Reasons())
	}
}

func TestSchedulerDisabledStartsNoHeartbeatAndJoinsCancellation(t *testing.T) {
	var scans atomic.Int32
	s := NewScheduler(nil, nil, SchedulerConfig{Disabled: true})
	s.scan = func(context.Context) error {
		scans.Add(1)
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Start(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("disabled scheduler returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("disabled scheduler did not join cancellation")
	}
	if got := scans.Load(); got != 0 {
		t.Fatalf("disabled scheduler ran %d scans, want zero", got)
	}
}

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

	// An injected lookup mirroring the real handler flags: reminder/agent_job/backup
	// reschedule (re-fire once on recovery — reminder flipped by fix-plan 1.2), the
	// periodic sweeps (skill_ttl_sweep) do not.
	reschedules := map[TaskKind]bool{
		KindReminder:       true,
		KindAgentJob:       true,
		KindBackupPostgres: true,
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
		KindSkillTTLSweep: &fakeHandler{meta: HandlerMeta{Kind: KindSkillTTLSweep, ReschedulesOnRecovery: false}},
		KindAgentJob:      &fakeHandler{meta: HandlerMeta{Kind: KindAgentJob, ReschedulesOnRecovery: true}},
	}, DispatchDeps{Store: &fakeCompleter{}})

	if d.ReschedulesOnRecovery(KindSkillTTLSweep) {
		t.Error("a sweep handler reports ReschedulesOnRecovery=false")
	}
	if !d.ReschedulesOnRecovery(KindAgentJob) {
		t.Error("an agent_job handler reports ReschedulesOnRecovery=true")
	}
	if d.ReschedulesOnRecovery(TaskKind("unmapped")) {
		t.Error("an unroutable kind must report false (the dispatcher fails it loud)")
	}
}

func TestSchedulerReadinessMaxAge(t *testing.T) {
	cases := []struct {
		name string
		tick time.Duration
		env  string
		want time.Duration
	}{
		{"tick 30s floors at 90s", 30 * time.Second, "", 90 * time.Second},
		{"tick 120s derives 3x", 120 * time.Second, "", 360 * time.Second},
		{"env override wins verbatim", 30 * time.Second, "45", 45 * time.Second},
		{"env unset falls to derived", 45 * time.Second, "", 135 * time.Second},
		{"env zero falls to derived", 120 * time.Second, "0", 360 * time.Second},
		{"env negative falls to derived", 120 * time.Second, "-5", 360 * time.Second},
		{"env garbage falls to derived", 120 * time.Second, "not-a-number", 360 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("AURA_SCHEDULER_READY_MAX_STALE_SEC", c.env)
			if got := schedulerReadinessMaxAge(c.tick); got != c.want {
				t.Errorf("schedulerReadinessMaxAge(%s) = %s, want %s", c.tick, got, c.want)
			}
		})
	}
}

// TestNewScheduler_PushesReadinessMaxAge proves NewScheduler derives the /readyz
// staleness window from the RESOLVED tick (not a re-read of the env) and pushes it
// into the readiness snapshot, so an operator-widened tick does not trip a false
// scheduler_stalled between ticks (fix-plan 1.4 residual).
func TestNewScheduler_PushesReadinessMaxAge(t *testing.T) {
	base := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	now := base
	snapshot := readiness.NewSnapshot(readiness.Config{
		Now:                 func() time.Time { return now },
		MigrationCompatible: true,
	})
	snapshot.MarkListenerRunning()

	NewScheduler(nil, nil, SchedulerConfig{
		Now:          func() time.Time { return now },
		TickInterval: 120 * time.Second,
		Readiness:    snapshot,
	})
	snapshot.MarkSchedulerProgress()

	now = base.Add(100 * time.Second) // stale for the historical 90s floor, fresh for 3x120s=360s
	if got := snapshot.Reasons(); slices.Contains(got, readiness.CodeSchedulerStalled) {
		t.Fatalf("Reasons() = %v, want no scheduler_stalled at 100s under a 120s-tick window", got)
	}

	now = base.Add(400 * time.Second)
	if got := snapshot.Reasons(); !slices.Contains(got, readiness.CodeSchedulerStalled) {
		t.Fatalf("Reasons() = %v, want scheduler_stalled past the derived 360s window", got)
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

func (s *Scheduler) markTerminalFailure(err error) {
	if s.readiness != nil {
		s.readiness.MarkSchedulerFailure(err)
	}
}
