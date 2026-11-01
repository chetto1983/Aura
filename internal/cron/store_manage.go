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
	"slices"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// personKinds is what a person schedules and may stop. It is also all an identity's deactivation
// or purge may touch: enrolment hands the deployment's sweeps to the operator's identity
// (retireLegacyLocalIdentity), so owning a task does not make it that person's. The queries take
// this list as a parameter, so Go and SQL cannot name two different sets.
var personKinds = []TaskKind{KindReminder, KindAgentJob, KindPIMWatch, KindPIMDigest}

func identityTaskKinds() []string {
	kinds := make([]string, 0, len(personKinds))
	for _, kind := range personKinds {
		kinds = append(kinds, string(kind))
	}
	return kinds
}

// IsUserManageableKind reports whether a task kind is operator-managed (reminder,
// agent_job, the two backups, the mail watch) rather than one of the system-seeded sweeps.
// The cockpit write handlers refuse to approve/run/edit/cancel a system sweep: those are
// seeded on a fixed cadence and must never be operator-mutated.
func IsUserManageableKind(k TaskKind) bool {
	return IsCancellableKind(k) || k == KindBackupPostgres || k == KindBackupObjects
}

// IsCancellableKind reports whether the operator may cancel a task of this kind: an
// operator-managed one other than a backup. The operator decided on 2026-10-05 that the
// database backup must never be stopped from the cockpit or the agent, and the object backup
// is its sibling. The agent answers to IsAgentStoppableKind.
func IsCancellableKind(k TaskKind) bool {
	return slices.Contains(personKinds, k)
}

// IsAgentStoppableKind reports whether the agent's task tool may cancel or pause a task of this
// kind: a cancellable one other than the watch. A job that read a hostile mail can call that tool
// at Normal tier, and a silenced watch is the failure the spec names first (§4), so the model's
// one way to stop a watch is mail_watch disable, which asks the person.
func IsAgentStoppableKind(k TaskKind) bool {
	return IsCancellableKind(k) && k != KindPIMWatch && k != KindPIMDigest
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

// DeleteIdentityTasks removes what a purged identity scheduled (personKinds) and, by ON DELETE
// CASCADE, their runs and those runs' notifications. Deleting an identity's row does not reach
// them: scheduler_tasks.identity_id is text with no foreign key. A system task stays and keeps
// firing, owned by an id that no longer exists: nothing on its path reads the identity's row.
func (s *Store) DeleteIdentityTasks(ctx context.Context, identityID string) error {
	err := s.q.DeleteIdentityTaskRows(ctx, sqlc.DeleteIdentityTaskRowsParams{IdentityID: identityID, Kinds: identityTaskKinds()})
	if err != nil {
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
// via ParseSchedule + FirstFire), the recomputed first fire, and the payload/notify. An empty
// OriginConversationID or TelegramRecipients keeps the task's own.
type UpdateTaskParams struct {
	Spec                 ScheduleSpec
	NextRunAt            time.Time
	Payload              []byte
	NotifyRoute          string
	OriginConversationID string
	TelegramRecipients   []int64
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
		ID:                   u,
		ScheduleKind:         string(p.Spec.Kind),
		CronExpr:             text(p.Spec.CronExpr),
		EveryMinutes:         int4OrNull(p.Spec.EveryMinutes),
		RunAt:                tsOrNull(p.Spec.RunAt),
		Tz:                   p.Spec.TZ,
		Payload:              payloadOrEmpty(p.Payload),
		NotifyRoute:          p.NotifyRoute,
		NextRunAt:            tsOrNull(p.NextRunAt),
		OriginConversationID: uuidOrNull(p.OriginConversationID),
		TelegramRecipients:   p.TelegramRecipients,
	})
	if err != nil {
		return fmt.Errorf("update task %q: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("update task %q: %w", id, ErrTaskNotFound)
	}
	return nil
}
