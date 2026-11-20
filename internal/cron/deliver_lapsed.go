package cron

// deliver_lapsed.go expires a one-shot whose time passed while it waited for approval, and tells
// the operator (docs/superpowers/specs/2026-11-05-scheduled-delivery-defaults-and-messages-design.md,
// Part 3, items 5-6, and Part 2's "no answer"). Nothing runs or is sent: the task is cancelled, its
// pause is closed with an answer the model reads, and a failed run carries the notice.

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
)

const lapsedApprovalSweepLimit = 50

// LapsedApprovalStore lists and expires the lapsed one-shots; *Store satisfies it.
type LapsedApprovalStore interface {
	ListLapsedApprovals(ctx context.Context, limit int) ([]Task, error)
	ExpireLapsedApproval(ctx context.Context, task Task, homeID string, notifyAfter time.Time, origin LapsedOriginRecorder) error
}

var _ LapsedApprovalStore = (*Store)(nil)

// LapsedOriginRecorder writes on the same identity-scoped transaction as the expiry.
type LapsedOriginRecorder interface {
	AppendAssistantTurnTx(ctx context.Context, q *sqlc.Queries, conversationID, notice string) error
}

// PauseClosed is what closing a lapsed task's pause found (decision 36).
type PauseClosed int

const (
	// PauseNone means there is no pending pause for the task.
	PauseNone PauseClosed = iota
	// PauseClosedNow means this call closed the pause.
	PauseClosedNow
	// PauseAnswered means the conversation holds a resolved accept for the task.
	PauseAnswered
)

// LapsedPauseCloser closes the approval pause of a lapsed task with an answer the model reads.
// The composition root satisfies it over the Runner; cron imports neither askuser nor runner.
type LapsedPauseCloser interface {
	CloseLapsedApproval(ctx context.Context, task Task, answer string) (PauseClosed, error)
}

// LapsedMessages settles a lapsed message task's draft (decision 36).
type LapsedMessages interface {
	ApprovedForTask(ctx context.Context, ownerID, taskID string) (bool, error)
	ActivateReviewedMessage(ctx context.Context, ownerID, taskID string) error
	ExpireForTask(ctx context.Context, ownerID, taskID string) error // a pending draft only
}

// ExpireLapsedApprovals runs one pass over the lapsed one-shots, every tick before the approval
// reminders. A store error stops the pass and is returned; a row that cannot be settled now is
// skipped and retried on the next tick.
func (d *Dispatch) ExpireLapsedApprovals(ctx context.Context) error {
	if d.deps.LapsedApprovals == nil {
		return nil
	}
	tasks, err := d.deps.LapsedApprovals.ListLapsedApprovals(ctx, lapsedApprovalSweepLimit)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if ctx.Err() != nil {
			return nil
		}
		d.expireLapsed(identityctx.WithIdentityID(ctx, operationIdentity(task.IdentityID)), task)
	}
	return nil
}

func (d *Dispatch) expireLapsed(ctx context.Context, task Task) {
	if task.Kind == KindMessage && d.deps.LapsedMessages != nil {
		approved, err := d.deps.LapsedMessages.ApprovedForTask(ctx, task.IdentityID, task.ID)
		if err != nil {
			slog.Warn("lapsed approval: draft not read (retry next tick)", "task", task.ID, "err", err)
			return
		}
		if approved {
			// The review won the race: the task activates, and the handler sends inside its grace.
			if err := d.deps.LapsedMessages.ActivateReviewedMessage(ctx, task.IdentityID, task.ID); err != nil {
				slog.Warn("lapsed approval: approved message not activated (retry next tick)", "task", task.ID, "err", err)
			}
			return
		}
	}
	home := task
	var notifyAfter time.Time
	if task.NotifyRoute != string(RouteNone) {
		var err error
		home, err = d.resolveTaskHome(ctx, task)
		if err != nil {
			return // Retry before closing the original conversation's pause.
		}
		if d.deferNotification(task, nil) {
			notifyAfter, _ = d.deferredUntil(task)
		}
	}
	closed := PauseNone
	if d.deps.LapsedPauseCloser != nil {
		var err error
		closed, err = d.deps.LapsedPauseCloser.CloseLapsedApproval(ctx, task, lapsedPauseAnswer(task))
		if err != nil {
			// The pause closes before the task cancels (decision 26): a cancelled task under a
			// live approval would be one nobody can use.
			slog.Warn("lapsed approval: pause not closed (retry next tick)", "task", task.ID, "err", err)
			return
		}
	}
	// A reviewed message has its own grace period. An ordinary one-shot which is
	// still pending here cannot activate, even if an older resume already saved an accept.
	if closed == PauseAnswered && task.Kind == KindMessage {
		return
	}
	if closed == PauseNone && task.Kind == KindMessage && d.deps.LapsedMessages != nil {
		if err := d.deps.LapsedMessages.ExpireForTask(ctx, task.IdentityID, task.ID); err != nil {
			slog.Warn("lapsed approval: message draft not expired (retry next tick)", "task", task.ID, "err", err)
			return
		}
	}
	if err := d.deps.LapsedApprovals.ExpireLapsedApproval(ctx, task, home.OriginConversationID, notifyAfter, d.deps.LapsedOrigin); err != nil {
		slog.Warn("lapsed approval: task not expired (retry next tick)", "task", task.ID, "err", err)
	}
}

// lapsedApprovalText is the notice the operator reads: what did not run, and for when.
func lapsedApprovalText(task Task) string {
	loc, err := time.LoadLocation(task.TZ)
	if err != nil {
		loc = time.UTC
	}
	return fmt.Sprintf("%s for %s (%s) was not approved in time and did not run.",
		task.Kind, task.NextRunAt.In(loc).Format("02/01 15:04"), loc.String())
}

// lapsedPauseAnswer is the tool answer that closes the task's pause: the model reads it in the
// conversation the task was scheduled from.
func lapsedPauseAnswer(task Task) string {
	return fmt.Sprintf("Scheduled %s task %s was not approved in time; it has been cancelled and did not run. Do NOT re-schedule it unless the operator asks.",
		task.Kind, shortID(task.ID))
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
