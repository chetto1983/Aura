package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/askuser"
)

// sendRecorder is a botSender double recording every (recipient, text) Send so the
// deliver test asserts on the RESPONSE payload (the spike ground truth: bot-sent
// messages never appear in getUpdates). sendErr forces the owns-but-failed branch.
type sendRecorder struct {
	mu      sync.Mutex
	sendErr error
	// failAt makes the Nth send (1-based) fail with sendErr, so the chunked push path
	// can be driven to a MID-sequence failure — the case where some of the chat has
	// already received text. 0 means "never fail by position".
	failAt int
	sends  []recordedSend
}

type recordedSend struct {
	to     tele.Recipient
	text   string
	markup *tele.ReplyMarkup // nil unless the send carried SendOptions (approval keyboard)
}

func (b *sendRecorder) Send(to tele.Recipient, what any, opts ...any) (*tele.Message, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failAt > 0 {
		// Count the attempt before deciding, so failAt is the attempt's position and a
		// failure at N still records N-1 successful sends for the assertion.
		if len(b.sends)+1 == b.failAt {
			return nil, b.sendErr
		}
	} else if b.sendErr != nil {
		return nil, b.sendErr
	}
	text, _ := what.(string)
	b.sends = append(b.sends, recordedSend{to: to, text: text, markup: replyMarkupFrom(opts)})
	return &tele.Message{ID: len(b.sends)}, nil
}

