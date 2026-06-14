package telegram

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/profile"
)

// TestOnTextStartPayloadRoutesToOnboarding proves a Telegram deep link
// (/start <token>) is routed to the onboarding handler before the generic command
// dispatcher. Without this, the setup deep link silently falls through to the
// ordinary /start greeting and the token is never consumed.
func TestOnTextStartPayloadRoutesToOnboarding(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
		d.Profile = profile.NewStore(t.TempDir())
		d.profileAccounts = profileAccountFake{acct: profileAccount()}
	})

	bot := &dispatchBot{}
	msg := chatMsg(42)
	msg.Sender = &tele.User{ID: 555, Username: "dav", FirstName: "Davide"}
	msg.Text = "/start tok-good"
	if err := tg.onText(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText(/start token): %v", err)
	}

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Errorf("a /start onboarding payload must not drive a turn, got %d calls", calls)
	}
	texts := bot.sentTexts()
	if len(texts) != 1 {
		t.Fatalf("expected one onboarding reply, got %d: %v", len(texts), texts)
	}
	if !strings.Contains(texts[0], "Onboarding non disponibile") {
		t.Fatalf("/start token was not routed to onboarding, reply: %q", texts[0])
	}
}

func TestOnTextStartPayloadActivatesSenderThenAllowsChat(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	accounts := newActivationAccountStore()
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.profileAccounts = accounts
	})
	tg.onboard = newOnboarding(accounts)

	bot := &dispatchBot{}
	handle := tg.onText(context.Background())
	start := chatMsg(888)
	start.Sender = &tele.User{ID: 888, Username: "new_user", FirstName: "Nuova"}
	start.Text = "/start tok-good"
	if err := handle(msgContext(bot, start)); err != nil {
		t.Fatalf("onText(/start token): %v", err)
	}
	if calls, _ := rt.snapshot(); calls != 0 {
		t.Fatalf("activation must not drive a turn, got %d calls", calls)
	}
	texts := bot.sentTexts()
	if len(texts) != 1 || !strings.Contains(texts[0], "Attivazione completata") {
		t.Fatalf("activation reply = %v, want completed activation", texts)
	}

	msg := chatMsg(888)
	msg.Sender = &tele.User{ID: 888, Username: "new_user", FirstName: "Nuova"}
	msg.Text = "ciao Aura"
	if err := handle(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText(after activation): %v", err)
	}
	tg.wg.Wait()

	calls, msgs := rt.snapshot()
	if calls != 1 {
		t.Fatalf("activated sender must drive one turn, got %d calls", calls)
	}
	if len(msgs) != 1 || msgs[0] != "ciao Aura" {
		t.Fatalf("turn userMsg = %v, want [ciao Aura]", msgs)
	}
}

func TestOnTextFirstLinkedNoProfileStartsProfileOnboardingNoTurn(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
		d.Profile = profile.NewStore(t.TempDir())
		d.profileAccounts = profileAccountFake{acct: profileAccount()}
	})

	bot := &dispatchBot{}
	handle := tg.onText(context.Background())
	msg := chatMsg(42)
	msg.Sender = &tele.User{ID: 555, Username: "dav", FirstName: "Davide"}
	msg.Text = "ciao Aura"
	if err := handle(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText(first linked message): %v", err)
	}

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Fatalf("first profile onboarding message must not drive a turn, got %d calls", calls)
	}
	texts := bot.sentTexts()
	if len(texts) != 1 || !strings.Contains(texts[0], "chiami") {
		t.Fatalf("first profile onboarding reply = %v, want identity question", texts)
	}
}

func TestOnTextActiveProfileReplyDoesNotDriveTurn(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
		d.Profile = profile.NewStore(t.TempDir())
		d.profileAccounts = profileAccountFake{acct: profileAccount()}
	})

	bot := &dispatchBot{}
	handle := tg.onText(context.Background())
	first := chatMsg(42)
	first.Sender = &tele.User{ID: 555}
	first.Text = "ciao Aura"
	if err := handle(msgContext(bot, first)); err != nil {
		t.Fatalf("onText(first): %v", err)
	}
	answer := chatMsg(42)
	answer.Sender = &tele.User{ID: 555}
	answer.Text = "Davide"
	if err := handle(msgContext(bot, answer)); err != nil {
		t.Fatalf("onText(profile answer): %v", err)
	}

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Fatalf("active profile onboarding reply must not drive a turn, got %d calls", calls)
	}
	texts := bot.sentTexts()
	if len(texts) != 2 || !strings.Contains(texts[1], "competenze") {
		t.Fatalf("active profile onboarding replies = %v, want work question second", texts)
	}
}

