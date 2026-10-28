package telegram

import (
	"context"
	"errors"
	"fmt"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/channels"
)

var _ channels.TelegramAccountDeliverer = (*Telegram)(nil)

// DeliverToTelegramAccount pushes text to one account a scheduled task named as a recipient
// (docs/superpowers/specs/2026-10-28-telegram-push-recipients-design.md). (false, nil) is a
// channel that cannot push now; an account the identity no longer links is a permanent error.
func (t *Telegram) DeliverToTelegramAccount(ctx context.Context, identityID string, telegramUserID int64, text string) (bool, error) {
	sender, resolver := t.pushSeams()
	if sender == nil || resolver == nil {
		return false, nil
	}
	if err := requireLinked(ctx, resolver, identityID, telegramUserID); err != nil {
		return false, err
	}
	if err := t.sendText(ctx, sender, telegramUserID, text); err != nil {
		return false, err
	}
	return true, nil
}

// DeliverApprovalToTelegramAccount is DeliverApproval for one account a task named.
func (t *Telegram) DeliverApprovalToTelegramAccount(ctx context.Context, identityID string, telegramUserID int64, token, taskID, kind string) (bool, error) {
	sender, resolver := t.pushSeams()
	if sender == nil || resolver == nil {
		return false, nil
	}
	if err := requireLinked(ctx, resolver, identityID, telegramUserID); err != nil {
		return false, err
	}
	if err := sendApproval(sender, telegramUserID, token, taskID, kind); err != nil {
		return false, err
	}
	return true, nil
}

// requireLinked confirms the identity still links the account a task named. Unlink means stop,
// so an account it no longer links is skipped, never replaced by another (spec R3).
func requireLinked(ctx context.Context, resolver accountResolver, identityID string, telegramUserID int64) error {
	_, found, err := linkedAccount(ctx, resolver, identityID, func(acct Account) bool {
		return acct.TelegramUserID == telegramUserID
	})
	if err != nil {
		return fmt.Errorf("telegram deliver: resolve identity %s: %w", identityID, err)
	}
	if !found {
		return undeliverable{fmt.Errorf("telegram account %d is no longer linked", telegramUserID)}
	}
	return nil
}

// undeliverable is a push no retry can deliver (spec R3, R5). Permanent is the method cron asks
// for through errors.As, so it needs no import of this package.
type undeliverable struct{ err error }

func (u undeliverable) Error() string { return u.err.Error() }
func (u undeliverable) Unwrap() error { return u.err }
func (undeliverable) Permanent() bool { return true }

// classified marks a send error permanent when Telegram refused it for good: the bot was
// blocked, the chat is gone, the user never started the bot or was deactivated. Anything else,
// a rate limit or the network, may succeed on a retry.
func classified(err error) error {
	for _, refused := range []error{tele.ErrBlockedByUser, tele.ErrChatNotFound, tele.ErrNotStartedByUser, tele.ErrUserIsDeactivated} {
		if errors.Is(err, refused) {
			return undeliverable{err}
		}
	}
	return err
}
