package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/approvaltext"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/jackc/pgx/v5"
)

var errScheduledTaskRequiresTx = errors.New("scheduled task approval requires an atomic resume transaction")

type scheduledTaskContext struct {
	TaskID   string
	Delivers string
}

func scheduledApprovalContext(pending askuser.Pending) *scheduledTaskContext {
	if pending.Kind != tools.KindApproval || len(pending.ResumeContext) == 0 {
		return nil
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(pending.ResumeContext, &fields) != nil || approvaltext.RootString(fields, "type") != "scheduled_task_approval" {
		return nil
	}
	return &scheduledTaskContext{TaskID: approvaltext.RootString(fields, "task_id"), Delivers: approvaltext.RootString(fields, "delivers")}
}

type scheduledTaskDecision struct {
	taskID  string
	pending askuser.Pending
}

func (r *Runner) operatorResumeClaim(token string, pending askuser.Pending, resp ResponseInput) (ResumeClaim, error) {
	claim := r.resumeClaim(token, pending, resp)
	if rc := scheduledApprovalContext(pending); rc != nil {
		claim.scheduledTask = &scheduledTaskDecision{taskID: rc.TaskID, pending: pending}
	}
	return claim, askuser.ValidateResumeAnswer(claim.Answer)
}

func (d *scheduledTaskDecision) apply(ctx context.Context, q *sqlc.Queries, claim ResumeClaim) error {
	owner := identityctx.IdentityID(ctx)
	if owner == "" {
		return fmt.Errorf("%w: scheduled task approval requires an authenticated identity", ErrResumeDecisionNotAllowed)
	}
	taskID, err := db.ParseUUID("task_id", d.taskID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrResumeDecisionNotAllowed, err)
	}
	conversationID, err := db.ParseUUID("conversation_id", claim.Turn.ConversationID)
	if err != nil {
		return err
	}
	status := "active"
	if claim.Answer.Action == askuser.ActionDecline {
		status = "cancelled"
	}
	row, err := q.ResolveChatTaskApproval(ctx, sqlc.ResolveChatTaskApprovalParams{
		NewStatus: status, TaskID: taskID, IdentityID: owner, ConversationID: conversationID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: scheduled task is unavailable for this decision", ErrResumeDecisionNotAllowed)
	}
	if err != nil {
		return fmt.Errorf("resolve scheduled task approval: %w", err)
	}
	if d.pending.Question == tools.ApprovalReminderQuestion(row.Kind, d.taskID) ||
		d.pending.Question == tools.ApprovalQuestion(row.Kind, d.taskID, row.ScheduleKind, row.CronExpr.String,
			int(row.EveryMinutes.Int32), row.Tz, d.reviewedFirstRun(row), row.Payload) {
		return nil
	}
	return fmt.Errorf("%w: scheduled task approval question was not written by the host", ErrResumeDecisionNotAllowed)
}

func (d *scheduledTaskDecision) reviewedFirstRun(row sqlc.ResolveChatTaskApprovalRow) time.Time {
	if !row.AlreadyDecided {
		return row.NextRunAt.Time
	}
	if row.ScheduleKind == "at" && row.RunAt.Valid {
		return row.RunAt.Time
	}
	// Once a recurring task has fired its next_run_at advances. Only its original
	// host review can supply the previous first fire for a read-only acknowledgement.
	if row.OriginToolCallID.Valid && row.OriginToolCallID.String == d.pending.ToolCallID {
		p, ok := approvaltext.FromContext(d.pending.Question, d.pending.ResumeContext)
		if ok && p.Key == approvaltext.ScheduledKey {
			loc := tools.LocationOrUTC(row.Tz)
			when := strings.TrimSuffix(p.Params["when"], " ("+loc.String()+")")
			if first, err := time.ParseInLocation("02/01 15:04", when, loc); err == nil {
				return first
			}
		}
	}
	return row.NextRunAt.Time
}
