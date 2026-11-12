package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/scoring"
)

// A task that needs approval is shown to the operator in the chat that creates it, before it can
// fire (docs/superpowers/specs/2026-11-05-scheduled-delivery-defaults-and-messages-design.md,
// Part 3). The agent parks the model's own call behind a host-authored question naming what will
// run; with nobody in the chat the task waits and the approval sweep reminds the operator.

const taskSummaryMaxRunes = 160

// taskNeedsApproval is the one gating rule (AG-016 plus the scorer), read before the call runs
// and inside it. Both read only Kind, ScheduleKind and Payload, and the scorer is pure, so the
// two cannot disagree (decision 19).
func taskNeedsApproval(a taskArgs) (scoring.RiskTier, bool) {
	tier := scoring.ComputeTaskTier(scoring.TaskArgs{Kind: a.Kind, ScheduleKind: a.ScheduleKind, Payload: a.Payload})
	// AG-016: an agent_job runs a fresh full-tool agent turn at fire time, so a benign-looking
	// goal can still drive any later action. The keyword score is necessary, not sufficient.
	return tier, a.Kind == "agent_job" || scoring.GateRecommended(tier)
}

// approvalSchedule is the cadence the question names.
func approvalSchedule(spec resolvedSpec) string {
	switch spec.ScheduleKind {
	case "every":
		return fmt.Sprintf("every %dm", spec.EveryMinutes)
	case "cron":
		return fmt.Sprintf("cron %q", spec.CronExpr)
	default:
		return "once"
	}
}

// approvalWhen is the first fire as the operator reads it.
func approvalWhen(next time.Time, loc *time.Location) string {
	return fmt.Sprintf("%s (%s)", next.In(loc).Format("02/01 15:04"), loc.String())
}

// taskSummary is what the task runs, on one line: an agent_job's goal, a reminder's text, else
// the kind's name.
func taskSummary(kind string, payload json.RawMessage) string {
	field := map[string]string{"agent_job": "goal", "reminder": "text"}[kind]
	if field == "" {
		return kind
	}
	var fields map[string]any
	_ = json.Unmarshal(payload, &fields)
	value, _ := fields[field].(string)
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return "(no goal)"
	}
	if runes := []rune(value); len(runes) > taskSummaryMaxRunes {
		value = string(runes[:taskSummaryMaxRunes]) + "…"
	}
	return value
}

// ApprovalQuestion is the host-authored question for a task awaiting approval, in the zone the
// task row stores. The scheduled-task hook recomputes it from the row and accepts only a pause
// that asks exactly this (decision 35), so the model cannot relay an innocent question over a
// pending job's id.
func ApprovalQuestion(kind, taskID, scheduleKind, cronExpr string, everyMinutes int, tz string, next time.Time, payload json.RawMessage) string {
	spec := resolvedSpec{ScheduleKind: scheduleKind, CronExpr: cronExpr, EveryMinutes: everyMinutes, TZ: tz}
	return approvalQuestion(kind, taskID, spec, next, payload)
}

func approvalQuestion(kind, taskID string, spec resolvedSpec, next time.Time, payload json.RawMessage) string {
	return fmt.Sprintf("Approve scheduled %s task %s (%s), first run %s? It runs: %s. It will not run until you approve.",
		kind, shortScheduledTaskID(taskID), approvalSchedule(spec), approvalWhen(next, LocationOrUTC(spec.TZ)), taskSummary(kind, payload))
}

// hostReviewApproval persists a task that needs approval pending_approval, keyed to the model's
// call, and returns the approval pause naming what will run. Any refusal returns (nil, false), so
// Execute gives the model the same refusal; a task that needs no approval is not parked.
func (t *TaskTool) hostReviewApproval(ctx context.Context, conversationID, toolCallID string, a taskArgs) (*ErrAwaitingUserInput, bool) {
	if validateSchedule(a) != nil || t.Store == nil {
		return nil, false
	}
	if _, needs := taskNeedsApproval(a); !needs {
		return nil, false
	}
	plan, directive, err := t.resolveDelivery(ctx, &a)
	if err != nil || directive != nil {
		return nil, false
	}
	if strings.TrimSpace(a.TZ) == "" {
		a.TZ = t.scheduleZone(ctx)
	}
	spec, next, err := t.resolveSchedule(a)
	if err != nil {
		return nil, false
	}
	task, err := t.Store.CreateScheduledTask(ctx, CreateTaskInput{
		Kind: a.Kind, ScheduleKind: spec.ScheduleKind, CronExpr: spec.CronExpr, EveryMinutes: spec.EveryMinutes,
		RunAt: spec.RunAt, TZ: spec.TZ, Payload: payloadJSON(a.Payload), StepBudget: a.StepBudget,
		NotifyRoute: a.Notify, Status: "pending_approval", NextRunAt: next,
		OriginConversationID: conversationID, OriginToolCallID: toolCallID,
		TelegramRecipients: TelegramRecipientIDs(plan.Recipients),
	})
	if err != nil {
		slog.Warn("task: approval-gated task not created", "error_type", errorType(err))
		return nil, false
	}
	// The delivery note rides the host-written context so the APPROVED answer can repeat where the
	// result goes: the model once confirmed a third party's address the route never reaches.
	resume, err := json.Marshal(map[string]string{"type": "scheduled_task_approval", "task_id": task.ID, "delivers": plan.Note})
	if err != nil {
		return nil, false
	}
	return &ErrAwaitingUserInput{
		Question: approvalQuestion(a.Kind, task.ID, spec, next, a.Payload), Kind: KindApproval, Priority: 90,
		ToolCallID: toolCallID, ResumeContext: resume,
	}, true
}
