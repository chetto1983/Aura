package cron

// store_manage.go holds the cockpit scheduler-management reads/writes (GOV-03 write):
// the active+pending board list and the approve / run-now / reschedule verbs the
// governance API exposes. They are thin sqlc wrappers (the :execrows queries return
// rows-affected so a miss maps to ErrTaskNotFound); the system-kind guard lives in the
// handler (IsUserManageableKind), never here — a store method mutates by id + status only.

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// IsUserManageableKind reports whether a task kind is operator-managed (reminder,
// agent_job, backups) rather than a system-seeded sweep (identity_purge, sandbox_reap,
// skill_ttl_sweep, share_expiry_sweep). The cockpit write handlers refuse to
// approve/run/edit/cancel a system sweep: those are seeded on a fixed cadence and must
// never be operator-mutated.
func IsUserManageableKind(k TaskKind) bool {
	switch k {
	case KindReminder, KindAgentJob, KindBackupPostgres:
		return true
	default:
		return false
	}
}

// ListManageableTasks returns active + pending_approval tasks (the cockpit board), so a
// gated task can be approved on-screen alongside the running ones.
func (s *Store) ListManageableTasks(ctx context.Context) ([]Task, error) {
	rows, err := s.q.ListManageableTasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list manageable tasks: %w", err)
	}
	out := make([]Task, 0, len(rows))
	for _, r := range rows {
		out = append(out, taskFromRow(r))
	}
	return out, nil
}

// ApproveTask flips a pending_approval task to active. A task that is not awaiting
// approval (already active, cancelled, or absent) is ErrTaskNotFound.
func (s *Store) ApproveTask(ctx context.Context, id string) error {
	u, err := db.ParseUUID("uuid", id)
	if err != nil {
		return fmt.Errorf("approve task: %w", err)
	}
	n, err := s.q.ApproveTaskRow(ctx, u)
	if err != nil {
		return fmt.Errorf("approve task %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("approve task %q: %w", id, ErrTaskNotFound)
	}
	return nil
}

// RunTaskNow advances an active task's next fire to now so the next tick claims it. A
// non-active task (pending/cancelled/absent) is ErrTaskNotFound.
func (s *Store) RunTaskNow(ctx context.Context, id string) error {
	u, err := db.ParseUUID("uuid", id)
	if err != nil {
		return fmt.Errorf("run task now: %w", err)
	}
	n, err := s.q.RunTaskNowRow(ctx, u)
	if err != nil {
		return fmt.Errorf("run task now %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("run task now %q: %w", id, ErrTaskNotFound)
	}
	return nil
}

// UpdateTaskParams carries a reschedule edit: the new grammar (validated by the caller
// via ParseSchedule + FirstFire), the recomputed first fire, and the payload/notify.
type UpdateTaskParams struct {
	Spec        ScheduleSpec
	NextRunAt   time.Time
	Payload     []byte
	NotifyRoute string
}

// UpdateTask rewrites a task's schedule + payload + notify route (the cockpit edit). Only
// an active/pending row updates; a cancelled/completed/absent task is ErrTaskNotFound.
func (s *Store) UpdateTask(ctx context.Context, id string, p UpdateTaskParams) error {
	u, err := db.ParseUUID("uuid", id)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	n, err := s.q.UpdateTaskScheduleRow(ctx, sqlc.UpdateTaskScheduleRowParams{
		ID:           u,
		ScheduleKind: string(p.Spec.Kind),
		CronExpr:     text(p.Spec.CronExpr),
		EveryMinutes: int4OrNull(p.Spec.EveryMinutes),
		RunAt:        tsOrNull(p.Spec.RunAt),
		Tz:           p.Spec.TZ,
		Payload:      payloadOrEmpty(p.Payload),
		NotifyRoute:  text(p.NotifyRoute),
		NextRunAt:    tsOrNull(p.NextRunAt),
	})
	if err != nil {
		return fmt.Errorf("update task %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("update task %q: %w", id, ErrTaskNotFound)
	}
	return nil
}
