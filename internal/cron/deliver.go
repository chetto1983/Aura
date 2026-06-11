package cron

// deliver.go is the origin-channel precedence helper (Phase 20 R4/R7). It sits in
// FRONT of the per-task Notifier route chain (notify.go): when the default-on
// kill-switch is on and a task carries an owning identity snapshot (no explicit
// route), the dispatcher prefers the origin channel — a reminder set in a Telegram
// DM lands back in that DM, not on the stdout/whatsapp fallback route.
//
// ChannelDeliverer is a cron-LOCAL consumer-declared interface (the notify.go
// SelfSendResolver/Notifier idiom): cron declares the shape it needs and the
// composition root (cmd/aura) adapts *channels.Registry onto it. cron imports
// NEITHER internal/channels NOR internal/config — the kill-switch bool is resolved
// once at the root via envBoolDefault and injected as DispatchDeps.PreferOriginChannel.

import (
	"context"
	"log/slog"
	"time"
)

// ChannelDeliverer pushes a notification to whatever channel owns identityID. It is
// the cron-local seam the composition root adapts *channels.Registry onto (the
// Registry.DeliverToIdentity fan-out, 20-01). The tri-state return mirrors the
// channels.Deliverer contract: (true,nil)=delivered → stop; (false,nil)=no channel
// owns the identity → caller falls back to the route; (false,err)=owns-but-failed →
// caller queues a same-channel retry and does NOT fall back (no double-delivery).
type ChannelDeliverer interface {
	DeliverToIdentity(ctx context.Context, identityID, text string) (delivered bool, err error)
}

// deliverToOrigin prefers the origin channel over the per-task Notifier route. It
// returns handled=true when delivery is the channel's concern (delivered OR
// owns-but-failed-and-queued); the caller then skips Notifier.Notify. It returns
// false to fall through to today's route chain.
//
// The gate order is load-bearing (Pitfall 2):
//  1. kill-switch off / no ChannelDeliverer → false (legacy route-only, regression guard).
//  2. an explicit NotifyRoute ALWAYS wins (channel skipped, R7); an un-owned identity
//     ("" / "local") → route fallback. This is checked BEFORE the channel.
//  3. otherwise push to the owning channel; on error queue a failed pending row
//     (same-channel retry key) and return handled=true WITHOUT calling Notifier
//     (Pitfall 3 — the owns-but-failed branch must never fall back to a sibling route).
func (d *Dispatch) deliverToOrigin(ctx context.Context, task Task, runID, text string) (handled bool) {
	if !d.deps.PreferOriginChannel || d.deps.ChannelDeliverer == nil {
		return false
	}
	if task.NotifyRoute != "" || task.IdentityID == "" || task.IdentityID == "local" {
		return false
	}
	delivered, err := d.deps.ChannelDeliverer.DeliverToIdentity(ctx, task.IdentityID, text)
	if err != nil {
		// owns-but-failed → queue a failed pending row (the Step-2 sweep re-attempts
		// the SAME channel, keyed on the persisted identity by 20-04) and stop. Do NOT
		// fall back to Notifier — that would double-deliver if the channel push half-landed.
		if perr := d.insertPendingNotification(ctx, task, runID, text, time.Now().UTC(), "failed", 0, err.Error()); perr != nil {
			slog.Warn("persist failed origin-channel notification", "task", task.ID, "run", runID, "err", perr)
		}
		return true
	}
	return delivered
}
