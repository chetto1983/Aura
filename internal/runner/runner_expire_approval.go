package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/approvaltext"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

// acceptedApprovalReader is the read that tells an approval in flight from one never answered;
// *askuser.Store satisfies it.
type acceptedApprovalReader interface {
	HasAcceptedTaskApprovalForIdentity(ctx context.Context, conversationID, identityID, taskID string) (bool, error)
}

// ExpireApprovalPause closes the pending approval of a task that will not run (its time passed
// unapproved, or it was cancelled) with an answer the model reads, in the conversation the task
// was scheduled from. A message review also has its draft expired. The resume hook never runs:
// the task is being cancelled, not approved.
//
// An approval the operator already accepted is left to the hook (PauseAnswered), and one resolved
// while the thread lock was awaited commits nothing (decision 36 of
// docs/superpowers/plans/2026-11-05-scheduled-delivery-defaults-and-messages.md).
func (r *Runner) ExpireApprovalPause(ctx context.Context, conversationID, taskID, answer string) (cron.PauseClosed, error) {
	if r.pause == nil || r.resumeCommitter == nil {
		return cron.PauseNone, nil
	}
	pending, found, err := r.taskApprovalPause(ctx, conversationID, taskID)
	if err != nil || !found {
		return r.unpausedApproval(ctx, conversationID, taskID, err)
	}
	if !threadLockHeld(ctx) {
		unlock, ok := r.TryLockThread(ctx, conversationID)
		if !ok {
			return cron.PauseNone, ErrThreadBusy
		}
		defer unlock()
	}
	pending, found, err = r.taskApprovalPause(ctx, conversationID, taskID)
	if err != nil || !found {
		return r.unpausedApproval(ctx, conversationID, taskID, err)
	}
	if draftID, review := messagedrafts.PauseDraftID(pending.ResumeContext); review && r.messageDrafts != nil {
		if _, err := r.messageDrafts.ExpireScheduled(ctx, identityctx.IdentityID(ctx), draftID); err != nil && !errors.Is(err, messagedrafts.ErrUnavailable) {
			return cron.PauseNone, fmt.Errorf("expire the scheduled message's draft: %w", err)
		}
	}
	claim := r.resumeClaim(pending.Token, pending, ResponseInput{Action: askuser.ActionDecline, Content: answer})
	claim.Turn.Content = answer
	claim.Answer.Content = answer
	if err := r.resumeCommitter.CommitResume(ctx, claim); err != nil {
		return cron.PauseNone, fmt.Errorf("close the approval of task %s: %w", taskID, err)
	}
	return cron.PauseClosedNow, nil
}

// taskApprovalPause finds the task's pending approval in the conversation: a scheduled-task
// approval naming it, or the review of its message.
func (r *Runner) taskApprovalPause(ctx context.Context, conversationID, taskID string) (askuser.Pending, bool, error) {
	pendings, err := r.pause.ListPending(ctx, conversationID)
	if err != nil {
		return askuser.Pending{}, false, err
	}
	for _, p := range pendings {
		var fields map[string]json.RawMessage
		if json.Unmarshal(p.ResumeContext, &fields) == nil &&
			approvaltext.RootString(fields, "type") == "scheduled_task_approval" && approvaltext.RootString(fields, "task_id") == taskID {
			return p, true, nil
		}
		if id, ok := messagedrafts.PauseTaskID(p.ResumeContext); ok && id == taskID {
			return p, true, nil
		}
	}
	return askuser.Pending{}, false, nil
}

// unpausedApproval is the answer when the task has no pending approval: answered (accepted, the
// hook activates it) or none at all.
func (r *Runner) unpausedApproval(ctx context.Context, conversationID, taskID string, err error) (cron.PauseClosed, error) {
	if err != nil {
		return cron.PauseNone, err
	}
	reader, ok := r.pause.(acceptedApprovalReader)
	if !ok {
		return cron.PauseNone, nil
	}
	accepted, err := reader.HasAcceptedTaskApprovalForIdentity(ctx, conversationID, identityctx.IdentityID(ctx), taskID)
	if err != nil {
		return cron.PauseNone, err
	}
	if accepted {
		return cron.PauseAnswered, nil
	}
	return cron.PauseNone, nil
}
