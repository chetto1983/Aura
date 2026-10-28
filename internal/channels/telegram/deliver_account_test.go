package telegram

import (
	"context"
	"errors"
	"fmt"
	"testing"

	tele "gopkg.in/telebot.v4"
)

// A task that names its Telegram recipients pushes to each by its id, and only while the
// identity still links it (docs/superpowers/specs/2026-10-28-telegram-push-recipients-design.md,
// R3 and R5).
func TestDeliverToTelegramAccount(t *testing.T) {
	t.Parallel()
	const identityID = "11111111-1111-1111-1111-111111111111"
	first := Account{TelegramUserID: 4242, IdentityID: identityID}
	second := Account{TelegramUserID: 4343, IdentityID: identityID}
	type push func(tg *Telegram, telegramUserID int64) (bool, error)
	text := func(tg *Telegram, telegramUserID int64) (bool, error) {
		return tg.DeliverToTelegramAccount(context.Background(), identityID, telegramUserID, "done")
	}
	approval := func(tg *Telegram, telegramUserID int64) (bool, error) {
		return tg.DeliverApprovalToTelegramAccount(context.Background(), identityID, telegramUserID, "tok-9", "task-abcdef12345", "agent_job")
	}
	channel := func(bot *sendRecorder, res *stubResolver) *Telegram {
		tg := NewChannel(Deps{Offline: true})
		tg.deliverBot, tg.deliverResolver = bot, res
		return tg
	}

	for name, deliver := range map[string]push{"text": text, "approval": approval} {
		t.Run(name+": each named account gets its own push, whatever the order of the links", func(t *testing.T) {
			t.Parallel()
			for _, linked := range [][]Account{{first, second}, {second, first}} {
				for _, want := range linked {
					bot, res := &sendRecorder{}, &stubResolver{linked: linked}
					ok, err := deliver(channel(bot, res), want.TelegramUserID)
					sends := bot.recorded()
					if err != nil || !ok || len(sends) != 1 || sends[0].to != tele.ChatID(want.TelegramUserID) {
						t.Fatalf("push to %d with links %v = (%v, %v), sends %+v; want one send to that chat", want.TelegramUserID, linked, ok, err, sends)
					}
					if (name == "approval") != (sends[0].markup != nil) {
						t.Fatalf("push to %d carried markup %v, want the keyboard only on the approval", want.TelegramUserID, sends[0].markup)
					}
					if res.callCount() != 0 {
						t.Fatalf("the identity-wide lookup was asked %d times, want 0", res.callCount())
					}
				}
			}
		})

		t.Run(name+": an account the identity does not link is skipped, never replaced", func(t *testing.T) {
			t.Parallel()
			for label, telegramUserID := range map[string]int64{"unlinked": 9999, "zero": 0} {
				bot := &sendRecorder{}
				ok, err := deliver(channel(bot, &stubResolver{linked: []Account{first, second}}), telegramUserID)
				if ok || !permanent(err) || len(bot.recorded()) != 0 {
					t.Fatalf("%s = (%v, %v) with %d sends, want a permanent error and no send", label, ok, err, len(bot.recorded()))
				}
				if want := fmt.Sprintf("telegram account %d is no longer linked", telegramUserID); err.Error() != want {
					t.Fatalf("%s said %q, want %q", label, err, want)
				}
			}
		})

		t.Run(name+": a failing list is an error a retry can fix", func(t *testing.T) {
			t.Parallel()
			down := errors.New("connection refused")
			bot := &sendRecorder{}
			ok, err := deliver(channel(bot, &stubResolver{linkedErr: down}), first.TelegramUserID)
			if ok || !errors.Is(err, down) || permanent(err) || len(bot.recorded()) != 0 {
				t.Fatalf("failing list = (%v, %v), want the list's error, not permanent, and no send", ok, err)
			}
		})

		t.Run(name+": a send Telegram refuses for good is permanent, a network error is not", func(t *testing.T) {
			t.Parallel()
			for _, c := range []struct {
				err  error
				want bool
			}{
				{tele.ErrBlockedByUser, true},
				{tele.ErrChatNotFound, true},
				{tele.ErrNotStartedByUser, true},
				{tele.ErrUserIsDeactivated, true},
				{fmt.Errorf("telebot: %w", errors.New("dial tcp: i/o timeout")), false},
				{tele.NewError(429, "Too Many Requests: retry after 3"), false},
			} {
				bot := &sendRecorder{sendErr: c.err}
				ok, err := deliver(channel(bot, &stubResolver{linked: []Account{first}}), first.TelegramUserID)
				if ok || !errors.Is(err, c.err) || permanent(err) != c.want {
					t.Fatalf("send failing with %v = (%v, %v, permanent=%v), want its error and permanent=%v", c.err, ok, err, permanent(err), c.want)
				}
			}
		})

		t.Run(name+": a channel that cannot push is not mine", func(t *testing.T) {
			t.Parallel()
			res := &stubResolver{linked: []Account{first}}
			tg := NewChannel(Deps{Offline: true})
			tg.deliverResolver = res
			ok, err := deliver(tg, first.TelegramUserID)
			if ok || err != nil || res.listCalls != 0 {
				t.Fatalf("no bot = (%v, %v) after %d lists, want (false, nil) and no lookup", ok, err, res.listCalls)
			}
			bot := &sendRecorder{}
			noStore := NewChannel(Deps{Offline: true})
			noStore.deliverBot = bot
			ok, err = deliver(noStore, first.TelegramUserID)
			if ok || err != nil || len(bot.recorded()) != 0 {
				t.Fatalf("no store = (%v, %v) with %d sends, want (false, nil) and none", ok, err, len(bot.recorded()))
			}
		})
	}
}

// permanent is the question cron asks of a push error without importing this package.
func permanent(err error) bool {
	var p interface{ Permanent() bool }
	return errors.As(err, &p) && p.Permanent()
}
