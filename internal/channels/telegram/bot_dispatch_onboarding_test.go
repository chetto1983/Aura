package telegram

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/askuser"
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
		d.Profile = newFakeMemoryStore()
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

// TestOnTextSeedNudgeIsAdditiveNotInsteadOfTheTurn is the behaviour Amendment #95 changes:
// the interview used to SWALLOW the operator's first message to ask its first question.
// The nudge must be sent alongside the turn, never instead of it — a pointer that eats the
// message it interrupts is hostile.
func TestOnTextSeedNudgeIsAdditiveNotInsteadOfTheTurn(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
		d.Profile = newFakeMemoryStore()
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
	tg.wg.Wait()

	calls, msgs := rt.snapshot()
	if calls != 1 || len(msgs) != 1 || msgs[0] != "ciao Aura" {
		t.Fatalf("nudged turn = %d calls %v, want the operator's message still run", calls, msgs)
	}
	texts := bot.sentTexts()
	if len(texts) == 0 || !strings.Contains(texts[0], "Impostazioni") {
		t.Fatalf("first reply = %v, want the seed-form nudge", texts)
	}
}

// TestOnTextSeedNudgeSilentForAnOnboardedIdentity proves the common path costs nothing: an
// operator who already submitted (or skipped) the cockpit form sees only their turn.
func TestOnTextSeedNudgeSilentForAnOnboardedIdentity(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	store := newFakeMemoryStore()
	store.markCompleted(profileAccount().IdentityID)
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
		d.Profile = store
		d.profileAccounts = profileAccountFake{acct: profileAccount()}
	})

	bot := &dispatchBot{}
	msg := chatMsg(42)
	msg.Sender = &tele.User{ID: 555}
	msg.Text = "ciao Aura"
	if err := tg.onText(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText: %v", err)
	}
	tg.wg.Wait()

	if calls, _ := rt.snapshot(); calls != 1 {
		t.Fatalf("onboarded identity turns = %d, want 1", calls)
	}
	if texts := bot.sentTexts(); len(texts) != 0 {
		t.Fatalf("onboarded identity was nudged: %v", texts)
	}
}

// TestOnTextOnboardCommandPointsAtTheCockpitForm proves /onboard is served by the generic
// command dispatcher (its interview special case is gone) and that its copy names the
// cockpit form rather than the Agent.md artifact Amendment #87 deleted.
func TestOnTextOnboardCommandPointsAtTheCockpitForm(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	store := newFakeMemoryStore()
	store.markCompleted(profileAccount().IdentityID)
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
	if len(texts) != 1 || !strings.Contains(texts[0], "Impostazioni") {
		t.Fatalf("/onboard reply = %v, want a pointer at the cockpit profile form", texts)
	}
	if strings.Contains(texts[0], "Agent.md") {
		t.Fatalf("/onboard still promises an Agent.md artifact: %q", texts[0])
	}
}

func TestOnTextUnlinkedUserRequiresActivationNoTurn(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
		d.Profile = newFakeMemoryStore()
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
		d.Profile = newFakeMemoryStore()
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

// TestBuildDispatchWiresProfileStore guards that buildDispatch builds the profileOnboarding
// over the wired Deps.Profile, so profileForDispatch's early-return path delivers a
// store-carrying instance to production (a nil store makes the nudge silently inert).
func TestBuildDispatchWiresProfileStore(t *testing.T) {
	t.Parallel()
	tg := dispatchChannel(t, &recordingTurn{}, func(d *Deps) {
		d.Profile = newFakeMemoryStore()
		d.profileAccounts = profileAccountFake{acct: profileAccount()}
	})
	if tg.profileForDispatch().store == nil {
		t.Fatal("buildDispatch must wire Deps.Profile onto the profileOnboarding; the seed-form nudge is silently inert without it")
	}
}
