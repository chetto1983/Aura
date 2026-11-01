package cron

// store_manage.go holds the cockpit scheduler-management reads/writes (GOV-03 write):
// the active+pending board list and the approve / run-now / reschedule verbs the
// governance API exposes. They are thin sqlc wrappers (the :execrows queries return
// rows-affected so a miss maps to ErrTaskNotFound). The board read leaves system sweeps
// out; the write guard lives in the handler (IsUserManageableKind, IsCancellableKind) —
// a store method mutates by id + status only.

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// IsUserManageableKind reports whether a task kind is operator-managed (reminder,
// agent_job, the two backups, the mail watch) rather than one of the system-seeded sweeps.
// The cockpit write handlers refuse to approve/run/edit/cancel a system sweep: those are
// seeded on a fixed cadence and must never be operator-mutated.
func IsUserManageableKind(k TaskKind) bool {
	switch k {
	case KindReminder, KindAgentJob, KindBackupPostgres, KindBackupObjects, KindPIMWatch, KindPIMDigest:
		return true
	default:
		return false
	}
}

// IsCancellableKind reports whether an operator or the agent may cancel a task of this
// kind: an operator-managed one other than a backup. The operator decided on 2026-10-05 that
// the database backup must never be stopped from the cockpit or the agent, and the object
// backup is its sibling.
func IsCancellableKind(k TaskKind) bool {
	return IsUserManageableKind(k) && k != KindBackupPostgres && k != KindBackupObjects
}

// ListLiveTasksOfKind returns one identity's active and paused tasks of a kind, the ones a
// repeated enable must find instead of creating a second.
func (s *Store) ListLiveTasksOfKind(ctx context.Context, identityID string, kind TaskKind) ([]Task, error) {
	rows, err := s.q.ListLiveTasksOfKind(ctx, sqlc.ListLiveTasksOfKindParams{IdentityID: identityID, Kind: string(kind)})
	if err != nil {
		return nil, fmt.Errorf("list live %s tasks: %w", kind, err)
	}
	out := make([]Task, 0, len(rows))
	for _, r := range rows {
		out = append(out, taskFromRow(r))
	}
	return out, nil
}

// DeleteIdentityTasks removes every task of a purged identity and, by ON DELETE CASCADE, their
// runs and those runs' notifications. Deleting an identity's row does not reach them:
// scheduler_tasks.identity_id is text with no foreign key.
func (s *Store) DeleteIdentityTasks(ctx context.Context, identityID string) error {
	if err := s.q.DeleteIdentityTaskRows(ctx, identityID); err != nil {
		return fmt.Errorf("delete tasks of identity %q: %w", identityID, err)
	}
	return nil
}

// ListManageableTasks returns the active + pending_approval tasks of operator-managed kinds
// (the cockpit board), so a gated task can be approved on-screen alongside the running ones.
// System sweeps are left out: the board has nothing to offer for them.
func (s *Store) ListManageableTasks(ctx context.Context) ([]Task, error) {
	rows, err := s.q.ListManageableTasks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list manageable tasks: %w", err)
	}
	out := make([]Task, 0, len(rows))
	for _, r := range rows {
		if task := taskFromRow(r); IsUserManageableKind(task.Kind) {
			out = append(out, task)
		}
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

// SetTaskOrigin records the conversation a task answers in from now on (task_home.go). An
// absent task is ErrTaskNotFound.
func (s *Store) SetTaskOrigin(ctx context.Context, id, conversationID string) error {
	u, err := db.ParseUUID("uuid", id)
	if err != nil {
		return fmt.Errorf("set task origin: %w", err)
	}
	conv, err := db.ParseUUID("origin_conversation_id", conversationID)
	if err != nil {
		return fmt.Errorf("set task origin: %w", err)
	}
	n, err := s.q.SetTaskOriginRow(ctx, sqlc.SetTaskOriginRowParams{ID: u, OriginConversationID: conv})
	if err != nil {
		return fmt.Errorf("set task origin %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("set task origin %q: %w", id, ErrTaskNotFound)
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
	if !ValidNotifyRoute(p.NotifyRoute) {
		return fmt.Errorf("update task: notify route %q is not explicit", p.NotifyRoute)
	}
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
		NotifyRoute:  p.NotifyRoute,
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
