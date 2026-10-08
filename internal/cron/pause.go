package cron

// pause.go holds the scheduler's pause policy (prd.md §15): who may stop a task, how a
// paused task comes back, and the auto-pause that stops a task after repeated failed runs
// instead of letting it fail and notify on every occurrence.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// Who paused a task, stored in scheduler_tasks.paused_reason (migration 0138).
const (
	PausedByOperator = "operator"
	PausedByFailures = "failures"
)

const defaultPauseAfterFailures = 3

// ErrResumePastOneShot is a paused one-shot whose fire time has passed: resuming cannot give
// it a next fire, so it has to be edited to a new time instead.
var ErrResumePastOneShot = errors.New("its one-shot time has passed; edit it to a new time instead of resuming it")

// IsPausableKind reports whether a task of this kind may be paused, by the operator or by
// the scheduler. It is the cancel rule: a task the operator may not stop may not be paused.
func IsPausableKind(k TaskKind) bool {
	return IsCancellableKind(k)
}

// pauseAfterFailures resolves AURA_SCHEDULER_PAUSE_AFTER_FAILURES. Like the approval reminder
// interval, a valid value <= 0 disables auto-pause (returns 0); empty or invalid is the default.
func pauseAfterFailures() int {
	v := os.Getenv("AURA_SCHEDULER_PAUSE_AFTER_FAILURES")
	if v == "" {
		return defaultPauseAfterFailures
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultPauseAfterFailures
	}
	return max(n, 0)
}

// PauseTask stops an active task from firing until it is resumed. A task that is not active
// (awaiting approval, already paused, cancelled or absent) is ErrTaskNotFound.
func (s *Store) PauseTask(ctx context.Context, id string) error {
	u, err := db.ParseUUID("uuid", id)
	if err != nil {
		return fmt.Errorf("pause task: %w", err)
	}
	n, err := s.q.PauseTaskRow(ctx, u)
	if err != nil {
		return fmt.Errorf("pause task %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("pause task %q: %w", id, ErrTaskNotFound)
	}
	return nil
}

// ResumeTask reactivates a paused task and returns its next fire, computed from now so the
// windows it missed while paused are not replayed. A task that is not paused is
// ErrTaskNotFound; a one-shot whose time has passed is ErrResumePastOneShot.
func (s *Store) ResumeTask(ctx context.Context, id string, now time.Time) (time.Time, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return time.Time{}, fmt.Errorf("resume task: %w", err)
	}
	if task.Status != "paused" {
		return time.Time{}, fmt.Errorf("resume task %q: %w", id, ErrTaskNotFound)
	}
	next, err := ResumeFire(task, now)
	if err != nil {
		return time.Time{}, fmt.Errorf("resume task %q: %w", id, err)
	}
	u, err := db.ParseUUID("uuid", id)
	if err != nil {
		return time.Time{}, fmt.Errorf("resume task: %w", err)
	}
	n, err := s.q.ResumeTaskRow(ctx, sqlc.ResumeTaskRowParams{ID: u, NextRunAt: tsOrNull(next)})
	if err != nil {
		return time.Time{}, fmt.Errorf("resume task %q: %w", id, err)
	}
	if n == 0 {
		return time.Time{}, fmt.Errorf("resume task %q: %w", id, ErrTaskNotFound)
	}
	return next, nil
}

// ResumeFire is the next fire of a resumed task: its own schedule evaluated from now.
func ResumeFire(task Task, now time.Time) (time.Time, error) {
	spec, err := ParseSchedule(string(task.ScheduleKind), task.CronExpr, task.EveryMinutes, task.RunAt, task.TZ)
	if err != nil {
		return time.Time{}, err
	}
	next, err := FirstFire(spec, now)
	if errors.Is(err, ErrPastRunAt) {
		return time.Time{}, ErrResumePastOneShot
	}
	return next, err
}

// RunOutcome is a finished run's effect on its task.
type RunOutcome struct {
	ConsecutiveFailures int
	// PausedNow is true when this run's failure is the one that paused the task: a task the
	// operator paused mid-run, or one already paused by an earlier failure, is not.
	PausedNow bool
}

// RecordRunOutcome resets the task's failure count on success, or increments it on failure
// and pauses the task when the count reaches pauseAfter (0 never pauses).
func (s *Store) RecordRunOutcome(ctx context.Context, taskID string, succeeded bool, pauseAfter int) (RunOutcome, error) {
	u, err := db.ParseUUID("uuid", taskID)
	if err != nil {
		return RunOutcome{}, fmt.Errorf("record run outcome: %w", err)
	}
	row, err := s.q.RecordTaskRunOutcome(ctx, sqlc.RecordTaskRunOutcomeParams{
		Succeeded:  succeeded,
		PauseAfter: int32(min(pauseAfter, 1<<30)), //nolint:gosec // G115: clamped above to fit int32.
		ID:         u,
	})
	if err != nil {
		return RunOutcome{}, fmt.Errorf("record run outcome %q: %w", taskID, err)
	}
	failures := int(row.ConsecutiveFailures)
	return RunOutcome{
		ConsecutiveFailures: failures,
		PausedNow:           row.PausedReason.String == PausedByFailures && failures == pauseAfter,
	}, nil
}
