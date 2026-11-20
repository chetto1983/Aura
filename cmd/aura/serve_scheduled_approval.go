package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/messagedrafts"
	"github.com/chetto1983/aura/internal/runner"
)

// Compile-time proof the composition-root ensurer satisfies the cron approval-sweep seam, and
// that its live collaborators satisfy the narrow ensurer interfaces (*askuser.Store lists pending;
// *runner.Runner mints). These keep the serve_dispatch wiring honest without a full-boot test.
var (
	_ cron.ApprovalPauseEnsurer = approvalPauseEnsurer{}
	_ cron.LapsedPauseCloser    = lapsedPauseCloser{}
	_ cron.LapsedMessages       = lapsedMessages{}
	_ approvalPauseExpirer      = (*runner.Runner)(nil)
	_ approvalPauseLister       = (*askuser.Store)(nil)
	_ approvalMinter            = (*runner.Runner)(nil)
)

// approvalMinter is the narrow pause-minting seam the ensurer drives when no matching pause
// exists. *runner.Runner satisfies it (MintApprovalPause); unit tests inject a fake.
type approvalMinter interface {
	MintApprovalPause(ctx context.Context, convID, question string, resumeContext json.RawMessage, allowedDecisions []string) (string, error)
}

// approvalPauseLister is the narrow pending-read seam the ensurer dedups against.
// *askuser.Store satisfies it (ListPending).
type approvalPauseLister interface {
	ListPending(ctx context.Context, conversationID string) ([]askuser.Pending, error)
}

// approvalPauseEnsurer satisfies cron.ApprovalPauseEnsurer: it reuses a still-pending
// scheduled_task_approval pause on the task's origin conversation (dedup — so the per-cadence
// sweep never mints a duplicate, and converges with a model-relayed pause), else mints a
// wire-valid one via Runner.MintApprovalPause. This makes the approval-reminder sweep
// deterministic (Amendment #92 revised): within one cadence every pending_approval task with an
// origin conversation has a real HITL pause on it regardless of whether the model ever relayed.
type approvalPauseEnsurer struct {
	pauses approvalPauseLister
	minter approvalMinter
}

// EnsureApprovalPause returns the token of the existing (or freshly minted) scheduled-task
// approval pause on conversationID for taskID, and whether it MINTED a fresh pause (true) or
// REUSED a model-relayed one (false). The sweep pushes to the channel only when it minted — a
// reused pause is already on the origin channel, so a second push would duplicate it. kind is the
// scheduler task kind (e.g. agent_job) used only for the bounded, secret-safe prompt text.
func (e approvalPauseEnsurer) EnsureApprovalPause(ctx context.Context, taskID, conversationID, kind string) (string, bool, error) {
	pending, err := e.pauses.ListPending(ctx, conversationID)
	if err != nil {
		return "", false, fmt.Errorf("ensure approval pause: list pending: %w", err)
	}
	if kind == string(cron.KindMessage) {
		return messageReviewPause(pending, taskID)
	}
	for _, p := range pending {
		if p.Kind != tools.KindApproval || len(p.ResumeContext) == 0 {
			continue
		}
		var rc struct {
			Type   string `json:"type"`
			TaskID string `json:"task_id"`
		}
		if json.Unmarshal(p.ResumeContext, &rc) == nil && rc.Type == "scheduled_task_approval" && rc.TaskID == taskID {
			return p.Token, false, nil // reuse — no duplicate across cadences / with the model relay
		}
	}
	rc, err := json.Marshal(map[string]string{"type": "scheduled_task_approval", "task_id": taskID})
	if err != nil {
		return "", false, fmt.Errorf("ensure approval pause: marshal resume context: %w", err)
	}
	question := tools.ApprovalReminderQuestion(kind, taskID)
	token, err := e.minter.MintApprovalPause(ctx, conversationID, question, rc, []string{
		askuser.ActionAccept,
		askuser.ActionDecline,
		askuser.ActionCancel,
	})
	return token, true, err
}

// errMessageReviewMissing is a message task with no review pause on its conversation: a bare
// Si/No would approve text the operator never read, so the sweep mints nothing for it.
var errMessageReviewMissing = errors.New("the scheduled message has no review pause; it is approved only from its message card")

// messageReviewPause is the review pause a message task's draft is waiting in. The sweep never
// mints a scheduled_task_approval for a message (decision 12); minted is always false, so it
// pushes nothing either: the review card already went out in the turn that scheduled it.
func messageReviewPause(pending []askuser.Pending, taskID string) (string, bool, error) {
	for _, p := range pending {
		if id, ok := messagedrafts.PauseTaskID(p.ResumeContext); ok && id == taskID {
			return p.Token, false, nil
		}
	}
	return "", false, errMessageReviewMissing
}

// shortTaskID renders the first 8 chars of a task id for a bounded, secret-safe prompt (the
// approval nudge carries kind + short id only, never the payload).
func shortTaskID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// approvalPauseExpirer closes a task's pending approval with an answer the model reads;
// *runner.Runner satisfies it.
type approvalPauseExpirer interface {
	ExpireApprovalPause(ctx context.Context, conversationID, taskID, answer string) (cron.PauseClosed, error)
}

// lapsedPauseCloser closes a lapsed task's approval in the conversation it was asked in. A task
// with no origin conversation (a CLI row) has no pause to close.
type lapsedPauseCloser struct{ runner approvalPauseExpirer }

func (c lapsedPauseCloser) CloseLapsedApproval(ctx context.Context, task cron.Task, answer string) (cron.PauseClosed, error) {
	if task.OriginConversationID == "" || c.runner == nil {
		return cron.PauseNone, nil
	}
	return c.runner.ExpireApprovalPause(ctx, task.OriginConversationID, task.ID, answer)
}

// lapsedMessages settles a lapsed message task's draft over the drafts and the task store.
type lapsedMessages struct {
	drafts *messagedrafts.Store
	tasks  *cron.Store
}

func (m lapsedMessages) ApprovedForTask(ctx context.Context, ownerID, taskID string) (bool, error) {
	return m.drafts.ApprovedForTask(ctx, ownerID, taskID)
}

func (m lapsedMessages) ActivateReviewedMessage(ctx context.Context, ownerID, taskID string) error {
	return m.tasks.ActivateReviewedMessage(ctx, ownerID, taskID)
}

func (m lapsedMessages) ExpireForTask(ctx context.Context, ownerID, taskID string) error {
	return m.drafts.ExpireForTask(ctx, ownerID, taskID)
}

// cancelledApprovalAnswer closes the approval of a task the operator cancelled while it waited.
func cancelledApprovalAnswer(kind, taskID string) string {
	return fmt.Sprintf("Scheduled %s task %s was cancelled by the operator; it will not run.", kind, shortTaskID(taskID))
}
