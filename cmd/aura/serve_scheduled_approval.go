package main

// serve_scheduled_approval.go is the composition-root wiring for the on-channel scheduled-task
// approval (Amendment #92 revised / fix-plan 1.7): the owner-scoped approve/cancel store
// transitions, the ResumeHook that resolves a scheduled_task_approval ask_user pause, and the
// approvalPauseEnsurer the reminder sweep drives to guarantee a real HITL pause exists on the
// task's origin conversation. Extracted from serve_adapters.go on touch (600-LOC cap).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

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

// ApproveScheduledTaskInConversation is the on-channel HITL approval transition: it
// flips a pending_approval task to active ONLY when the task's origin conversation
// matches the AUTHENTICATED conversation the approval pause was raised in. Keying on
// the server-stored origin (never a model-relayed id) is the WR-02 authorization: a
// model relaying a foreign task_id from another thread cannot approve it, and the task
// can only be approved from the very conversation that scheduled it.
func (s *cronTaskStore) ApproveScheduledTaskInConversation(ctx context.Context, taskID, conversationID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE aura.scheduler_tasks
		SET status = 'active', updated_at = now()
		WHERE id = $1 AND origin_conversation_id = $2 AND status = 'pending_approval' AND kind <> 'message'`, taskID, conversationID)
	if err != nil {
		return fmt.Errorf("approve scheduled task %s: %w", taskID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("scheduled task %s is not awaiting approval in conversation %s (a scheduled message is approved from its message card)", taskID, conversationID)
	}
	return nil
}

// CancelScheduledTaskInConversation is the on-channel HITL reject transition: it soft-cancels
// a pending_approval task ONLY when the task's origin conversation matches the AUTHENTICATED
// conversation the approval pause was raised in (the WR-02 owner-scoping, symmetric with
// ApproveScheduledTaskInConversation). It backs the "No" button / decline resolution so a
// declined scheduled task is dropped and stops re-nudging (Amendment #92 revised).
func (s *cronTaskStore) CancelScheduledTaskInConversation(ctx context.Context, taskID, conversationID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE aura.scheduler_tasks
		SET status = 'cancelled', updated_at = now()
		WHERE id = $1 AND origin_conversation_id = $2 AND status = 'pending_approval' AND kind <> 'message'`, taskID, conversationID)
	if err != nil {
		return fmt.Errorf("cancel scheduled task %s: %w", taskID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("scheduled task %s is not awaiting approval in conversation %s (a scheduled message is approved from its message card)", taskID, conversationID)
	}
	return nil
}

// ApprovalQuestions are the questions the host writes for a task awaiting approval in this
// conversation: the in-turn one naming what will run, and the sweep's bounded one. The hook
// accepts an approval only on one of them (decision 35).
func (s *cronTaskStore) ApprovalQuestions(ctx context.Context, taskID, conversationID string) ([]string, error) {
	var (
		kind, scheduleKind, tz string
		cronExpr               *string
		everyMinutes           *int32
		next                   time.Time
		payload                []byte
	)
	err := s.pool.QueryRow(ctx, `
		SELECT kind, schedule_kind, cron_expr, every_minutes, tz, next_run_at, payload
		FROM aura.scheduler_tasks
		WHERE id = $1 AND origin_conversation_id = $2 AND status = 'pending_approval'`, taskID, conversationID).
		Scan(&kind, &scheduleKind, &cronExpr, &everyMinutes, &tz, &next, &payload)
	if err != nil {
		return nil, fmt.Errorf("scheduled task %s: read its approval: %w", taskID, err)
	}
	expr, every := "", 0
	if cronExpr != nil {
		expr = *cronExpr
	}
	if everyMinutes != nil {
		every = int(*everyMinutes)
	}
	return []string{
		tools.ApprovalQuestion(kind, taskID, scheduleKind, expr, every, tz, next, payload),
		mintedApprovalQuestion(kind, taskID),
	}, nil
}

// scheduledTaskApprover is the owner-scoped approve/cancel seam the scheduled-task resume
// hook drives. *cronTaskStore satisfies it live; unit tests inject a fake.
type scheduledTaskApprover interface {
	ApproveScheduledTaskInConversation(ctx context.Context, taskID, conversationID string) error
	CancelScheduledTaskInConversation(ctx context.Context, taskID, conversationID string) error
	ApprovalQuestions(ctx context.Context, taskID, conversationID string) ([]string, error)
}

// errQuestionNotHostWritten is an approval whose question the host did not write: ask_user lets
// the model set a resume context, so an innocent question could otherwise approve a pending job.
var errQuestionNotHostWritten = errors.New("scheduled task approval: the question was not written by the host")

// newScheduledTaskResumeHook is the production ResumeHook that resolves a pending_approval
// scheduled task when the operator ACTS on its scheduled_task_approval ask_user pause — the
// on-channel HITL parity for the scheduler (the analog of newShellResumeHook/newGatewayResumeHook).
// The pause is created either in-turn by the task tool's host review (tools.ApprovalQuestion), or
// deterministically by the reminder sweep via approvalPauseEnsurer. An accept is honoured only on
// a question one of the two wrote (decision 35). It keys the transition on the
// AUTHENTICATED pending.ConversationID (server-stored at pause creation), never the model-relayed
// resume_context id, so an action surfaced in conv-B cannot touch a task scheduled from conv-A
// (WR-02). Accept → active; decline → cancelled. ActionCancel never reaches a ResumeHook, and a
// wrong-type / non-approval-kind resolves nothing (fail-closed).
func newScheduledTaskResumeHook(store scheduledTaskApprover) runner.ResumeHook {
	if store == nil {
		return nil
	}
	return func(ctx context.Context, pending askuser.Pending, resp runner.ResponseInput) error {
		// Accept → approve (task fires); Decline → cancel (task is dropped, stops re-nudging).
		// ActionCancel never reaches a ResumeHook — SubmitAnswer short-circuits it to
		// cancelConversation BEFORE applyResumeHook (runner_resume.go), and it aborts the whole
		// conversation — so the on-channel "No" button submits Decline, not Cancel.
		if pending.Kind != tools.KindApproval || len(pending.ResumeContext) == 0 {
			return nil
		}
		if resp.Action != askuser.ActionAccept && resp.Action != askuser.ActionDecline {
			return nil
		}
		var rc struct {
			Type   string `json:"type"`
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(pending.ResumeContext, &rc); err != nil {
			return fmt.Errorf("scheduled task resume context: %w", err)
		}
		if rc.Type != "scheduled_task_approval" {
			return nil
		}
		if rc.TaskID == "" {
			return fmt.Errorf("scheduled task resume context: missing task_id")
		}
		if resp.Action == askuser.ActionDecline {
			return store.CancelScheduledTaskInConversation(ctx, rc.TaskID, pending.ConversationID)
		}
		questions, err := store.ApprovalQuestions(ctx, rc.TaskID, pending.ConversationID)
		if err != nil {
			return err
		}
		if !slices.Contains(questions, pending.Question) {
			return errQuestionNotHostWritten
		}
		return store.ApproveScheduledTaskInConversation(ctx, rc.TaskID, pending.ConversationID)
	}
}

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
	question := mintedApprovalQuestion(kind, taskID)
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

// mintedApprovalQuestion is the sweep's bounded question: kind and short id, never the payload.
func mintedApprovalQuestion(kind, taskID string) string {
	return fmt.Sprintf("Scheduled %s task %s needs your approval. Approve or reject it below.", kind, shortTaskID(taskID))
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
