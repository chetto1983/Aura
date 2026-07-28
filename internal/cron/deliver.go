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

// originGate decides whether a notification keyed on (identityID, notifyRoute)
// should prefer the origin channel over the per-task Notifier route. It is the
// SINGLE source of the precedence semantics, shared by BOTH the live-task notify
// path (deliverToOrigin) and the swept-row sweep path (deliverSweptRow) — no
// duplicated precedence logic (Pitfall 2 / R4).
//
// The gate order is load-bearing:
//  1. kill-switch off / no ChannelDeliverer → false (legacy route-only, regression guard).
//  2. a DELIBERATE alternate-channel NotifyRoute (whatsapp/email) wins (channel skipped, R7);
//     an un-owned identity ("" / "local") → route fallback. Checked BEFORE the channel.
//
// "stdout" is the always-available fallback sink (notify.go), NOT a deliberate external
// channel: the scheduling agent fills it in as the implicit default route, so it must
// defer to the origin channel exactly like an unset route — otherwise the headline case
// (a Telegram reminder routing back to its DM) silently lands on the server console. Only
// whatsapp/email pre-empt origin (R7 amended after the Phase 20 live gate proved the agent
// auto-populates notify="stdout").
func (d *Dispatch) originGate(identityID, notifyRoute string) bool {
	if !d.deps.PreferOriginChannel || d.deps.ChannelDeliverer == nil {
		return false
	}
	if !originPreferring(notifyRoute) || identityID == "" || identityID == "local" {
		return false
	}
	return true
}

// originPreferring splits the routes that DEFER to the origin channel from the ones that
// deliberately pre-empt it. whatsapp and email are alternate destinations: choosing one
// means "send it somewhere else", so they skip the channel (R7). "" and "stdout" defer
// because neither expresses a destination — the agent auto-fills stdout as the implicit
// default. "telegram" deferring is the opposite of a fallback: it is the operator asking
// for the origin channel BY NAME, which the composite Notifier cannot honour (it holds no
// identity and no channel registry — notify.go), so the only layer that can deliver it is
// this one.
func originPreferring(route string) bool {
	switch route {
	case "", string(RouteStdout), string(RouteTelegram):
		return true
	default:
		return false
	}
}

// deliverToOrigin prefers the origin channel over the per-task Notifier route for a
// LIVE task. It returns handled=true when delivery is the channel's concern
// (delivered OR owns-but-failed-and-queued); the caller then skips Notifier.Notify.
// It returns false to fall through to today's route chain.
//
// On owns-but-failed it queues a NEW failed pending row (same-channel retry key,
// keyed on task.IdentityID) and returns handled=true WITHOUT calling Notifier
// (Pitfall 3 — the owns-but-failed branch must never fall back to a sibling route).
func (d *Dispatch) deliverToOrigin(ctx context.Context, task Task, runID, text string) (handled bool) {
	if !d.originGate(task.IdentityID, task.NotifyRoute) {
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

// sweepOutcome is the disposition of a swept row routed through the origin channel.
type sweepOutcome int

const (
	// sweepFallback: no origin channel owns the row → caller delivers via Notifier.Notify.
	sweepFallback sweepOutcome = iota
	// sweepDelivered: the origin channel delivered → caller marks the row delivered.
	sweepDelivered
	// sweepKeep: the origin channel owns-but-failed → caller marks the EXISTING row
	// failed (same-channel retry on the next sweep) and does NOT fall back to Notifier
	// (Pitfall 3 — no cross-channel double-delivery during a sweep).
	sweepKeep
)

// deliverSweptRow routes one swept pending_notifications row through the SAME origin
// gate as the live-task path, keyed on the ROW's identity snapshot (there is no live
// task at sweep time — the row carries the 0014 identity_id). Unlike the live path it
// never inserts a new pending row: the row already exists, so owns-but-failed returns
// sweepKeep and the caller marks the existing row failed for the next sweep.
func (d *Dispatch) deliverSweptRow(ctx context.Context, n PendingNotification) sweepOutcome {
	if !d.originGate(n.IdentityID, n.NotifyRoute) {
		return sweepFallback
	}
	delivered, err := d.deps.ChannelDeliverer.DeliverToIdentity(ctx, n.IdentityID, n.Body)
	if err != nil {
		slog.Warn("origin-channel sweep delivery failed (kept for next sweep)", "notification", n.ID, "err", err)
		return sweepKeep
	}
	if delivered {
		return sweepDelivered
	}
	return sweepFallback
}
