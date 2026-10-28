package cron

// deliver_recipients.go: a Telegram task that names its recipients pushes to each of them
// (docs/superpowers/specs/2026-10-28-telegram-push-recipients-design.md, R3-R5), and a push
// queued for later is queued once per recipient, so a retry never reaches a phone that
// already has it.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"
)

// telegramRecipients are the Telegram accounts a task's pushes reach, each once. Only a
// notify=telegram task names them; none means the identity's account seen last (R6).
func telegramRecipients(task Task) []int64 {
	if task.NotifyRoute != string(RouteTelegram) {
		return nil
	}
	var distinct []int64
	for _, account := range task.TelegramRecipients {
		if !slices.Contains(distinct, account) {
			distinct = append(distinct, account)
		}
	}
	return distinct
}

// deliverToRecipients pushes a live task's text to each recipient on its own. A failure a
// retry can fix queues a failed row for that account only; a permanent one, an account no
// longer linked or a bot it blocked, queues nothing. When no channel could push to any of
// them it returns handled=false, so the route chain handles it as for any Telegram push.
// The joined errors name each account the push did not reach.
func (d *Dispatch) deliverToRecipients(ctx context.Context, task Task, recipients []int64, runID, text string) (handled bool, undelivered error) {
	var errs []error
	var unowned []int64
	for _, account := range recipients {
		delivered, err := d.deps.ChannelDeliverer.DeliverToTelegramAccount(ctx, task.IdentityID, account, text)
		switch {
		case delivered:
		case err == nil:
			unowned = append(unowned, account)
		case permanent(err):
			errs = append(errs, err)
		default:
			errs = append(errs, err)
			d.queueRecipientRetry(ctx, task, account, runID, text, err)
		}
	}
	if len(unowned) == len(recipients) {
		return false, nil
	}
	// The channel stopped part way through (a token swap): retry the accounts it did not
	// reach, never the ones it did.
	for _, account := range unowned {
		err := fmt.Errorf("telegram account %d: no channel could push", account)
		errs = append(errs, err)
		d.queueRecipientRetry(ctx, task, account, runID, text, err)
	}
	return true, errors.Join(errs...)
}

func (d *Dispatch) queueRecipientRetry(ctx context.Context, task Task, account int64, runID, text string, cause error) {
	if err := d.insertPendingNotificationFor(ctx, task, account, runID, text, time.Now().UTC(), "failed", cause.Error()); err != nil {
		slog.Warn("persist failed Telegram recipient notification", "task", task.ID, "run", runID, "account", account, "err", err)
	}
}

// permanent reports a push error no retry can fix. The channel marks it by answering
// Permanent() true (channels.TelegramAccountDeliverer), so cron imports neither channels nor
// telegram.
func permanent(err error) bool {
	var p interface{ Permanent() bool }
	return errors.As(err, &p) && p.Permanent()
}

// insertPendingNotification queues a push for the sweep: one row per Telegram recipient the
// task names, so each account is retried alone (spec R4), else one row.
func (d *Dispatch) insertPendingNotification(ctx context.Context, task Task, runID, body string, notifyAfter time.Time, status, lastErr string) error {
	recipients := telegramRecipients(task)
	if len(recipients) == 0 {
		return d.insertPendingNotificationFor(ctx, task, 0, runID, body, notifyAfter, status, lastErr)
	}
	var errs []error
	for _, account := range recipients {
		errs = append(errs, d.insertPendingNotificationFor(ctx, task, account, runID, body, notifyAfter, status, lastErr))
	}
	return errors.Join(errs...)
}

func (d *Dispatch) insertPendingNotificationFor(ctx context.Context, task Task, telegramUserID int64, runID, body string, notifyAfter time.Time, status, lastErr string) error {
	if d.deps.NotificationStore == nil {
		return nil
	}
	_, err := d.deps.NotificationStore.InsertPendingNotification(ctx, InsertPendingNotificationParams{
		RunID:          runID,
		NotifyRoute:    task.NotifyRoute,
		Body:           body,
		NotifyAfter:    notifyAfter,
		LastError:      lastErr,
		Status:         status,
		IdentityID:     task.IdentityID,
		TelegramUserID: telegramUserID,
	})
	return err
}