// replyMarkupFrom digs the ReplyMarkup out of a telebot variadic send option so the approval
// tests can assert on the RENDERED keyboard — the callback endpoint and its on-wire size are
// only observable there (a wrong Unique 400s at Telegram, never locally).
func replyMarkupFrom(opts []any) *tele.ReplyMarkup {
	for _, o := range opts {
		switch v := o.(type) {
		case *tele.SendOptions:
			if v != nil {
				return v.ReplyMarkup
			}
		case *tele.ReplyMarkup:
			return v
		}
	}
	return nil
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

// TestDeliverChunksOverTelegramCap is fix-plan 2.2. The Bot API refuses a message over
// 4096 characters, so an unsplit push — a long scheduled agent_job summary, typically —
// was lost in FULL: the interactive renderer has always split, this path never did.
func TestDeliverChunksOverTelegramCap(t *testing.T) {
	const validUUID = "11111111-1111-1111-1111-111111111111"

	newChannel := func(bot *sendRecorder) *Telegram {
		tg := NewChannel(Deps{Offline: true})
		tg.deliverBot = bot
		tg.deliverResolver = &stubResolver{acct: Account{TelegramUserID: 4242, IdentityID: validUUID}}
		// Without this the paced loop pays the real 1s chat rate limit per chunk.
		tg.deliverSleep = func(time.Duration) {}
		return tg
	}

	t.Run("splits a long push and preserves every rune", func(t *testing.T) {
		t.Parallel()
		// Multi-byte on purpose: the cap is in RUNES, and a byte-based split would both
		// miscount the chunks and cut an accented character or an emoji in half.
		unit := "Però il riepilogo è lungo 🙂 "
		long := strings.Repeat(unit, 700)
		if len([]rune(long)) <= telegramTextCap {
			t.Fatalf("fixture must exceed the cap: %d runes", len([]rune(long)))
		}

		bot := &sendRecorder{}
		ok, err := newChannel(bot).Deliver(context.Background(), validUUID, long)
		if err != nil {
			t.Fatalf("Deliver: %v", err)
		}
		if !ok {
			t.Error("want delivered=true")
		}

		sends := bot.recorded()
		if len(sends) < 2 {
			t.Fatalf("Send calls = %d, want the text split across several", len(sends))
		}
		var joined strings.Builder
		for i, s := range sends {
			if n := len([]rune(s.text)); n > telegramTextCap {
				t.Errorf("chunk %d is %d runes, over the %d cap", i+1, n, telegramTextCap)
			}
			if s.to != tele.ChatID(4242) {
				t.Errorf("chunk %d recipient = %v, want ChatID(4242)", i+1, s.to)
			}
			joined.WriteString(s.text)
		}
		// The splitter cuts AT a separator and consumes it, so the chunks do not rejoin
		// to the original byte-for-byte. The invariant that matters is that no
		// non-whitespace rune was dropped, in order — compare with whitespace removed.
		dense := func(s string) string {
			return strings.Map(func(r rune) rune {
				if unicode.IsSpace(r) {
					return -1
				}
				return r
			}, s)
		}
		if got, want := dense(joined.String()), dense(long); got != want {
			t.Errorf("rejoined chunks lost content: %d runes vs %d", len([]rune(got)), len([]rune(want)))
		}
	})

	t.Run("stops at the first failing chunk and reports its position", func(t *testing.T) {
		t.Parallel()
		long := strings.Repeat("a", telegramTextCap*3)
		bot := &sendRecorder{failAt: 2, sendErr: errors.New("flood limit")}

		ok, err := newChannel(bot).Deliver(context.Background(), validUUID, long)
		if ok {
			t.Error("want delivered=false when a chunk fails")
		}
		if err == nil {
			t.Fatal("want an error when a chunk fails")
		}
		if !strings.Contains(err.Error(), "chunk 2/") {
			t.Errorf("error must name the failing chunk position, got %q", err)
		}
		// One send recorded: chunk 1 landed, chunk 2 failed, chunk 3 was never attempted.
		if n := len(bot.recorded()); n != 1 {
			t.Errorf("recorded sends = %d, want 1 (stop at the failure, do not push the remainder)", n)
		}
	})

	t.Run("a short push is still one unsplit send", func(t *testing.T) {
		t.Parallel()
		bot := &sendRecorder{}
		if _, err := newChannel(bot).Deliver(context.Background(), validUUID, "breve"); err != nil {
			t.Fatalf("Deliver: %v", err)
		}
		sends := bot.recorded()
		if len(sends) != 1 || sends[0].text != "breve" {
			t.Fatalf("short push = %d send(s) %+v, want exactly one unchanged send", len(sends), sends)
		}
	})
}

// TestDeliverApprovalUsesUnifiedCallback is the Phase A consolidation pin: the sweep push must
// render through the SAME endpoint as the in-turn relay (callbackUnique), not a second one, and
// the button must fit Telegram's 64-byte cap WITH telebot's "\f<Unique>|" framing — the exact
// budget the deleted 19-char aura_sched_approval endpoint blew (BUTTON_DATA_INVALID, fix-plan 1.7).
func TestDeliverApprovalUsesUnifiedCallback(t *testing.T) {
	t.Parallel()
	const validUUID = "11111111-1111-1111-1111-111111111111"
	const token = "22222222-2222-2222-2222-222222222222" // a real 36-char UUID length

	bot := &sendRecorder{}
	res := &stubResolver{acct: Account{TelegramUserID: 4242, IdentityID: validUUID}}
	tg := NewChannel(Deps{Offline: true})
	tg.deliverBot = bot
	tg.deliverResolver = res

	ok, err := tg.DeliverApproval(context.Background(), validUUID, token, "task-abcdef12345", "agent_job")
	if err != nil || !ok {
		t.Fatalf("DeliverApproval = (%v,%v), want (true,nil)", ok, err)
	}
	sends := bot.recorded()
	if len(sends) != 1 || sends[0].markup == nil {
		t.Fatalf("want 1 send carrying a keyboard, got %+v", sends)
	}
	kb := sends[0].markup.InlineKeyboard
	if len(kb) != 2 || len(kb[0]) != 2 {
		t.Fatalf("want the shared approval keyboard (Approva/Rifiuta + Dettagli), got %v", kb)
	}
	for _, row := range kb {
		for _, b := range row {
			if b.Unique != callbackUnique {
				t.Errorf("button Unique = %q, want the unified %q", b.Unique, callbackUnique)
			}
			if wire := len("\f") + len(b.Unique) + len(callbackSep) + len(b.Data); wire > callbackDataMaxBytes {
				t.Errorf("on-wire callback_data = %d bytes > %d (\\f%s|%s)", wire, callbackDataMaxBytes, b.Unique, b.Data)
			}
		}
	}
	if kb[0][0].Data != callbackData(token, askuser.ActionAccept, "yes") {
		t.Errorf("accept button data = %q", kb[0][0].Data)
	}
	if kb[0][1].Data != callbackData(token, askuser.ActionDecline, "") {
		t.Errorf("decline button data = %q", kb[0][1].Data)
	}
}

// TestDeliverApprovalTriState pins the ApprovalDeliverer contract (moved here from the deleted
// scheduled_approval_test.go — DeliverApproval itself survives the consolidation): bounded copy,
// (false,nil) for a foreign identity so the WebUI leg pulls it instead, (false,err) on a send fail.
func TestDeliverApprovalTriState(t *testing.T) {
	const validUUID = "11111111-1111-1111-1111-111111111111"

	t.Run("delivered sends bounded prompt to resolved chat", func(t *testing.T) {
		t.Parallel()
		bot := &sendRecorder{}
		res := &stubResolver{acct: Account{TelegramUserID: 4242, IdentityID: validUUID}}
		tg := NewChannel(Deps{Offline: true})
		tg.deliverBot = bot
		tg.deliverResolver = res

		ok, err := tg.DeliverApproval(context.Background(), validUUID, "tok-9", "task-abcdef12345", "agent_job")
		if err != nil || !ok {
			t.Fatalf("DeliverApproval = (%v,%v), want (true,nil)", ok, err)
		}
		sends := bot.recorded()
		if len(sends) != 1 || sends[0].to != tele.ChatID(4242) {
			t.Fatalf("want 1 send to ChatID(4242), got %v", sends)
		}
		if !strings.Contains(sends[0].text, "agent_job") || !strings.Contains(sends[0].text, "task-abc") {
			t.Errorf("prompt = %q, want kind + short id", sends[0].text)
		}
		if strings.Contains(sends[0].text, "task-abcdef12345") {
			t.Errorf("prompt must NOT carry the full task id / payload: %q", sends[0].text)
		}
	})

	t.Run("not my user returns false,nil (WebUI pulls)", func(t *testing.T) {
		t.Parallel()
		bot := &sendRecorder{}
		res := &stubResolver{err: fmt.Errorf("resolve: %w", pgx.ErrNoRows)}
		tg := NewChannel(Deps{Offline: true})
		tg.deliverBot = bot
		tg.deliverResolver = res

		ok, err := tg.DeliverApproval(context.Background(), validUUID, "tok", "task", "agent_job")
		if ok || err != nil {
			t.Fatalf("want (false,nil), got (%v,%v)", ok, err)
		}
		if len(bot.recorded()) != 0 {
			t.Errorf("no account → no send")
		}
	})

	t.Run("send failure is owns-but-failed", func(t *testing.T) {
		t.Parallel()
		bot := &sendRecorder{sendErr: errors.New("net down")}
		res := &stubResolver{acct: Account{TelegramUserID: 7, IdentityID: validUUID}}
		tg := NewChannel(Deps{Offline: true})
		tg.deliverBot = bot
		tg.deliverResolver = res

		ok, err := tg.DeliverApproval(context.Background(), validUUID, "tok", "task", "agent_job")
		if ok || err == nil {
			t.Fatalf("want (false, err), got (%v,%v)", ok, err)
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
