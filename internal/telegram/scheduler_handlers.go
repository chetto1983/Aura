package telegram

import (
	"context"
	"fmt"
	"strconv"

	"github.com/aura/aura/internal/scheduler"
	tele "gopkg.in/telebot.v4"
)

// dispatchTask is the scheduler.Dispatcher implementation. It routes a
// fired task to the right side-effect: reminders go to Telegram via the
// stored RecipientID, wiki_maintenance runs the autonomous pass.
// Errors are returned so the scheduler records last_error; the row is
// always persisted regardless of outcome so the LLM can introspect.
func (b *Bot) dispatchTask(ctx context.Context, task *scheduler.Task) error {
	switch task.Kind {
	case scheduler.KindReminder:
		return b.dispatchReminder(task)
	case scheduler.KindWikiMaintenance:
		return b.dispatchWikiMaintenance(ctx)
	default:
		return fmt.Errorf("dispatchTask: unknown kind %q", task.Kind)
	}
}

func (b *Bot) dispatchReminder(task *scheduler.Task) error {
	if task.RecipientID == "" {
		return fmt.Errorf("reminder %q has no recipient", task.Name)
	}
	chatID, err := strconv.ParseInt(task.RecipientID, 10, 64)
	if err != nil {
		return fmt.Errorf("parse recipient %q: %w", task.RecipientID, err)
	}
	body := task.Payload
	if body == "" {
		body = "Reminder: " + task.Name
	} else {
		body = "⏰ " + body
	}
	if _, err := b.bot.Send(tele.ChatID(chatID), body); err != nil {
		return fmt.Errorf("send reminder: %w", err)
	}
	return nil
}

// dispatchWikiMaintenance runs the autonomous nightly wiki pass via
// MaintenanceJob: rebuilds index, lints, auto-fixes single-candidate
// broken links (Levenshtein ≤ 2), and defers the rest to 12h.
func (b *Bot) dispatchWikiMaintenance(ctx context.Context) error {
	if b.wiki == nil {
		return fmt.Errorf("wiki maintenance: wiki store unavailable")
	}
	b.wiki.RebuildIndex(ctx)
	job := scheduler.NewMaintenanceJob(b.wiki, b.logger).
		WithIssuesStore(b.issues).
		WithOwnerNotifier(func(ctx context.Context, msg string) {
			for _, ownerID := range b.collectOwnerIDs() {
				if err := b.SendToUser(ownerID, msg); err != nil {
					b.logger.Warn("maintenance notify failed", "owner", ownerID, "error", err)
				}
			}
		})
	fixed, deferred, err := job.Run(ctx)
	if err != nil {
		return fmt.Errorf("wiki maintenance: %w", err)
	}
	b.wiki.AppendLog(ctx, "nightly-maintenance", "")
	b.logger.Info("nightly wiki maintenance complete",
		"auto_fixed", fixed, "deferred", deferred)
	return nil
}
