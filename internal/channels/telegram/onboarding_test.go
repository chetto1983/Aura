package telegram

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// fakeOnboard is the onboardingStore double. It consumes a single token ONCE
// (single-use): a first ConsumeOnboarding with the valid token returns an account;
// a replay (or any other token) returns ErrTokenConsumed and writes nothing —
// modeling the DB single-use chokepoint without a live PG (T-13-06-TokenReplay).
type fakeOnboard struct {
	mu         sync.Mutex
	validToken string
	consumed   bool
	identityID string
	inserts    int
}

func (f *fakeOnboard) ConsumeOnboarding(_ context.Context, p ConsumeParams) (Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.OnboardingToken != f.validToken || f.consumed {
		return Account{}, ErrTokenConsumed
	}
	f.consumed = true
	f.inserts++
	return Account{
		TelegramUserID: p.TelegramUserID,
		IdentityID:     f.identityID,
		Username:       p.Username,
		FirstName:      p.FirstName,
	}, nil
}

// TestOnboardValidTokenOnboards: a valid /start <token> consumes the pending row
// and writes exactly one account; the user is greeted.
func TestOnboardValidTokenOnboards(t *testing.T) {
	t.Parallel()
	store := &fakeOnboard{validToken: "tok-good", identityID: "id-1"}
	o := newOnboarding(store)

	reply, onboarded := o.handleStart(context.Background(), startMsg{
		Token: "tok-good", TelegramUserID: 555, Username: "dav", FirstName: "Davide",
	})
	if !onboarded {
		t.Fatal("a valid token must onboard")
	}
	if store.inserts != 1 {
		t.Errorf("want exactly 1 account insert, got %d", store.inserts)
	}
	if reply == "" {
		t.Error("a successful onboarding must greet the user")
	}
}

// TestOnboardReplayedTokenRejected: a second /start with the SAME (now consumed)
// token writes NO account and replies with an invalid/expired message
// (T-13-06-TokenReplay).
func TestOnboardReplayedTokenRejected(t *testing.T) {
	t.Parallel()
	store := &fakeOnboard{validToken: "tok-good", identityID: "id-1"}
	o := newOnboarding(store)

	if _, ok := o.handleStart(context.Background(), startMsg{Token: "tok-good", TelegramUserID: 1}); !ok {
		t.Fatal("first consume must succeed")
	}
	reply, onboarded := o.handleStart(context.Background(), startMsg{Token: "tok-good", TelegramUserID: 1})
	if onboarded {
		t.Fatal("a replayed token must NOT onboard")
	}
	if store.inserts != 1 {
		t.Errorf("a replay must not write a second account, inserts=%d", store.inserts)
	}
	if !mentionsInvalidLink(reply) {
		t.Errorf("a replayed token reply should mention an invalid/expired link, got %q", reply)
	}
}

// TestOnboardUnknownTokenRejected: an unknown token writes no account and is
// rejected with a clear message.
func TestOnboardUnknownTokenRejected(t *testing.T) {
	t.Parallel()
	store := &fakeOnboard{validToken: "tok-good"}
	o := newOnboarding(store)
	reply, onboarded := o.handleStart(context.Background(), startMsg{Token: "tok-nope", TelegramUserID: 9})
	if onboarded {
		t.Fatal("an unknown token must not onboard")
	}
	if store.inserts != 0 {
		t.Errorf("an unknown token must write no account, inserts=%d", store.inserts)
	}
	if !mentionsInvalidLink(reply) {
		t.Errorf("an unknown token reply should mention an invalid/expired link, got %q", reply)
	}
}

// TestOnboardEmptyTokenIsPlainStart: a bare /start (no token payload) is the
// ordinary greeting path — NOT an onboarding attempt (no store call).
func TestOnboardEmptyTokenIsPlainStart(t *testing.T) {
	t.Parallel()
	store := &fakeOnboard{validToken: "tok-good"}
	o := newOnboarding(store)
	reply, onboarded := o.handleStart(context.Background(), startMsg{Token: "", TelegramUserID: 1})
	if onboarded {
		t.Error("a bare /start must not be treated as an onboarding")
	}
	if store.inserts != 0 {
		t.Error("a bare /start must not call the store")
	}
	if reply == "" {
		t.Error("a bare /start should still greet")
	}
}

// TestParseStartPayload proves the deep-link payload is parsed from a raw /start
// message (the "/start <token>" Telegram deep-link convention).
func TestParseStartPayload(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"/start tok-123":        "tok-123",
		"/start  tok-456 ":      "tok-456",
		"/start":                "",
		"/start@aura_bot tok-7": "tok-7",
		"hello":                 "",
	}
	for in, want := range cases {
		if got := parseStartPayload(in); got != want {
			t.Errorf("parseStartPayload(%q) = %q, want %q", in, got, want)
		}
	}
}

func mentionsInvalidLink(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "scadut") || strings.Contains(l, "non valid") || strings.Contains(l, "invalid")
}
