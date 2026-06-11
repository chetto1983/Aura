package telegram

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/conversations"
	tele "gopkg.in/telebot.v4"
)

type seenAccountFake struct {
	acct     Account
	err      error
	touchErr error

	lookups []int64
	touches []int64
}

func (f *seenAccountFake) GetAccountByTelegramID(_ context.Context, telegramUserID int64) (Account, error) {
	f.lookups = append(f.lookups, telegramUserID)
	if f.err != nil {
		return Account{}, f.err
	}
	acct := f.acct
	if acct.TelegramUserID == 0 {
		acct.TelegramUserID = telegramUserID
	}
	return acct, nil
}

func (f *seenAccountFake) TouchLastSeen(_ context.Context, telegramUserID int64) error {
	f.touches = append(f.touches, telegramUserID)
	return f.touchErr
}

type deletingDispatchBot struct {
	dispatchBot
	deletes int
}

func (b *deletingDispatchBot) Delete(_ tele.Editable) error {
	b.deletes++
	return nil
}

func callbackContext(bot tele.API, cb *tele.Callback) tele.Context {
	return tele.NewContext(bot, tele.Update{Callback: cb})
}

func callbackUpdate(chatID, senderID int64, data string) *tele.Callback {
	return &tele.Callback{
		Sender:  &tele.User{ID: senderID},
		Message: chatMsg(chatID),
		Data:    data,
	}
}

func TestTelegramAuthRejectsMissingResolverAndSender(t *testing.T) {
	t.Parallel()

	if NewChannel(Deps{}).telegramUserIsLinked(context.Background(), 555) {
		t.Fatal("missing account resolver must fail closed")
	}
	tg := NewChannel(Deps{profileAccounts: profileAccountFake{acct: profileAccount()}})
	if tg.telegramUserIsLinked(context.Background(), 0) {
		t.Fatal("missing Telegram sender must fail closed")
	}
}

func TestTelegramAuthTouchesLastSeen(t *testing.T) {
	t.Parallel()

	fake := &seenAccountFake{acct: profileAccount()}
	tg := NewChannel(Deps{profileAccounts: fake})
	if !tg.telegramUserIsLinked(context.Background(), 777) {
		t.Fatal("linked Telegram account should pass auth")
	}
	if len(fake.lookups) != 1 || fake.lookups[0] != 777 {
		t.Fatalf("lookups = %v, want [777]", fake.lookups)
	}
	if len(fake.touches) != 1 || fake.touches[0] != 777 {
		t.Fatalf("touches = %v, want [777]", fake.touches)
	}
}

func TestTelegramAuthAllowsWhenLastSeenTouchFails(t *testing.T) {
	t.Parallel()

	fake := &seenAccountFake{acct: profileAccount(), touchErr: errors.New("temporary write failure")}
	tg := NewChannel(Deps{profileAccounts: fake})
	if !tg.telegramUserIsLinked(context.Background(), 888) {
		t.Fatal("last_seen failure should not reject an otherwise linked account")
	}
}

func TestTelegramUserIDExtractors(t *testing.T) {
	t.Parallel()

	if got := telegramUserIDFromMessage(nil); got != 0 {
		t.Fatalf("nil message id = %d, want 0", got)
	}
	msg := &tele.Message{Sender: &tele.User{ID: 11}, Chat: &tele.Chat{ID: 22}}
	if got := telegramUserIDFromMessage(msg); got != 11 {
		t.Fatalf("message sender id = %d, want 11", got)
	}
	msg.Sender = nil
	if got := telegramUserIDFromMessage(msg); got != 22 {
		t.Fatalf("message chat id = %d, want 22", got)
	}
	if got := telegramUserIDFromMessage(&tele.Message{}); got != 0 {
		t.Fatalf("empty message id = %d, want 0", got)
	}

	cb := &tele.Callback{Sender: &tele.User{ID: 33}, Message: &tele.Message{Chat: &tele.Chat{ID: 44}}}
	if got := telegramUserIDFromCallback(cb); got != 33 {
		t.Fatalf("callback sender id = %d, want 33", got)
	}
	cb.Sender = nil
	if got := telegramUserIDFromCallback(cb); got != 44 {
		t.Fatalf("callback message id = %d, want 44", got)
	}
	if got := telegramUserIDFromCallback(nil); got != 0 {
		t.Fatalf("nil callback id = %d, want 0", got)
	}
}

func TestOnSearchCallbackUnlinkedRequiresActivation(t *testing.T) {
	t.Parallel()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.profileAccounts = profileAccountFake{err: errors.New("no linked account")}
	})

	bot := &dispatchBot{}
	cb := callbackUpdate(42, 777, searchCallbackData(1))
	if err := tg.onSearchCallback()(callbackContext(bot, cb)); err != nil {
		t.Fatalf("onSearchCallback: %v", err)
	}
	if got := bot.responseTexts(); len(got) != 1 || got[0] != activationRequiredToast {
		t.Fatalf("responses = %v, want activation toast", got)
	}
	if edits := bot.recordedEdits(); len(edits) != 0 {
		t.Fatalf("unlinked search callback must not edit messages, got %d edits", len(edits))
	}
}

