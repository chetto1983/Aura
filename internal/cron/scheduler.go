package cron

// scheduler.go is the long-lived tick loop (D-02): every tick it selects due
// tasks (next_run_at<=Now) and claims each under a max-concurrent cap, driving the
// held-conn advisory-lock singleton (claim.go) + heartbeat (heartbeat.go). Boot
// runs the orphan scan + missed catch-up (recover.go) before the first tick.
//
// The clock is an injectable Now func (W8, budget.go precedent): a plain func
// field, NOT Go 1.26 synctest, so the loop's tests stay deterministic with zero
// background goroutines that would trip the goleak gate. The max-concurrent cap is
// sized strictly below the pool's MaxConns so the held conns never starve the tick
// query or a run's heartbeat (Pitfall 2).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chetto1983/aura/internal/readiness"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Default tick + concurrency knobs. The cap is deliberately below db.defaultMaxConns
// (10) so held run conns leave headroom for the tick DueTasks query (Pitfall 2).
const (
	defaultTickInterval      = 30 * time.Second
	defaultMaxConcurrentRuns = 4
	defaultHeartbeatInterval = 30 * time.Second
	staleRecoverySeconds     = 90 // last_heartbeat_at older than this → unknown_recovery (D-02)
)

// Dispatcher runs a claimed task's actual work (the per-TaskKind handlers land in
// 10-05). The tick loop owns the claim/heartbeat/release lifecycle and hands the
// dispatcher the held conn so its writes share the advisory-lock session. A nil
// dispatcher (tests / pre-10-05 wiring) makes the tick a claim-and-release probe.
type Dispatcher interface {
	Dispatch(ctx context.Context, task Task, c *Claim) error
}

type notificationSweeper interface {
	sweepNotifications(ctx context.Context) error
}

// Scheduler is the in-process tick loop owning the due-task claim lifecycle.
type Scheduler struct {
	// Now is the injectable clock (W8). Defaults to time.Now; tests inject a frozen
	// clock so tick selection and reschedule are deterministic with no real sleep.
	Now func() time.Time

	store         *Store
	pool          *pgxpool.Pool
	dispatch      Dispatcher
	maxConcurrent int
	tickInterval  time.Duration
	hbInterval    time.Duration
	lastTickUnix  atomic.Int64
	readiness     *readiness.Snapshot
	disabled      bool
	scan          func(context.Context) error
	// reschedulesOnRecovery reports a kind's HandlerMeta.ReschedulesOnRecovery (M-g):
	// the boot catch-up consults it so a handler that does NOT reschedule on recovery
	// (e.g. a reminder whose window has passed, or any future side-effecting handler)
	// is never auto-re-fired — its cadence still resumes, but the committed side effect
	// is not replayed. Nil (tests / pre-wired callers) preserves the historical
	// fire-every-overdue-task behavior for safety. Supplied by the composition root,
	// which owns the kind→handler map (the *Dispatch.ReschedulesOnRecovery seam).
	reschedulesOnRecovery func(kind TaskKind) bool
}

// SchedulerConfig carries explicit overrides; zero fields fall through to the
// AURA_SCHEDULER_* env then the builtin defaults.
type SchedulerConfig struct {
	Dispatch      Dispatcher
	MaxConcurrent int
	TickInterval  time.Duration
	Now           func() time.Time
	// Readiness receives scheduler scan progress and terminal failures.
	Readiness *readiness.Snapshot
	// Disabled keeps the lifecycle joined to ctx without starting a ticker or scan.
	Disabled bool
	// ReschedulesOnRecovery is the M-g catch-up seam: it reports whether a task kind's
	// handler reschedules (re-fires) on boot recovery. A nil func means "always fire"
	// (the historical behavior); the composition root passes the *Dispatch lookup so
	// the flag the handlers already declare is finally consulted.
	ReschedulesOnRecovery func(kind TaskKind) bool
}

