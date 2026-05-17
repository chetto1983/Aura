package telegramadapter

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/chat"
	tgtelegram "github.com/aura/aura/internal/telegram"
	tele "gopkg.in/telebot.v4"
)

func TestInbound_Channel(t *testing.T) {
	if got := (Inbound{}).Channel(); got != chat.ChannelTelegram {
		t.Fatalf("Channel = %s, want telegram", got)
	}
}

func TestInbound_NormalizeFromTeleContext(t *testing.T) {
	tb, err := tele.NewBot(tele.Settings{Token: "t", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tele.NewContext(tb, tele.Update{Message: &tele.Message{
		ID:     42,
		Sender: &tele.User{ID: 1234, LanguageCode: "it"},
		Chat:   &tele.Chat{ID: 9876},
		Text:   "hello aura",
	}})

	a := New()
	msg, err := a.Normalize(context.Background(), ctx)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if msg.Channel != chat.ChannelTelegram {
		t.Fatalf("Channel = %s", msg.Channel)
	}
	if msg.PrincipalID != "1234" {
		t.Fatalf("PrincipalID = %s, want 1234", msg.PrincipalID)
	}
	if msg.ThreadID != "9876" {
		t.Fatalf("ThreadID = %s, want 9876", msg.ThreadID)
	}
	if msg.Text != "hello aura" {
		t.Fatalf("Text = %q", msg.Text)
	}
	if msg.Locale != "it" {
		t.Fatalf("Locale = %s", msg.Locale)
	}
	if msg.Mode != chat.DeliveryModeStreaming {
		t.Fatalf("Mode = %s, want streaming", msg.Mode)
	}
	if msg.ID != "42" {
		t.Fatalf("ID = %q, want 42", msg.ID)
	}
	if _, ok := msg.ChannelData[ChannelDataKeyContext]; !ok {
		t.Fatalf("ChannelData missing tele_context key: %+v", msg.ChannelData)
	}
}

func TestInbound_NormalizeRejectsNonTeleContext(t *testing.T) {
	a := New()
	if _, err := a.Normalize(context.Background(), "not a context"); err == nil {
		t.Fatal("expected error for non-tele.Context payload")
	}
}

func TestInbound_NormalizeAskUserCallbackAsTextReply(t *testing.T) {
	tb, err := tele.NewBot(tele.Settings{Token: "t", Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tele.NewContext(tb, tele.Update{Callback: &tele.Callback{
		ID:     "cb-123",
		Unique: tgtelegram.AskUserCallbackUnique,
		Data:   "2",
		Sender: &tele.User{ID: 1234, LanguageCode: "it"},
		Message: &tele.Message{
			ID:   42,
			Chat: &tele.Chat{ID: 9876},
		},
	}})

	msg, err := New().Normalize(context.Background(), ctx)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if msg.Text != "2" {
		t.Fatalf("Text = %q, want callback data 2", msg.Text)
	}
	if msg.ID != "cb-123" {
		t.Fatalf("ID = %q, want callback id", msg.ID)
	}
	if msg.ThreadID != "9876" {
		t.Fatalf("ThreadID = %q, want callback message chat id", msg.ThreadID)
	}
}
