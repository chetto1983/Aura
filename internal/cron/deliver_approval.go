package cron

// deliver_approval.go — fix-plan 1.7 / Amendment #92 (REVISED): the per-tick pending-approval
// reminder sweep. A task forced to status='pending_approval' (every agent_job per AG-016, or a
// risky-scored task) whose approval the model never relays is silently forgotten. This makes the
// sweep the DETERMINISTIC guarantor: per due row it (a) ensures a real scheduled_task_approval
// ask_user pause exists on the task's origin conversation (reused if the model relayed one, else
// minted host-side via ApprovalPauseEnsurer), then (b) pushes the actionable prompt to the ORIGIN
// channel (Telegram Sì/No via ApprovalChannel; WebUI needs no push — it pulls /api/approvals).
// Throttled by scheduler_tasks.approval_reminded_at. cron imports NEITHER askuser NOR channels —
// both are consumer-side seams the composition root satisfies (import-cycle constraint).

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"
)

const (
	defaultApprovalReminderSeconds = 3600
	approvalReminderSweepLimit     = 50
)

// ApprovalReminderStore lists due pending_approval tasks and stamps their throttle. The
// concrete *Store satisfies it; unit tests inject a fake without a pool.
type ApprovalReminderStore interface {
	ListDueApprovalReminders(ctx context.Context, cutoff time.Time, limit int) ([]Task, error)
	MarkApprovalReminded(ctx context.Context, id string) error
}

var _ ApprovalReminderStore = (*Store)(nil)

// ApprovalPauseEnsurer ensures a real scheduled_task_approval ask_user pause exists on a task's
// origin conversation (reused if the model already relayed one, else minted host-side), returning
// its token. The composition root satisfies it over askuser.Store + the Runner; cron never imports
// either. Nil → the approval sweep no-ops.
type ApprovalPauseEnsurer interface {
	EnsureApprovalPause(ctx context.Context, taskID, conversationID, kind string) (token string, err error)
}

// ApprovalChannel pushes an ACTIONABLE approval prompt (inline accept/reject bound to the pause
// token) to the channel that owns identityID. *channels.Registry satisfies it (DeliverApproval).
// Nil → the approval sweep no-ops. A (false,nil) return is expected for a WebUI-origin identity (no
// push channel owns it; it pulls the pause) and is NOT treated as an error.
type ApprovalChannel interface {
	DeliverApproval(ctx context.Context, identityID, token, taskID, kind string) (delivered bool, err error)
}

// approvalReminderInterval resolves AURA_SCHEDULER_APPROVAL_REMINDER_SEC. Unlike envInt,
// a valid <=0 value DISABLES the sweep (returns 0) rather than falling back to the
// default — the kill switch (Amendment #92 point 5). Empty/invalid → the 1h default.
func approvalReminderInterval() time.Duration {
	v := os.Getenv("AURA_SCHEDULER_APPROVAL_REMINDER_SEC")
	if v == "" {
		return defaultApprovalReminderSeconds * time.Second
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return defaultApprovalReminderSeconds * time.Second
	}
	if n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// sweepApprovalReminders is the deterministic on-channel HITL guarantor (Amendment #92 revised):
// per due pending_approval row it (a) ensures a real scheduled_task_approval ask_user pause exists
// on the task's origin conversation (reused or minted host-side), then (b) pushes the actionable
// prompt to the origin channel. It runs on the scheduler tick right after sweepNotifications.
// Every selected row is stamped after the attempt REGARDLESS of outcome (ensured/minted, pushed /
// no-push-channel / channel-error) so a pending approval re-nudges at most once per cadence and a
// permanently-failing channel cannot spam the tick. A store-query error is returned (swallowed per
// tick by runScheduledScan → slog.Warn, never terminates the scheduler); a per-row ensure/push/stamp
// error is logged and the sweep continues.
//
// A WebUI-origin row surfaces its pause via the pull /api/approvals: EnsureApprovalPause still runs
// (so the pause is minted if the model never relayed), and DeliverApproval returns (false,nil) — no
// push channel owns it — which is the expected no-op, not an error.
func (d *Dispatch) sweepApprovalReminders(ctx context.Context) error {
	interval := approvalReminderInterval()
	if interval <= 0 || d.deps.ApprovalReminderStore == nil ||
		d.deps.ApprovalPauseEnsurer == nil || d.deps.ApprovalChannel == nil {
		return nil
	}
	cutoff := time.Now().UTC().Add(-interval)
	rows, err := d.deps.ApprovalReminderStore.ListDueApprovalReminders(ctx, cutoff, approvalReminderSweepLimit)
	if err != nil {
		return err
	}
	for _, task := range rows {
		token, eerr := d.deps.ApprovalPauseEnsurer.EnsureApprovalPause(ctx, task.ID, task.OriginConversationID, string(task.Kind))
		if eerr != nil {
			// Ensure failed (e.g. transient DB): still stamp so a permanently-failing ensure
			// cannot spam the tick; retried next cadence.
			slog.Warn("approval reminder: ensure pause failed (retry next cadence)", "task", task.ID, "err", eerr)
		} else {
			delivered, derr := d.deps.ApprovalChannel.DeliverApproval(ctx, task.IdentityID, token, task.ID, string(task.Kind))
			switch {
			case derr != nil:
				slog.Warn("approval reminder push failed (retry next cadence)", "task", task.ID, "err", derr)
			case !delivered:
				slog.Debug("approval reminder: no push channel owns identity (WebUI pulls the pause)", "task", task.ID, "identity", task.IdentityID)
			}
		}
		if merr := d.deps.ApprovalReminderStore.MarkApprovalReminded(ctx, task.ID); merr != nil {
			slog.Warn("mark approval reminded", "task", task.ID, "err", merr)
		}
	}
	return nil
}
