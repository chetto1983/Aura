package cron

// deliver.go projects explicit stdout routes to their owning conversation and explicit
// Telegram routes to the identity's Telegram account before the external notifier.
//
// ChannelDeliverer is a cron-LOCAL consumer-declared interface (the notify.go
// SelfSendResolver/Notifier idiom): cron declares the shape it needs and the
// composition root (cmd/aura) adapts *channels.Registry onto it. cron imports
// neither internal/channels nor internal/config.

import (
	"context"
	"log/slog"
	"time"
)

// ChannelDeliverer exposes deliberately separate rails. Conversation delivery proves the
// exact origin thread; identity delivery is reserved for an explicit cross-channel route
// such as notify=telegram; account delivery reaches one Telegram account a task named. All
// keep the tri-state contract: (true,nil)=delivered, (false,nil)=not owned,
// (false,err)=owns-but-failed, and an err that answers Permanent() true cannot succeed on
// a retry (see permanent).
type ChannelDeliverer interface {
	DeliverToIdentity(ctx context.Context, identityID, text string) (delivered bool, err error)
	DeliverToConversation(ctx context.Context, identityID, conversationID, text string) (delivered bool, err error)
	DeliverToTelegramAccount(ctx context.Context, identityID string, telegramUserID int64, text string) (delivered bool, err error)
}

// originGate decides whether a notification keyed on
// (identityID, conversationID, notifyRoute)
// should prefer the origin channel over the per-task Notifier route. It is the
// SINGLE source of the precedence semantics, shared by BOTH the live-task notify
// path (deliverToOrigin) and the swept-row sweep path (deliverSweptRow) — no
// duplicated precedence logic (Pitfall 2 / R4).
//
// The gate order is load-bearing:
//  1. no ChannelDeliverer → false.
//  2. none/whatsapp/email are already authoritative destinations; an un-owned identity
//     ("" / "local") cannot be projected to a channel.
//
// stdout means the exact origin conversation when one exists; without one it remains
// literal process output. Telegram is the only explicit identity-level cross-channel push.
func (d *Dispatch) originGate(identityID, conversationID, notifyRoute string) bool {
	if d.deps.ChannelDeliverer == nil {
		return false
	}
	if !originPreferring(notifyRoute) || identityID == "" || identityID == "local" {
		return false
	}
	if notifyRoute != string(RouteTelegram) && conversationID == "" {
		return false
	}
	return true
}

// originPreferring splits the routes that DEFER to the origin channel from the ones that
// deliberately pre-empt it. none, whatsapp and email skip this projection; stdout and
// telegram are the two routes this layer can deliver.
func originPreferring(route string) bool {
	switch route {
	case string(RouteStdout), string(RouteTelegram):
		return true
	default:
		return false
	}
}

// deliverToOrigin prefers the origin channel over the per-task Notifier route for a
// LIVE task. It returns handled=true when delivery is the channel's concern
// (delivered OR owns-but-failed-and-queued); the caller then skips Notifier.Notify.
// It returns false to fall through to today's route chain. A Telegram task that names
// its recipients pushes to each of them instead (deliverToRecipients).
//
// On owns-but-failed it queues a NEW failed pending row (same-channel retry key,
// keyed on task.IdentityID) and returns handled=true WITHOUT calling Notifier
// (Pitfall 3 — the owns-but-failed branch must never fall back to a sibling route),
// together with the channel's error so the conversation can say it was not delivered.
// A permanent failure queues nothing: no retry can deliver it (spec R5).
func (d *Dispatch) deliverToOrigin(ctx context.Context, task Task, runID, text string) (handled bool, undelivered error) {
	if !d.originGate(task.IdentityID, task.OriginConversationID, task.NotifyRoute) {
		return false, nil
	}
	if recipients := telegramRecipients(task); len(recipients) > 0 {
		return d.deliverToRecipients(ctx, task, recipients, runID, text)
	}
	var delivered bool
	var err error
	if task.NotifyRoute == string(RouteTelegram) {
		delivered, err = d.deps.ChannelDeliverer.DeliverToIdentity(ctx, task.IdentityID, text)
	} else {
		delivered, err = d.deps.ChannelDeliverer.DeliverToConversation(ctx, task.IdentityID, task.OriginConversationID, text)
	}
	if err != nil {
		// owns-but-failed → queue a failed pending row (the Step-2 sweep re-attempts
		// the SAME channel, keyed on the persisted identity by 20-04) and stop. Do NOT
		// fall back to Notifier — that would double-deliver if the channel push half-landed.
		if !permanent(err) {
			if perr := d.insertPendingNotification(ctx, task, runID, text, time.Now().UTC(), "failed", err.Error()); perr != nil {
				slog.Warn("persist failed origin-channel notification", "task", task.ID, "run", runID, "err", perr)
			}
		}
		return true, err
	}
	return delivered, nil
}

// sweepOutcome is the disposition of a swept row routed through the origin channel.
type sweepOutcome int

const (
	// sweepFallback: the explicit route is handled by Notifier.Notify.
	sweepFallback sweepOutcome = iota
	// sweepDelivered: the origin channel delivered → caller marks the row delivered.
	sweepDelivered
	// sweepKeep: the origin channel owns-but-failed → caller marks the EXISTING row
	// failed (same-channel retry on the next sweep) and does NOT fall back to Notifier
	// (Pitfall 3 — no cross-channel double-delivery during a sweep).
	sweepKeep
	// sweepUndeliverable: no retry can deliver the row (spec R3, R5) → caller ends it.
	sweepUndeliverable
)

// deliverSweptRow routes one swept pending_notifications row through the SAME origin
// gate as the live-task path, keyed on the ROW's identity snapshot (there is no live
// task at sweep time — the row carries the 0014 identity_id). Unlike the live path it
// never inserts a new pending row: the row already exists, so owns-but-failed returns
// sweepKeep and the caller marks the existing row failed for the next sweep. A Telegram
// row that names an account goes to that account only. The error is the channel's, for
// the row's last_error.
func (d *Dispatch) deliverSweptRow(ctx context.Context, n PendingNotification) (sweepOutcome, error) {
	if !d.originGate(n.IdentityID, n.OriginConversationID, n.NotifyRoute) {
		return sweepFallback, nil
	}
	var delivered bool
	var err error
	switch {
	case n.NotifyRoute == string(RouteTelegram) && n.TelegramUserID != 0:
		delivered, err = d.deps.ChannelDeliverer.DeliverToTelegramAccount(ctx, n.IdentityID, n.TelegramUserID, n.Body)
	case n.NotifyRoute == string(RouteTelegram):
		delivered, err = d.deps.ChannelDeliverer.DeliverToIdentity(ctx, n.IdentityID, n.Body)
	default:
		delivered, err = d.deps.ChannelDeliverer.DeliverToConversation(ctx, n.IdentityID, n.OriginConversationID, n.Body)
	}
	if permanent(err) {
		slog.Warn("origin-channel sweep delivery cannot succeed (ended)", "notification", n.ID, "err", err)
		return sweepUndeliverable, err
	}
	if err != nil {
		slog.Warn("origin-channel sweep delivery failed (kept for next sweep)", "notification", n.ID, "err", err)
		return sweepKeep, err
	}
	if delivered {
		return sweepDelivered, nil
	}
	return sweepFallback, nil
}
