package cron

// deliver_approval.go — fix-plan 1.7 / Amendment #92: the per-tick pending-approval
// reminder sweep. A task forced to status='pending_approval' (every agent_job per AG-016,
// or a risky-scored task) whose approval the model never relays is silently forgotten.
// This re-surfaces it to its origin channel until it is approved or cancelled, throttled
// by scheduler_tasks.approval_reminded_at. It reuses the existing ChannelDeliverer seam —
// no new channel wiring, no cron→channels/tools import (the import-cycle constraint the
// notify.go/deliver.go headers document).

import (
	"context"
	"fmt"
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

// sweepApprovalReminders re-surfaces channel-owned pending_approval tasks to their origin
// channel, throttled to once per cadence. It runs on the scheduler tick right after
// sweepNotifications. Every selected row is stamped after the attempt REGARDLESS of
// delivery outcome (delivered / no-channel / channel-error) so a pending approval
// re-nudges at most once per cadence and a permanently-failing channel cannot spam the
// tick (Amendment #92 point 4). A store-query error is returned (swallowed per tick by
// runScheduledScan → slog.Warn, never terminates the scheduler); a per-row delivery or
// stamp error is logged and the sweep continues.
func (d *Dispatch) sweepApprovalReminders(ctx context.Context) error {
	interval := approvalReminderInterval()
	if interval <= 0 || d.deps.ApprovalReminderStore == nil || d.deps.ChannelDeliverer == nil {
		return nil
	}
	cutoff := time.Now().UTC().Add(-interval)
	rows, err := d.deps.ApprovalReminderStore.ListDueApprovalReminders(ctx, cutoff, approvalReminderSweepLimit)
	if err != nil {
		return err
	}
	for _, task := range rows {
		delivered, derr := d.deps.ChannelDeliverer.DeliverToIdentity(ctx, task.IdentityID, approvalReminderText(task))
		switch {
		case derr != nil:
			slog.Warn("approval reminder delivery failed (retry next cadence)", "task", task.ID, "err", derr)
		case !delivered:
			slog.Debug("approval reminder: no channel owns identity", "task", task.ID, "identity", task.IdentityID)
		}
		if merr := d.deps.ApprovalReminderStore.MarkApprovalReminded(ctx, task.ID); merr != nil {
			slog.Warn("mark approval reminded", "task", task.ID, "err", merr)
		}
	}
	return nil
}

// approvalReminderText is the bounded nudge (Amendment #92 point 4): task kind + a short
// id for identification, never the raw payload — no goal/secret leakage beyond what the
// origin channel already saw when the task was scheduled.
func approvalReminderText(task Task) string {
	id := task.ID
	if len(id) > 8 {
		id = id[:8]
	}
	return fmt.Sprintf("A scheduled %s task (id %s) is awaiting your approval. Approve or cancel it in the Aura cockpit.", task.Kind, id)
}