func TestOnTextOnboardRestartsProfileOnboardingNoTurn(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	store := profile.NewStore(t.TempDir())
	if err := store.WriteProfile(profileAccount().IdentityID, profile.Profile{
		AgentMD: "old profile",
		Metadata: profile.Metadata{
			Version:             1,
			SchemaVersion:       1,
			OnboardingCompleted: true,
		},
	}); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
		d.Profile = store
		d.profileAccounts = profileAccountFake{acct: profileAccount()}
	})

	bot := &dispatchBot{}
	msg := chatMsg(42)
	msg.Sender = &tele.User{ID: 555}
	msg.Text = "/onboard"
	if err := tg.onText(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText(/onboard): %v", err)
	}

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Fatalf("/onboard must not drive a turn, got %d calls", calls)
	}
	texts := bot.sentTexts()
	if len(texts) != 1 || !strings.Contains(texts[0], "chiami") {
		t.Fatalf("/onboard reply = %v, want restarted identity prompt", texts)
	}
}

func TestOnTextUnlinkedUserRequiresActivationNoTurn(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
		d.Profile = profile.NewStore(t.TempDir())
		d.profileAccounts = profileAccountFake{err: errors.New("no linked account")}
	})

	bot := &dispatchBot{}
	msg := chatMsg(7)
	msg.Sender = &tele.User{ID: 777}
	msg.Text = "ciao Aura"
	if err := tg.onText(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText(unlinked): %v", err)
	}
	tg.wg.Wait()

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Fatalf("unlinked Telegram users must not drive a turn, got %d calls", calls)
	}
	texts := bot.sentTexts()
	if len(texts) != 1 || !strings.Contains(strings.ToLower(texts[0]), "attivazione") {
		t.Fatalf("unlinked reply = %v, want activation-required copy", texts)
	}
}

func TestOnReplyUnlinkedUserDoesNotDoubleActivationOnText(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{
		remaining: 0,
		pending:   []askuser.Pending{{Token: "tok-c", Kind: "clarification", Question: "Nome?"}},
	}
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Resume = rs
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
		d.Profile = profile.NewStore(t.TempDir())
		d.profileAccounts = profileAccountFake{err: errors.New("no linked account")}
	})

	bot := &dispatchBot{}
	msg := chatMsg(51)
	msg.ID = 99
	msg.Sender = &tele.User{ID: 777}
	msg.Text = "Davide"
	msg.ReplyTo = &tele.Message{ID: 1}
	if err := tg.onReply(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onReply(unlinked): %v", err)
	}
	if err := tg.onText(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText(after unlinked OnReply): %v", err)
	}
	tg.wg.Wait()

	if len(rs.calls()) != 0 {
		t.Fatalf("unlinked ForceReply must not submit HITL answers, got %d submits", len(rs.calls()))
	}
	if calls, _ := rt.snapshot(); calls != 0 {
		t.Fatalf("unlinked ForceReply must not drive turns, got %d calls", calls)
	}
	texts := bot.sentTexts()
	if len(texts) != 1 || !strings.Contains(strings.ToLower(texts[0]), "attivazione") {
		t.Fatalf("unlinked ForceReply replies = %v, want one activation-required copy", texts)
	}
}

type activationAccountStore struct {
	mu       sync.Mutex
	accounts map[int64]Account
}

func newActivationAccountStore() *activationAccountStore {
	return &activationAccountStore{accounts: make(map[int64]Account)}
}

func (s *activationAccountStore) ConsumeOnboarding(_ context.Context, p ConsumeParams) (Account, error) {
	if p.OnboardingToken != "tok-good" {
		return Account{}, ErrTokenConsumed
	}
	acct := Account{
		TelegramUserID: p.TelegramUserID,
		IdentityID:     profileAccount().IdentityID,
		Username:       p.Username,
		FirstName:      p.FirstName,
	}
	s.mu.Lock()
	s.accounts[p.TelegramUserID] = acct
	s.mu.Unlock()
	return acct, nil
}

func (s *activationAccountStore) GetAccountByTelegramID(_ context.Context, telegramUserID int64) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acct, ok := s.accounts[telegramUserID]
	if !ok {
		return Account{}, errors.New("no linked account")
	}
	return acct, nil
}
