package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A cancel stops a task for good, and the gateway lets it through at Normal tier. So the
// model cancels at once only a task its own conversation scheduled; any other one — from
// another conversation, the CLI or the cockpit — waits for the operator's approval
// (docs/superpowers/specs/2026-11-26-task-cancel-foreign-approval-design.md). The challenge
// copies plugin_pack's: a one-shot approval bound to the task, consumed by the repeat.

// TaskCancelAction is what a task cancel approval names.
type TaskCancelAction struct {
	TaskID string
}

// TaskCancelApprovals is the task cancel approvals' own store, apart from the shell's and
// the plugins', so neither of their resume hooks can approve a cancel.
type TaskCancelApprovals = ApprovalStore[TaskCancelAction]

// TaskCancelApprovalContext is the resume_context type of a task cancel approval.
const TaskCancelApprovalContext = "task_cancel_approval"

var taskCancelApprovalPrompt = approvalPrompt{tool: "task", name: "task_cancel_approval", contextType: TaskCancelApprovalContext}

const taskCancelPayloadRunes = 160

func (t *TaskTool) actionCancel(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	id, err := requireTaskID(ctx, raw, "cancel")
	if err != nil {
		return ToolResult{}, err
	}
	task, err := t.Store.CancelTarget(ctx, id)
	if err != nil {
		return ToolResult{}, fmt.Errorf("task cancel: %w", err)
	}
	if task.OriginConversationID == "" || task.OriginConversationID != callerConversationID(ctx) {
		session := SessionIDFromContext(ctx)
		digest := taskCancelDigest(task)
		if !t.CancelApprovals.Consume(session, digest) {
			challenge := t.CancelApprovals.CreateChallenge(session, digest, taskCancelQuestion(task, digest), TaskCancelAction{TaskID: task.ID})
			return taskCancelApprovalPrompt.required(challenge.Digest, challenge.Question,
				map[string]any{"action": "cancel", "task_id": task.ID}), nil
		}
	}
	if err := t.Store.CancelScheduledTask(ctx, task.ID); err != nil {
		return ToolResult{}, fmt.Errorf("task cancel: %w", err)
	}
	s := "cancelled task " + task.ID
	return ToolResult{Preview: s, Bytes: len(s)}, nil
}

// taskCancelDigest binds the approval to the task and what it does. The next fire is left out:
// a recurring task moves it at every run, and an approval must survive the operator's pause.
func taskCancelDigest(task ScheduledTask) string {
	return approvalDigest("task_cancel", task.ID, task.Kind, task.ScheduleKind, task.Payload)
}

// taskCancelQuestion is the approval card, one value per line. The payload is the only text
// someone else wrote, so it is capped and quoted, which escapes every newline and control
// character: it cannot forge a line of the card.
func taskCancelQuestion(task ScheduledTask, digest string) string {
	next := "none"
	if !task.NextRunAt.IsZero() {
		next = task.NextRunAt.UTC().Format(time.RFC3339)
	}
	payload := strings.Join(strings.Fields(task.Payload), " ")
	if runes := []rune(payload); len(runes) > taskCancelPayloadRunes {
		payload = string(runes[:taskCancelPayloadRunes]) + "…"
	}
	return strings.Join([]string{
		"Approve cancelling a task not scheduled in this chat?",
		"task: " + task.ID,
		"kind: " + task.Kind,
		"schedule: " + task.ScheduleKind,
		"next run: " + next,
		"payload: " + strconv.Quote(payload),
		"sha256: " + digest,
	}, "\n")
}

// callerConversationID is the conversation a call belongs to: a headless worker's explicit
// origin, else the tool-call session. It is the id a scheduled task records as its origin.
func callerConversationID(ctx context.Context) string {
	if id := OriginConversationIDFromContext(ctx); id != "" {
		return id
	}
	return SessionIDFromContext(ctx)
}

// Evict reclaims a finished conversation's cancel approvals (SessionEvictor, R-41).
func (t *TaskTool) Evict(sessionID string) {
	t.CancelApprovals.Evict(sessionID)
}
