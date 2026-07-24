package main

// serve_scheduled_approval.go is the composition-root wiring for the on-channel scheduled-task
// approval (Amendment #92 revised / fix-plan 1.7): the owner-scoped approve/cancel store
// transitions, the ResumeHook that resolves a scheduled_task_approval ask_user pause, and the
// approvalPauseEnsurer the reminder sweep drives to guarantee a real HITL pause exists on the
// task's origin conversation. Extracted from serve_adapters.go on touch (600-LOC cap).

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/runner"
)

// Compile-time proof the composition-root ensurer satisfies the cron approval-sweep seam, and
// that its live collaborators satisfy the narrow ensurer interfaces (*askuser.Store lists pending;
// *runner.Runner mints). These keep the serve_dispatch wiring honest without a full-boot test.
var (
	_ cron.ApprovalPauseEnsurer = approvalPauseEnsurer{}
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
		WHERE id = $1 AND origin_conversation_id = $2 AND status = 'pending_approval'`, taskID, conversationID)
	if err != nil {
		return fmt.Errorf("approve scheduled task %s: %w", taskID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("scheduled task %s is not awaiting approval in conversation %s", taskID, conversationID)
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
		WHERE id = $1 AND origin_conversation_id = $2 AND status = 'pending_approval'`, taskID, conversationID)
	if err != nil {
		return fmt.Errorf("cancel scheduled task %s: %w", taskID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("scheduled task %s is not awaiting approval in conversation %s", taskID, conversationID)
	}
	return nil
}

// scheduledTaskApprover is the owner-scoped approve/cancel seam the scheduled-task resume
// hook drives. *cronTaskStore satisfies it live; unit tests inject a fake.
type scheduledTaskApprover interface {
	ApproveScheduledTaskInConversation(ctx context.Context, taskID, conversationID string) error
	CancelScheduledTaskInConversation(ctx context.Context, taskID, conversationID string) error
}

// newScheduledTaskResumeHook is the production ResumeHook that resolves a pending_approval
// scheduled task when the operator ACTS on its scheduled_task_approval ask_user pause — the
// on-channel HITL parity for the scheduler (the analog of newShellResumeHook/newGatewayResumeHook).
// The pause is created either by the model relaying scheduledApprovalRequiredResult, or
// deterministically by the reminder sweep via approvalPauseEnsurer. It keys the transition on the
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
		return store.ApproveScheduledTaskInConversation(ctx, rc.TaskID, pending.ConversationID)
	}
}

// approvalMinter is the narrow pause-minting seam the ensurer drives when no matching pause
// exists. *runner.Runner satisfies it (MintApprovalPause); unit tests inject a fake.
type approvalMinter interface {
	MintApprovalPause(ctx context.Context, convID, question string, resumeContext json.RawMessage) (string, error)
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
	question := fmt.Sprintf("Scheduled %s task %s needs your approval. Approve or reject it below.", kind, shortTaskID(taskID))
	token, err := e.minter.MintApprovalPause(ctx, conversationID, question, rc)
	return token, true, err
}

// shortTaskID renders the first 8 chars of a task id for a bounded, secret-safe prompt (the
// approval nudge carries kind + short id only, never the payload).
func shortTaskID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
