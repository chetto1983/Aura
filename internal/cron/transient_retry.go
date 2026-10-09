package cron

// transient_retry.go fires a job again when its run failed on a transient model error before it
// did anything, instead of reporting the job lost (prd.md §15, decision of 2026-10-09: an
// internet outage lost a one-shot agent_job and its failure notice). The retry is a future
// fire, never a wait inside the run: a tick waits for its runs to finish, and /readyz reports
// scheduler_stalled after 90 s without progress.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
)

// transientRetryDelays is the wait before each retry of one failed fire, about 52 min in all.
var transientRetryDelays = []time.Duration{2 * time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute}

// TransientRetry is a re-armed fire: when it runs, and which retry of the chain it is (from 1).
type TransientRetry struct {
	At     time.Time
	Number int
}

// TransientRetryScheduler re-arms a task for a retry. *Store satisfies it.
type TransientRetryScheduler interface {
	ScheduleTransientRetry(ctx context.Context, taskID string, delays []time.Duration) (TransientRetry, bool, error)
}

var _ TransientRetryScheduler = (*Store)(nil)

// ScheduleTransientRetry moves the task's next fire to the earlier of its regular one and now
// plus the delay for this retry, and counts the retry. It reports false, with no error, when
// the task has used every delay or is no longer active.
func (s *Store) ScheduleTransientRetry(ctx context.Context, taskID string, delays []time.Duration) (TransientRetry, bool, error) {
	u, err := db.ParseUUID("uuid", taskID)
	if err != nil {
		return TransientRetry{}, false, fmt.Errorf("schedule transient retry: %w", err)
	}
	seconds := make([]int32, len(delays))
	for i, d := range delays {
		seconds[i] = positiveInt32(int(d/time.Second), 1)
	}
	row, err := s.q.ScheduleTransientRetry(ctx, sqlc.ScheduleTransientRetryParams{ID: u, DelaySeconds: seconds})
	if errors.Is(err, pgx.ErrNoRows) {
		return TransientRetry{}, false, nil
	}
	if err != nil {
		return TransientRetry{}, false, fmt.Errorf("schedule transient retry %q: %w", taskID, err)
	}
	return TransientRetry{At: row.NextRunAt.Time, Number: int(row.TransientRetries)}, true, nil
}

// retryableBeforeEffects reports whether the handler marked runErr as safe to run again: a
// transient failure before the job did anything. The handlers live in internal/cron/handlers,
// which cron cannot import, so the mark is a method rather than a sentinel.
func retryableBeforeEffects(err error) bool {
	var r interface{ RetryableBeforeEffects() bool }
	return errors.As(err, &r) && r.RetryableBeforeEffects()
}

// scheduleRetry re-arms the task when its handler marked runErr retryable, and reports whether
// it did. A retried failure stays on the run ledger but is not reported: the operator hears of
// the job once, when it succeeds or when the last retry fails.
func (d *Dispatch) scheduleRetry(ctx context.Context, task Task, runID string, runErr error) bool {
	if d.deps.RetryScheduler == nil || !retryableBeforeEffects(runErr) {
		return false
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), completeRunTimeout)
	defer cancel()
	retry, ok, err := d.deps.RetryScheduler.ScheduleTransientRetry(writeCtx, task.ID, transientRetryDelays)
	if err != nil {
		slog.Warn("dispatch schedule transient retry", "task", task.ID, "run", runID, "err", err)
		return false
	}
	if ok {
		slog.Info("scheduler: transient failure before any effect, retry scheduled",
			"task", task.ID, "run", runID, "retry", retry.Number, "retry_at", retry.At, "err", runErr)
	}
	return ok
}