// NewScheduler builds a Scheduler over an open pool + Store. It resolves the
// concurrency cap and tick interval from the explicit config, then the
// AURA_SCHEDULER_* env, then the defaults — and defaults Now to time.Now.
func NewScheduler(pool *pgxpool.Pool, store *Store, cfg SchedulerConfig) *Scheduler {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	maxC := cfg.MaxConcurrent
	if maxC <= 0 {
		maxC = envInt("AURA_SCHEDULER_MAX_CONCURRENT_RUNS", defaultMaxConcurrentRuns)
	}
	tick := cfg.TickInterval
	if tick <= 0 {
		tick = time.Duration(envInt("AURA_SCHEDULER_TICK_SECONDS", int(defaultTickInterval/time.Second))) * time.Second
	}
	scheduler := &Scheduler{
		Now:                   now,
		store:                 store,
		pool:                  pool,
		dispatch:              cfg.Dispatch,
		maxConcurrent:         maxC,
		tickInterval:          tick,
		hbInterval:            defaultHeartbeatInterval,
		readiness:             cfg.Readiness,
		disabled:              cfg.Disabled,
		reschedulesOnRecovery: cfg.ReschedulesOnRecovery,
	}
	if cfg.Readiness != nil {
		cfg.Readiness.SetSchedulerEnabled(!cfg.Disabled)
	}
	return scheduler
}

// envInt reads a non-negative integer env var, falling back to def on unset or
// unparseable (a malformed scheduler knob is a misconfig, not a crash — the default
// is always a safe value).
func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// Start runs the boot reconciliation (orphan scan + missed catch-up) then the tick
// loop until ctx is cancelled. It is graceful: on cancel it stops scheduling new
// ticks, lets the in-flight tick finish (its claims drain their semaphore), and
// returns nil. The loop owns no leaked goroutines — every claim's heartbeat is
// joined before the tick returns (goleak gate).
func (s *Scheduler) Start(ctx context.Context) error {
	ctx, end := schedulerLifecycleBoundary.Start(ctx)
	var observeErr error
	defer end.PanicSafe(&observeErr)
	if s.disabled {
		<-ctx.Done()
		observeErr = ctx.Err()
		return nil
	}
	_ = s.recoverOrphans(ctx) // WARN-only, never blocks boot (D-02)
	missed, err := s.catchUpMissed(ctx)
	if err != nil {
		slog.Warn("scheduler boot catch-up failed", "err", err)
	}
	// Dispatch the collapsed missed fires (D-18): catchUpMissed advanced each task's
	// next_run_at to a FUTURE instant, so without firing them here the missed window
	// is silently dropped — the very next tick would not re-select them. runMissed
	// claims + heartbeats + dispatches each ONCE, carrying MissedSince, and does NOT
	// reschedule (catchUpMissed already did).
	for _, m := range missed {
		s.runMissed(ctx, m)
	}
	if err == nil && ctx.Err() == nil {
		s.markTick()
	}

	ticker := time.NewTicker(s.tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			observeErr = ctx.Err()
			return nil
		case <-ticker.C:
			if err := s.runScheduledScan(ctx); err != nil {
				slog.Warn("scheduler tick failed", "err", err)
			}
		}
	}
}

func (s *Scheduler) runScheduledScan(ctx context.Context) (err error) {
	ctx, end := schedulerScanBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	scan := s.tick
	if s.scan != nil {
		scan = s.scan
	}
	if err := scan(ctx); err != nil {
		return err
	}
	s.markTick()
	return nil
}

func (s *Scheduler) markTerminalFailure(err error) {
	if s.readiness != nil {
		s.readiness.MarkSchedulerFailure(err)
	}
}

