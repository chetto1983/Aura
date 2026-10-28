//go:build db_integration

package telegram

import (
	"fmt"
	"testing"
)

// A push bound to a conversation names its chat: the origin conversation is the Telegram
// thread of exactly one linked account, whatever else the identity links. Each chat's push
// must reach that chat (spec 2026-10-27-telegram-account-links, D9).
func TestConversationPushReachesItsOwnChat(t *testing.T) {
	s := New(migratedPool(t))
	ctx := ownerCtx()
	identityID := userIdentity(t, s.pool, "two-phones")
	chats := []int64{9_000_020_001, 9_000_020_002}
	for _, chat := range chats {
		linkThroughOnboarding(t, s, identityID, chat)
	}

	for _, chat := range chats {
		bot := &sendRecorder{}
		tg := NewChannel(Deps{Offline: true})
		tg.deliverBot, tg.deliverResolver = bot, s
		ok, err := tg.DeliverConversation(ctx, identityID, convID(chat), "promemoria")
		if err != nil || !ok {
			t.Fatalf("push bound to chat %d = (%v, %v), want delivered", chat, ok, err)
		}
		if sends := bot.recorded(); len(sends) != 1 || sends[0].to.Recipient() != fmt.Sprint(chat) {
			t.Fatalf("push bound to chat %d went to %+v, want that chat", chat, sends)
		}
	}
}
