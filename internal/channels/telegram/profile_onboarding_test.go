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

// fakeMemoryStore is an in-memory profileflow.ProfileMemoryStore for the telegram tests:
// it records the confirmed Answers and the completed/skipped sentinel. statusErr forces
// the Status read-error branch; statusCalls proves the nudge reads the sentinel at most
// once per chat per process.
type fakeMemoryStore struct {
	mu          sync.Mutex
	states      map[string]profileflow.OnboardingState
	confirmed   map[string]profileflow.Answers
	statusErr   error
	statusCalls int
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
	f.statusCalls++
	if f.statusErr != nil {
		return profileflow.OnboardingState{}, f.statusErr
	}
	return f.states[identityID], nil
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

func (f *fakeMemoryStore) reads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statusCalls
}

// TestProfileNudgeFiresOnceForAnUnseededIdentity is the case the nudge exists for: an
// identity an admin provisioned with a blank seed has NO sentinel, so it would otherwise
// link Telegram from the admin's QR and never learn the cockpit form exists. It must fire
// exactly once, and cost exactly one memory read per chat per process.
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
		t.Fatal("the nudge must be one-shot per chat")
	}
	if got := store.reads(); got != 1 {
		t.Fatalf("status reads = %d, want 1 — the latch is checked BEFORE the memory call", got)
	}
}

// TestProfileNudgeSilentWhenSentinelExists proves both terminal states suppress it: an
// operator who filled OR skipped the form is never pestered.
func TestProfileNudgeSilentWhenSentinelExists(t *testing.T) {
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
			// A terminal sentinel latches too: nudge runs synchronously on the inbound-message
			// hot path and Status costs a whole memory-MCP session, so an already-onboarded
			// operator must not pay one per message.
			if text, ok := p.nudge(context.Background(), 42, 555); ok {
				t.Fatalf("a %s identity was nudged on the second message: %q", name, text)
			}
			if got := store.reads(); got != 1 {
				t.Fatalf("status reads = %d, want 1 — a terminal sentinel must latch", got)
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

// TestProfileNudgeLatchesPerChat proves the latch is keyed by chat, not global: a second
// chat on the same process still gets its own nudge.
func TestProfileNudgeLatchesPerChat(t *testing.T) {
	t.Parallel()
	p := newProfileOnboarding(newFakeMemoryStore(), profileAccountFake{acct: profileAccount()})
	if _, ok := p.nudge(context.Background(), 42, 555); !ok {
		t.Fatal("first chat must be nudged")
	}
	if _, ok := p.nudge(context.Background(), 43, 555); !ok {
		t.Fatal("a different chat must get its own nudge")
	}
	if _, ok := p.nudge(context.Background(), 42, 555); ok {
		t.Fatal("the first chat must stay latched")
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