// tick selects the due tasks (next_run_at<=Now, FOR UPDATE SKIP LOCKED) up to the
// concurrency cap, then claims+dispatches each under a semaphore bounded by
// maxConcurrent so held conns never exceed the cap (Pitfall 2). A task already
// in-flight (another worker holds its advisory lock) is skipped, logged
// `skipped: previous run in progress`, and rescheduled via NextRunAt (D-04). The
// tick blocks until all dispatched runs finish so Start's graceful shutdown joins
// them (no leaked goroutine).
func (s *Scheduler) tick(ctx context.Context) error {
	due, err := s.store.DueTasks(ctx, s.maxConcurrent)
	if err != nil {
		return fmt.Errorf("tick due tasks: %w", err)
	}

	sem := make(chan struct{}, s.maxConcurrent)
	var wg sync.WaitGroup
	for _, task := range due {
		wg.Add(1)
		sem <- struct{}{}
		go func(task Task) {
			defer wg.Done()
			defer func() { <-sem }()
			s.runOne(ctx, task)
		}(task)
	}
	wg.Wait()
	if sweeper, ok := s.dispatch.(notificationSweeper); ok {
		if err := sweeper.sweepNotifications(ctx); err != nil {
			return fmt.Errorf("tick sweep notifications: %w", err)
		}
	}
	return nil
}

func (s *Scheduler) markTick() {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	s.lastTickUnix.Store(now().UTC().UnixNano())
	if s.readiness != nil {
		s.readiness.MarkSchedulerProgress()
	}
}

// LastTick returns the scheduler tick timestamp most recently recorded by tick.
// A zero time means the process has started but no tick has run yet.
func (s *Scheduler) LastTick() time.Time {
	n := s.lastTickUnix.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n).UTC()
}

// runOne claims and dispatches a single due task. On a lost advisory lock it logs
// the D-04 skip and reschedules; on a win it heartbeats the run on the held conn,
// dispatches (10-05 handlers via the Dispatcher seam; nil = claim-and-release
// probe), then releases the lock + conn regardless of outcome.
func (s *Scheduler) runOne(ctx context.Context, task Task) {
	claim, err := s.claim(ctx, task)
	if err != nil {
		if errors.Is(err, ErrAlreadyRunning) {
			slog.Info("skipped: previous run in progress", "task", task.ID)
			s.reschedule(ctx, task)
			return
		}
		slog.Warn("scheduler claim failed", "task", task.ID, "err", err)
		return
	}
	defer claim.release(ctx)

	// Advance next_run_at to the next fire as soon as the claim is won, BEFORE the
	// dispatch runs. The held advisory lock makes the task a singleton for the run's
	// lifetime, but it does NOT take the task out of the due-set: until next_run_at is
	// pushed forward, every subsequent tick re-selects (and, once the lock releases,
	// re-fires) the same due task — a recurring reminder otherwise fires once per tick
	// instead of once per window (SC#1). A one-shot `at` task advances to a zero
	// next_run_at (retired). Reschedule on claim, not after dispatch, so a long/failed
	// run never re-fires mid-flight.
	s.reschedule(ctx, task)

	stop := startHeartbeat(ctx, claim.Conn(), claim.RunID, s.hbInterval)
	defer stop()

	if s.dispatch != nil {
		if err := s.dispatch.Dispatch(ctx, task, claim); err != nil {
			slog.Warn("scheduler dispatch failed", "task", task.ID, "run", claim.RunID, "err", err)
		}
	}
}

// runMissed dispatches a collapsed boot catch-up fire ONCE (D-18). It mirrors runOne
// — same held-conn advisory-lock claim + heartbeat + dispatch lifecycle — but with
// two deliberate differences: (a) it does NOT reschedule, because catchUpMissed
// already advanced next_run_at to the next future fire; and (b) it threads the
// task's MissedSince onto the claim (and stamps it on the run row) so the dispatcher
// and the backup handler can deliver a late-run notification and the SC#3 alert. A
// lost lock (another booting worker already caught it up) is a benign skip — the
// catch-up is at-most-once across the cluster by the same advisory-lock singleton.
func (s *Scheduler) runMissed(ctx context.Context, m MissedTask) {
	claim, err := s.claim(ctx, m.Task)
	if err != nil {
		if errors.Is(err, ErrAlreadyRunning) {
			slog.Info("skipped catch-up: previous run in progress", "task", m.Task.ID)
			return
		}
		slog.Warn("scheduler catch-up claim failed", "task", m.Task.ID, "err", err)
		return
	}
	defer claim.release(ctx)

	claim.MissedSince = m.MissedSince
	if err := s.store.setMissedSinceOnConn(ctx, claim.Conn(), claim.RunID, m.MissedSince); err != nil {
		slog.Warn("scheduler catch-up stamp missed_since failed", "task", m.Task.ID, "run", claim.RunID, "err", err)
	}

	stop := startHeartbeat(ctx, claim.Conn(), claim.RunID, s.hbInterval)
	defer stop()

	if s.dispatch != nil {
		if err := s.dispatch.Dispatch(ctx, m.Task, claim); err != nil {
			slog.Warn("scheduler catch-up dispatch failed", "task", m.Task.ID, "run", claim.RunID, "err", err)
		}
	}
}