func TestOnSearchCallbackLinkedEditsStoredPage(t *testing.T) {
	t.Parallel()

	hits := make([]conversations.SearchResult, 0, searchPageSize+1)
	for i := 0; i < searchPageSize+1; i++ {
		hits = append(hits, conversations.SearchResult{Seq: i + 1, Content: "meteo risultato " + strconv.Itoa(i+1)})
	}
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Search = &fakeSearch{results: hits}
	})

	bot := &dispatchBot{}
	msg := chatMsg(42)
	msg.Sender = &tele.User{ID: 555}
	msg.Text = "/search meteo"
	if err := tg.onText(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText(/search): %v", err)
	}

	cb := callbackUpdate(42, 555, searchCallbackData(1))
	if err := tg.onSearchCallback()(callbackContext(bot, cb)); err != nil {
		t.Fatalf("onSearchCallback: %v", err)
	}
	edits := bot.recordedEdits()
	if len(edits) != 1 {
		t.Fatalf("search page callback edits = %d, want 1", len(edits))
	}
	text, _ := edits[0].what.(string)
	if !strings.Contains(text, "meteo risultato 6") || !strings.Contains(text, "Pagina 2/2") {
		t.Fatalf("search page edit text = %q, want second page", text)
	}
	if edits[0].markup == nil {
		t.Fatal("search page edit should preserve pager markup")
	}
}

func TestOnSearchCallbackCloseDeletesPager(t *testing.T) {
	t.Parallel()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, nil)
	bot := &deletingDispatchBot{}
	cb := callbackUpdate(42, 555, searchCallbackCloseData())
	if err := tg.onSearchCallback()(callbackContext(bot, cb)); err != nil {
		t.Fatalf("onSearchCallback(close): %v", err)
	}
	if bot.deletes != 1 {
		t.Fatalf("close callback deletes = %d, want 1", bot.deletes)
	}
	if got := bot.responseTexts(); len(got) != 1 || got[0] != "Ricevuto" {
		t.Fatalf("responses = %v, want Ricevuto", got)
	}
}

func TestOnProfileCallbackUnlinkedRequiresActivation(t *testing.T) {
	t.Parallel()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.profileAccounts = profileAccountFake{err: errors.New("no linked account")}
	})

	bot := &dispatchBot{}
	cb := callbackUpdate(42, 777, profileCallbackData(42, profileActionConfirm))
	if err := tg.onProfileCallback(context.Background())(callbackContext(bot, cb)); err != nil {
		t.Fatalf("onProfileCallback: %v", err)
	}
	if got := bot.responseTexts(); len(got) != 1 || got[0] != activationRequiredToast {
		t.Fatalf("responses = %v, want activation toast", got)
	}
	if len(bot.sentTexts()) != 0 {
		t.Fatalf("unlinked profile callback must not send profile replies, got %v", bot.sentTexts())
	}
}

func TestOnProfileCallbackLinkedConfirmsOnboarding(t *testing.T) {
	t.Parallel()

	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, nil)
	bot := &dispatchBot{}

	for _, text := range []string{
		"/onboard",
		"Davide",
		"italiano Europe/Rome tono tecnico risposte brevi voce",
	} {
		msg := chatMsg(42)
		msg.Sender = &tele.User{ID: 555}
		msg.Text = text
		if err := tg.onText(context.Background())(msgContext(bot, msg)); err != nil {
			t.Fatalf("onText(%q): %v", text, err)
		}
	}

	cb := callbackUpdate(42, 555, profileCallbackData(42, profileActionConfirm))
	if err := tg.onProfileCallback(context.Background())(callbackContext(bot, cb)); err != nil {
		t.Fatalf("onProfileCallback(confirm): %v", err)
	}
	if got := bot.responseTexts(); len(got) == 0 || got[len(got)-1] != "Confermato" {
		t.Fatalf("responses = %v, want final Confermato", got)
	}
	if edits := bot.recordedEdits(); len(edits) == 0 {
		t.Fatal("profile confirm should disarm the inline keyboard")
	}
	foundSaved := false
	for _, text := range bot.sentTexts() {
		if strings.Contains(text, "Profilo salvato") {
			foundSaved = true
			break
		}
	}
	if !foundSaved {
		t.Fatalf("sent texts = %v, want saved profile confirmation", bot.sentTexts())
	}
}

func TestOnCallbackFallbackAuthGate(t *testing.T) {
	t.Parallel()

	t.Run("unlinked", func(t *testing.T) {
		t.Parallel()
		rt := &recordingTurn{}
		tg := dispatchChannel(t, rt, func(d *Deps) {
			d.profileAccounts = profileAccountFake{err: errors.New("no linked account")}
		})
		bot := &dispatchBot{}
		if err := tg.onCallbackFallback()(callbackContext(bot, callbackUpdate(42, 777, "unknown"))); err != nil {
			t.Fatalf("onCallbackFallback: %v", err)
		}
		if got := bot.responseTexts(); len(got) != 1 || got[0] != activationRequiredToast {
			t.Fatalf("responses = %v, want activation toast", got)
		}
	})

	t.Run("linked", func(t *testing.T) {
		t.Parallel()
		rt := &recordingTurn{}
		tg := dispatchChannel(t, rt, nil)
		bot := &dispatchBot{}
		if err := tg.onCallbackFallback()(callbackContext(bot, callbackUpdate(42, 555, "unknown"))); err != nil {
			t.Fatalf("onCallbackFallback: %v", err)
		}
		if got := bot.responseTexts(); len(got) != 1 || got[0] != "" {
			t.Fatalf("responses = %v, want empty ack", got)
		}
	})
}
