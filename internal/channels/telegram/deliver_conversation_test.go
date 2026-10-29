package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
)

// A push bound to a conversation names its chat, so it goes to the linked account whose
// Telegram thread that conversation is, among however many the identity links (spec
// 2026-10-27-telegram-account-links, D9).
func TestDeliverConversation(t *testing.T) {
	t.Parallel()
	const identityID = "11111111-1111-1111-1111-111111111111"
	first := Account{TelegramUserID: 4242, IdentityID: identityID}
	second := Account{TelegramUserID: 4343, IdentityID: identityID}
	deliver := func(res *stubResolver, conversationID string) (bool, []recordedSend, error) {
		bot := &sendRecorder{}
		tg := NewChannel(Deps{Offline: true})
		tg.deliverBot, tg.deliverResolver = bot, res
		ok, err := tg.DeliverConversation(context.Background(), identityID, conversationID, "done")
		return ok, bot.recorded(), err
	}

	t.Run("each chat's conversation reaches that chat, whatever the order", func(t *testing.T) {
		t.Parallel()
		for _, linked := range [][]Account{{first, second}, {second, first}} {
			for _, want := range linked {
				res := &stubResolver{linked: linked}
				ok, sends, err := deliver(res, convID(want.TelegramUserID))
				order := fmt.Sprintf("links listed %d then %d", linked[0].TelegramUserID, linked[1].TelegramUserID)
				if err != nil || !ok {
					t.Fatalf("push to %d, %s = (%v, %v), want delivered", want.TelegramUserID, order, ok, err)
				}
				if len(sends) != 1 || sends[0].to.Recipient() != strconv.FormatInt(want.TelegramUserID, 10) || sends[0].text != "done" {
					t.Fatalf("push to %d, %s sent %+v, want one message to that chat", want.TelegramUserID, order, sends)
				}
				if res.callCount() != 0 {
					t.Fatalf("the identity-wide lookup was asked %d times, want 0", res.callCount())
				}
			}
		}
	})

	t.Run("a conversation no linked account owns is not mine", func(t *testing.T) {
		t.Parallel()
		for name, linked := range map[string][]Account{"two links": {first, second}, "no link": nil} {
			ok, sends, err := deliver(&stubResolver{linked: linked}, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
			if err != nil || ok || len(sends) != 0 {
				t.Fatalf("%s: cockpit conversation = (%v, %v) with %d sends, want (false, nil) and none", name, ok, err, len(sends))
			}
		}
	})

	t.Run("a failing list is an error, not not-mine", func(t *testing.T) {
		t.Parallel()
		down := errors.New("connection refused")
		ok, sends, err := deliver(&stubResolver{linkedErr: down}, convID(first.TelegramUserID))
		if ok || !errors.Is(err, down) || len(sends) != 0 {
			t.Fatalf("failing list = (%v, %v) with %d sends, want (false, the list's error) and none", ok, err, len(sends))
		}
	})

	t.Run("no conversation resolves nothing", func(t *testing.T) {
		t.Parallel()
		res := &stubResolver{linked: []Account{first}}
		ok, sends, err := deliver(res, "")
		if err != nil || ok || len(sends) != 0 || res.listCalls != 0 || res.callCount() != 0 {
			t.Fatalf("empty conversation = (%v, %v), %d sends, %d lists, %d lookups; want nothing", ok, err, len(sends), res.listCalls, res.callCount())
		}
	})
}