// reschedule advances a skipped/overdue task's next_run_at to its next fire so the
// loop does not re-pick it every tick (D-04). A one-shot `at` task with no further
// fire goes to next_run_at zero (UpdateNextRunAt nulls it), retiring it.
func (s *Scheduler) reschedule(ctx context.Context, task Task) {
	next, err := NextRunAt(specFromTask(task), s.Now().UTC())
	if err != nil {
		slog.Warn("scheduler reschedule compute failed", "task", task.ID, "err", err)
		return
	}
	if err := s.store.UpdateNextRunAt(ctx, task.ID, next); err != nil {
		slog.Warn("scheduler reschedule failed", "task", task.ID, "err", err)
	}
}

// DuringQuietHours reports whether the injectable clock's wall time falls inside the
// AURA_SCHEDULER_QUIET_HOURS window (e.g. "23:00-07:30"), evaluated in the scheduler
// TZ. It is a Now-based predicate the Notifier (10-05) consults to DEFER
// non-destructive notifications (D-23) — it never gates firing here. An unset or
// malformed window is "never quiet" (fail-open: notifications are not silently lost).
func (s *Scheduler) DuringQuietHours(tz string) bool {
	_, ok := s.QuietHoursEnd(tz)
	return ok
}

// QuietHoursEnd returns the end instant of the current quiet-hours window. ok=false
// when quiet hours are unset/malformed or the scheduler clock is outside the window.
func (s *Scheduler) QuietHoursEnd(tz string) (time.Time, bool) {
	window := os.Getenv("AURA_SCHEDULER_QUIET_HOURS")
	if window == "" {
		return time.Time{}, false
	}
	start, end, ok := parseQuietWindow(window)
	if !ok {
		return time.Time{}, false
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	now := s.Now().In(loc)
	nowM := minuteOfDay(now)
	endTime := func(dayOffset int) time.Time {
		return time.Date(now.Year(), now.Month(), now.Day(), end/60, end%60, 0, 0, loc).
			AddDate(0, 0, dayOffset).
			UTC()
	}
	if start <= end {
		if nowM >= start && nowM < end {
			return endTime(0), true
		}
		return time.Time{}, false
	}
	// Wrap-around window (e.g. 23:00-07:30): quiet if before end OR at/after start.
	switch {
	case nowM >= start:
		return endTime(1), true
	case nowM < end:
		return endTime(0), true
	default:
		return time.Time{}, false
	}
}

// parseQuietWindow parses "HH:MM-HH:MM" to start/end minute-of-day. ok=false on any
// malformed input (the caller treats that as never-quiet).
func parseQuietWindow(w string) (start, end int, ok bool) {
	lo, hi, found := strings.Cut(w, "-")
	if !found {
		return 0, 0, false
	}
	s, ok1 := parseHHMM(lo)
	e, ok2 := parseHHMM(hi)
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	return s, e, true
}

// parseHHMM parses "HH:MM" to minute-of-day in [0,1440).
func parseHHMM(s string) (int, bool) {
	hh, mm, found := strings.Cut(strings.TrimSpace(s), ":")
	if !found {
		return 0, false
	}
	h, err1 := strconv.Atoi(hh)
	m, err2 := strconv.Atoi(mm)
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

// minuteOfDay returns t's wall-clock minute-of-day in its location.
func minuteOfDay(t time.Time) int { return t.Hour()*60 + t.Minute() }
