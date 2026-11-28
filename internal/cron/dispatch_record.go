package cron

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/chetto1983/aura/internal/steer"
)

// SteerPublisher queues a message for the turn running in a conversation, delivered after its
// next tool round. *steer.PostgresStore satisfies it.
type SteerPublisher interface {
	Push(conv, source, text string) error
}

// recordToOrigin writes the run's outcome into the conversation it was scheduled
// from. It is deliberately NOT part of notify: a push tells an operator who is
// elsewhere, a record answers the operator who asked here. Both are wanted, and the
// live measurement that prompted this had the push succeed while the asking
// conversation learned nothing.
//
// A push that failed is said so beside the outcome. Measured 2026-10-05: a WhatsApp
// reminder that reached nobody appeared here as plain text, which read as delivered.
// A failure that asked for silence asked it of both, and leaves nothing here either.
//
// The record names its author, and an agent_job's record is also steered to a turn that
// may be running there: measured 2026-11-28, a run_now job recorded its report as plain
// assistant text while the turn that fired it slept and polled, never saw it, and in the
// next turn did not recognise the record as the job's.
//
// Best-effort. The work already happened and is already on the run ledger, so a
// failed write or push is a WARN and never a run failure.
func (d *Dispatch) recordToOrigin(ctx context.Context, task Task, summary string, runErr, undelivered error) {
	if task.NotifyRoute == string(RouteNone) || d.deps.ConversationRecorder == nil || task.OriginConversationID == "" || suppressed(runErr) {
		return
	}
	text := summary
	if runErr != nil {
		text = failureText(task, runErr)
	}
	if text == "" {
		return
	}
	if undelivered != nil {
		text += fmt.Sprintf("\n\nNot delivered via %s: %v", task.NotifyRoute, undelivered)
	}
	text = scheduledRecordText(task, text)
	if err := d.deps.ConversationRecorder.AppendAssistantTurn(ctx, task.OriginConversationID, text); err != nil {
		slog.Warn("cron: could not record the run outcome in its origin conversation",
			"kind", task.Kind, "conversation", task.OriginConversationID, "err", err)
	}
	d.steerToOrigin(task, text)
}

// steerToOrigin is limited to agent_job: a steer row still queued when a turn ends starts a
// follow-on turn (Runner.deliverLeftoverSteer), so a watch that runs every few minutes would
// buy a model call each time.
func (d *Dispatch) steerToOrigin(task Task, text string) {
	if task.Kind != KindAgentJob || d.deps.Steer == nil {
		return
	}
	if err := d.deps.Steer.Push(task.OriginConversationID, steer.SourceScheduler, text); err != nil {
		slog.Warn("cron: could not steer the run outcome to its origin conversation",
			"kind", task.Kind, "conversation", task.OriginConversationID, "err", err)
	}
}

// scheduledRecordText prefixes a record with its author, so a later turn reading the history
// does not take the report for its own words.
func scheduledRecordText(task Task, text string) string {
	id := task.ID
	if len(id) > 8 {
		id = id[:8]
	}
	return fmt.Sprintf("[Scheduled task report — %s %s]\n%s", task.Kind, id, text)
}
