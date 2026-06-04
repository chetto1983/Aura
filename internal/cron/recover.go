package cron

// recover.go is the boot reconciliation (D-02/D-18): the orphan scan that marks
// runs whose heartbeat lapsed (>90s) as unknown_recovery, and the missed catch-up
// that collapses multiple skipped windows of an overdue task into a SINGLE
// catch-up fire carrying MissedSince. Both run once at Start (scheduler.go) before
// the first tick. A scan failure is a WARN-level degradation, never a boot blocker
// (mirroring conversations.ScanOrphans, cmd/aura/chat.go).

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// recoverOrphans marks every running row with a stale heartbeat (>90s) as
// unknown_recovery — the repudiation audit trail for a worker that died mid-flight
// (D-02). It complements, and does not replace, any future PRD MaxDuration boot
// query: the heartbeat-staleness scan is the liveness-based half. A scan or mark
// failure is logged WARN and does NOT abort boot (a degraded recovery still lets the
// daemon serve), so the returned error is ALWAYS nil — Start treats a degraded
// recovery as non-fatal (it calls this for its side effect, discarding the nil). The
// error return is kept only to leave room for a future hard-fail policy without a
// signature change; today there is no failure the caller acts on.
func (s *Scheduler) recoverOrphans(ctx context.Context) error {
	stale, err := s.store.ScanStaleRuns(ctx, staleRecoverySeconds)
	if err != nil {
		slog.Warn("scheduler boot orphan scan failed", "err", err)
		return nil
	}
	for _, r := range stale {
		if err := s.store.MarkUnknownRecovery(ctx, r.RunID); err != nil {
			slog.Warn("scheduler mark unknown_recovery failed", "run", r.RunID, "task", r.TaskID, "err", err)
		}
	}
	return nil
}

// MissedTask is an overdue task collapsed for a single catch-up fire (D-18):
// MissedSince is the ORIGINAL fire the task first slipped, carried into the one
// catch-up run so a notification can say "late summary, scheduled 9:30".
type MissedTask struct {
	Task        Task
	MissedSince time.Time
}

// catchUpMissed collapses every overdue recurring task to ONE catch-up fire (D-18).
// For each active task whose next_run_at is in the past, it computes the next FUTURE
// fire (skipping the intervening missed windows so the task fires once, not N times)
// and advances next_run_at to it; the task's original overdue next_run_at becomes
// MissedSince. The returned MissedTasks feed the dispatcher (10-05), which fires each
// once. A one-shot `at` task that is overdue is returned with no forward fire (its
// next_run_at goes zero) so it fires exactly once and never again.
func (s *Scheduler) catchUpMissed(ctx context.Context) ([]MissedTask, error) {
	now := s.Now().UTC()
	tasks, err := s.store.ListActiveTasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("catch-up list active tasks: %w", err)
	}

	var missed []MissedTask
	for _, task := range tasks {
		if task.NextRunAt.IsZero() || !task.NextRunAt.Before(now) {
			continue // not overdue (or never scheduled)
		}
		spec := specFromTask(task)
		// Collapse: the next fire STRICTLY after now skips every intervening missed
		// window, so the task fires once at catch-up then resumes its cadence.
		next, err := NextRunAt(spec, now)
		if err != nil {
			slog.Warn("scheduler catch-up next-fire compute failed", "task", task.ID, "err", err)
			continue
		}
		if err := s.store.UpdateNextRunAt(ctx, task.ID, next); err != nil {
			slog.Warn("scheduler catch-up advance failed", "task", task.ID, "err", err)
			continue
		}
		missed = append(missed, MissedTask{Task: task, MissedSince: task.NextRunAt.UTC()})
	}
	return missed, nil
}

// specFromTask rebuilds the ScheduleSpec from a persisted Task so NextRunAt can
// recompute the next fire in-zone (D-07). The grammar fields are mutually exclusive
// per ScheduleKind, matching ParseSchedule's projection.
func specFromTask(t Task) ScheduleSpec {
	return ScheduleSpec{
		Kind:         t.ScheduleKind,
		CronExpr:     t.CronExpr,
		EveryMinutes: t.EveryMinutes,
		RunAt:        t.RunAt,
		TZ:           t.TZ,
	}
}
