package telegram

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	profileflow "github.com/chetto1983/aura/internal/onboarding"
)

// profile_onboarding_test.go covers the channel's only remaining onboarding behaviour
// (Amendment #95): the one-shot nudge toward the cockpit's seed form. The 5-question
// interview this file used to drive no longer exists. It also holds the shared
// profileflow fixtures the rest of the dispatch suite builds on.

// fakeMemoryStore is an in-memory profileflow.Store for the telegram tests: it records the
// confirmed Answers and the gate. statusErr forces the Status read-error branch; nudgeErr
// forces the bookkeeping-failed branch.
type fakeMemoryStore struct {
	mu        sync.Mutex
	states    map[string]profileflow.OnboardingState
	confirmed map[string]profileflow.Answers
	statusErr error
	nudgeErr  error
}

func newFakeMemoryStore() *fakeMemoryStore {
	return &fakeMemoryStore{
		states:    map[string]profileflow.OnboardingState{},
		confirmed: map[string]profileflow.Answers{},
	}
}

func (f *fakeMemoryStore) StoreConfirmed(_ context.Context, identityID string, a profileflow.Answers) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[identityID] = profileflow.OnboardingState{Completed: true}
	f.confirmed[identityID] = a
	return nil
}

func (f *fakeMemoryStore) StoreSkipped(_ context.Context, identityID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[identityID] = profileflow.OnboardingState{Skipped: true}
	return nil
}

func (f *fakeMemoryStore) Status(_ context.Context, identityID string) (profileflow.OnboardingState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statusErr != nil {
		return profileflow.OnboardingState{}, f.statusErr
	}
	return f.states[identityID], nil
}

func (f *fakeMemoryStore) MarkNudged(_ context.Context, identityID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nudgeErr != nil {
		return f.nudgeErr
	}
	st := f.states[identityID]
	st.Nudged = true
	f.states[identityID] = st
	return nil
}

// markCompleted seeds the completed sentinel so a test can model an already-onboarded
// identity without submitting the seed form.
func (f *fakeMemoryStore) markCompleted(identityID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[identityID] = profileflow.OnboardingState{Completed: true}
}

func (f *fakeMemoryStore) markSkipped(identityID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[identityID] = profileflow.OnboardingState{Skipped: true}
}

// TestProfileNudgeFiresOnceForAnUnseededIdentity is the case the nudge exists for: an
// identity an admin provisioned with a blank seed has NO gate, so it would otherwise link
// Telegram from the admin's QR and never learn the cockpit form exists. It must fire
// exactly once — and "once" now means once per operator, recorded in the store, not once
// per daemon lifetime.
func TestProfileNudgeFiresOnceForAnUnseededIdentity(t *testing.T) {
	t.Parallel()
	store := newFakeMemoryStore()
	p := newProfileOnboarding(store, profileAccountFake{acct: profileAccount()})

	text, ok := p.nudge(context.Background(), 42, 555)
	if !ok {
		t.Fatal("an identity with no onboarding sentinel must be nudged")
	}
	if !strings.Contains(text, "Impostazioni") {
		t.Fatalf("nudge text = %q, want a pointer at the cockpit form", text)
	}
	if strings.Contains(text, "Agent.md") {
		t.Fatalf("nudge text promises an Agent.md artifact Amendment #87 deleted: %q", text)
	}

	if _, ok := p.nudge(context.Background(), 42, 555); ok {
		t.Fatal("the nudge must be one-shot per operator")
	}
	// The record lives in the store, so a fresh channel (i.e. a restarted daemon) stays quiet.
	if _, ok := newProfileOnboarding(store, profileAccountFake{acct: profileAccount()}).
		nudge(context.Background(), 42, 555); ok {
		t.Fatal("a restarted channel must not re-nudge an operator the store says was told")
	}
}

// TestProfileNudgeSilentWhenGateIsSet proves both terminal states suppress it: an operator
// who filled OR skipped the form is never pestered.
func TestProfileNudgeSilentWhenGateIsSet(t *testing.T) {
	t.Parallel()
	for name, seed := range map[string]func(*fakeMemoryStore){
		"completed": func(s *fakeMemoryStore) { s.markCompleted(profileAccount().IdentityID) },
		"skipped":   func(s *fakeMemoryStore) { s.markSkipped(profileAccount().IdentityID) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newFakeMemoryStore()
			seed(store)
			p := newProfileOnboarding(store, profileAccountFake{acct: profileAccount()})
			if text, ok := p.nudge(context.Background(), 42, 555); ok {
				t.Fatalf("a %s identity was nudged: %q", name, text)
			}
			if text, ok := p.nudge(context.Background(), 42, 555); ok {
				t.Fatalf("a %s identity was nudged on the second message: %q", name, text)
			}
		})
	}
}

// TestProfileNudgeSilentOnDegradedDeps proves every unavailable-dependency path is a quiet
// no-op rather than an error message in the operator's chat.
func TestProfileNudgeSilentOnDegradedDeps(t *testing.T) {
	t.Parallel()
	cases := map[string]*profileOnboarding{
		"nil receiver":     nil,
		"no store":         newProfileOnboarding(nil, profileAccountFake{acct: profileAccount()}),
		"no accounts":      newProfileOnboarding(newFakeMemoryStore(), nil),
		"unlinked account": newProfileOnboarding(newFakeMemoryStore(), profileAccountFake{err: errors.New("no linked account")}),
		"status read error": newProfileOnboarding(
			&fakeMemoryStore{states: map[string]profileflow.OnboardingState{}, statusErr: errors.New("sidecar down")},
			profileAccountFake{acct: profileAccount()}),
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if text, ok := p.nudge(context.Background(), 42, 555); ok {
				t.Fatalf("%s must not nudge, got %q", name, text)
			}
		})
	}
}

// TestProfileNudgeIsPerOperatorNotPerChat: the operator has been told the form exists, and
// telling them again in a second chat is the same message twice, not a new one.
func TestProfileNudgeIsPerOperatorNotPerChat(t *testing.T) {
	t.Parallel()
	p := newProfileOnboarding(newFakeMemoryStore(), profileAccountFake{acct: profileAccount()})
	if _, ok := p.nudge(context.Background(), 42, 555); !ok {
		t.Fatal("first message must be nudged")
	}
	if _, ok := p.nudge(context.Background(), 43, 555); ok {
		t.Fatal("the same operator in another chat must not be nudged again")
	}
}

// TestProfileNudgeSurvivesFailedBookkeeping: losing the record costs a repeated nudge,
// while swallowing the message costs the operator the only pointer at the form. Send it.
func TestProfileNudgeSurvivesFailedBookkeeping(t *testing.T) {
	t.Parallel()
	store := newFakeMemoryStore()
	store.nudgeErr = errors.New("write failed")
	p := newProfileOnboarding(store, profileAccountFake{acct: profileAccount()})
	if _, ok := p.nudge(context.Background(), 42, 555); !ok {
		t.Fatal("a failed MarkNudged must not suppress the nudge itself")
	}
}

type profileAccountFake struct {
	acct Account
	err  error
}

func (f profileAccountFake) GetAccountByTelegramID(context.Context, int64) (Account, error) {
	if f.err != nil {
		return Account{}, f.err
	}
	return f.acct, nil
}

func profileAccount() Account {
	return Account{TelegramUserID: 555, IdentityID: "00000000-0000-0000-0000-000000000001", Username: "dav"}
}
