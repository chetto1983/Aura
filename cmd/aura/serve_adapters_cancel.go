package main

// serve_adapters_cancel.go adapts the task tool's cancel to Postgres. The tool reads the task
// first (CancelTarget) to tell the conversation's own task from another one, which needs the
// operator's approval (docs/superpowers/specs/2026-11-26-task-cancel-foreign-approval-design.md);
// the read and the cancel share one query, so the tool never asks about a task the cancel would
// refuse.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/jackc/pgx/v5"
)

// CancelTarget reads the task a cancel would stop.
func (s *cronTaskStore) CancelTarget(ctx context.Context, id string) (tools.ScheduledTask, error) {
	_, task, err := s.cancelTarget(ctx, id)
	return task, err
}

// CancelScheduledTask soft-cancels only inside the identity carried by the tool call, and
// only a kind the agent may stop: never a system sweep or the database backup, which
// enrolment can hand to the operator's identity, and never the mail watch.
func (s *cronTaskStore) CancelScheduledTask(ctx context.Context, id string) error {
	identityID, task, err := s.cancelTarget(ctx, id)
	if err != nil {
		return err
	}
	if task.Kind == string(cron.KindMessage) {
		if err := s.drafts.DeclineForTask(ctx, identityID, id); err != nil {
			return fmt.Errorf("cancel task %s: decline its message: %w", id, err)
		}
	}
	if task.Status == "pending_approval" && task.OriginConversationID != "" && s.pauses != nil {
		if _, err := s.pauses.ExpireApprovalPause(ctx, task.OriginConversationID, id, cancelledApprovalAnswer(task.Kind, id)); err != nil {
			return fmt.Errorf("cancel task %s: close its approval: %w", id, err)
		}
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE aura.scheduler_tasks
		SET status = 'cancelled', paused_reason = NULL, updated_at = now()
		WHERE id = $1::uuid AND identity_id = $2`, id, identityID); err != nil {
		return fmt.Errorf("cancel task %s: %w", id, err)
	}
	return nil
}

// cancelTarget reads a live task of the calling identity and refuses a kind the agent may not
// cancel. It returns the identity the cancel writes under.
func (s *cronTaskStore) cancelTarget(ctx context.Context, id string) (string, tools.ScheduledTask, error) {
	identityID, err := taskIdentity(ctx)
	if err != nil {
		return "", tools.ScheduledTask{}, err
	}
	var task tools.ScheduledTask
	var next *time.Time
	var payload []byte
	err = s.pool.QueryRow(ctx, `
		SELECT id::text, kind, schedule_kind, status, next_run_at, payload, notify_route,
		       COALESCE(origin_conversation_id::text, '')
		FROM aura.scheduler_tasks
		WHERE id = $1::uuid
		  AND identity_id = $2
		  AND status IN ('active', 'pending_approval', 'paused')`, id, identityID).Scan(
		&task.ID, &task.Kind, &task.ScheduleKind, &task.Status, &next, &payload, &task.NotifyRoute, &task.OriginConversationID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", tools.ScheduledTask{}, fmt.Errorf("task %s is not active, paused or awaiting approval, or not owned by this identity", id)
	}
	if err != nil {
		return "", tools.ScheduledTask{}, fmt.Errorf("cancel task %s: %w", id, err)
	}
	if !cron.IsCancellableKind(cron.TaskKind(task.Kind)) {
		return "", tools.ScheduledTask{}, fmt.Errorf("task %s is a %s task, which cannot be cancelled", id, task.Kind)
	}
	if !cron.IsAgentStoppableKind(cron.TaskKind(task.Kind)) {
		return "", tools.ScheduledTask{}, watchStopRefused(id)
	}
	if next != nil {
		task.NextRunAt = *next
	}
	task.Payload = string(payload)
	return identityID, task, nil
}

// newTaskCancelResumeHook records an accept of a relayed task cancel challenge into the task
// tool's own store. Like the shell's hook it checks no capability: the cancel runs inside the
// calling identity, and the store refuses a task that identity does not own.
func newTaskCancelResumeHook(task *tools.TaskTool) runner.ResumeHook {
	if task == nil || task.CancelApprovals == nil {
		return nil
	}
	return newChallengeResumeHook(task.CancelApprovals.ApproveChallenge, task.CancelApprovals.DiscardChallenge,
		tools.TaskCancelApprovalContext, "approval_sha256", nil)
}
