package telegram

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	tele "gopkg.in/telebot.v4"
)

// sendRecorder is a botSender double recording every (recipient, text) Send so the
// deliver test asserts on the RESPONSE payload (the spike ground truth: bot-sent
// messages never appear in getUpdates). sendErr forces the owns-but-failed branch.
type sendRecorder struct {
	mu      sync.Mutex
	sendErr error
	sends   []recordedSend
}

type recordedSend struct {
	to   tele.Recipient
	text string
}

func (b *sendRecorder) Send(to tele.Recipient, what any, _ ...any) (*tele.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sendErr != nil {
		return nil, b.sendErr
	}
	text, _ := what.(string)
	b.sends = append(b.sends, recordedSend{to: to, text: text})
	return &tele.Message{ID: len(b.sends)}, nil
}

func (b *sendRecorder) Edit(_ tele.Editable, _ any, _ ...any) (*tele.Message, error) {
	return &tele.Message{}, nil
}

func (b *sendRecorder) recorded() []recordedSend {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]recordedSend, len(b.sends))
	copy(out, b.sends)
	return out
}

// stubResolver stands in for *Store.GetAccountByIdentity so the deliver branches
// (found / not-found / 'local') are exercised without a DB. calls counts lookups
// so the nil-bot case can assert the resolver was never touched.
type stubResolver struct {
	mu      sync.Mutex
	acct    Account
	err     error
	byID    map[string]Account // optional per-identity override (keyed lookup)
	idMiss  error              // error returned for an unknown id when byID is set
	calls   int
	lastarg string
}

func (s *stubResolver) GetAccountByIdentity(_ context.Context, identityID string) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastarg = identityID
	if s.byID != nil {
		if a, ok := s.byID[identityID]; ok {
			return a, nil
		}
		return Account{}, s.idMiss
	}
	return s.acct, s.err
}

func (s *stubResolver) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestDeliver(t *testing.T) {
	const validUUID = "11111111-1111-1111-1111-111111111111"

	t.Run("found account sends to resolved chat", func(t *testing.T) {
		t.Parallel()
		bot := &sendRecorder{}
		res := &stubResolver{acct: Account{TelegramUserID: 4242, IdentityID: validUUID}}
		tg := NewChannel(Deps{Offline: true})
		tg.deliverBot = bot
		tg.deliverResolver = res

		ok, err := tg.Deliver(context.Background(), validUUID, "hi")
		if err != nil {
			t.Fatalf("Deliver: unexpected error: %v", err)
		}
		if !ok {
			t.Error("want delivered=true for a found account")
		}
		sends := bot.recorded()
		if len(sends) != 1 {
			t.Fatalf("Send calls = %d, want 1", len(sends))
		}
		if sends[0].to != tele.ChatID(4242) {
			t.Errorf("Send recipient = %v, want ChatID(4242)", sends[0].to)
		}
		if sends[0].text != "hi" {
			t.Errorf("Send text = %q, want %q", sends[0].text, "hi")
		}
	})

	t.Run("not my user returns false,nil without sending", func(t *testing.T) {
		t.Parallel()
		bot := &sendRecorder{}
		// Resolver wraps pgx.ErrNoRows (the not-found signal Store emits).
		res := &stubResolver{err: fmt.Errorf("get telegram account by identity: %w", pgx.ErrNoRows)}
		tg := NewChannel(Deps{Offline: true})
		tg.deliverBot = bot
		tg.deliverResolver = res

		ok, err := tg.Deliver(context.Background(), validUUID, "hi")
		if err != nil {
			t.Fatalf("Deliver: want nil error on not-my-user, got %v", err)
		}
		if ok {
			t.Error("want delivered=false for not-my-user")
		}
		if len(bot.recorded()) != 0 {
			t.Errorf("Send calls = %d, want 0 (no account → no send)", len(bot.recorded()))
		}
	})

	t.Run("send failure returns owns-but-failed error", func(t *testing.T) {
		t.Parallel()
		sendErr := errors.New("network down")
		bot := &sendRecorder{sendErr: sendErr}
		res := &stubResolver{acct: Account{TelegramUserID: 99, IdentityID: validUUID}}
		tg := NewChannel(Deps{Offline: true})
		tg.deliverBot = bot
		tg.deliverResolver = res

		ok, err := tg.Deliver(context.Background(), validUUID, "hi")
		if ok {
			t.Error("want delivered=false on send failure")
		}
		if !errors.Is(err, sendErr) {
			t.Fatalf("Deliver error = %v, want it to %%w-wrap %v", err, sendErr)
		}
	})

	t.Run("local identity is not-my-user not an error", func(t *testing.T) {
		t.Parallel()
		bot := &sendRecorder{}
		// The REAL Store.GetAccountByIdentity maps a non-UUID 'local' to wrapped
		// pgx.ErrNoRows via parseUUID; the stub reproduces that by keyed lookup.
		res := &stubResolver{
			byID:   map[string]Account{validUUID: {TelegramUserID: 7, IdentityID: validUUID}},
			idMiss: fmt.Errorf("get telegram account by identity %q: %w", "local", pgx.ErrNoRows),
		}
		tg := NewChannel(Deps{Offline: true})
		tg.deliverBot = bot
		tg.deliverResolver = res

		ok, err := tg.Deliver(context.Background(), "local", "hi")
		if err != nil {
			t.Fatalf("Deliver('local'): want nil error, got %v", err)
		}
		if ok {
			t.Error("want delivered=false for 'local' (non-UUID → not my user)")
		}
		if len(bot.recorded()) != 0 {
			t.Errorf("Send calls = %d, want 0 for 'local'", len(bot.recorded()))
		}
	})

	t.Run("nil bot returns false,nil without touching the resolver", func(t *testing.T) {
		t.Parallel()
		res := &stubResolver{acct: Account{TelegramUserID: 1, IdentityID: validUUID}}
		tg := NewChannel(Deps{Offline: true})
		// deliverBot left nil and the channel is never started → t.bot is nil too.
		tg.deliverResolver = res

		ok, err := tg.Deliver(context.Background(), validUUID, "hi")
		if err != nil {
			t.Fatalf("Deliver with nil bot: unexpected error: %v", err)
		}
		if ok {
			t.Error("want delivered=false when the bot is nil (can't push)")
		}
		if res.callCount() != 0 {
			t.Errorf("resolver calls = %d, want 0 (nil bot must short-circuit before the lookup)", res.callCount())
		}
	})
}

// TestGetAccountByIdentityLocalMapsToNotFound proves the Store boundary: a non-UUID
// identity ('local') surfaces as wrapped pgx.ErrNoRows from parseUUID, never an
// error of another kind — the single not-my-user signal Deliver branches on.
func TestGetAccountByIdentityLocalMapsToNotFound(t *testing.T) {
	t.Parallel()
	s := &Store{} // q is nil — parseUUID fails before any query is issued for 'local'.
	_, err := s.GetAccountByIdentity(context.Background(), "local")
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetAccountByIdentity('local') err = %v, want it to wrap pgx.ErrNoRows", err)
	}
}
