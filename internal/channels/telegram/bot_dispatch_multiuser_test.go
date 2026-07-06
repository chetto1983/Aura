package telegram

// bot_dispatch_multiuser_test.go proves the D-24 multi-user Telegram routing invariant:
// each Telegram user resolves to its OWN Aura identity in the turn context, and an
// unknown chat-id runs no agent at all. These are the T-36-11-S (spoofing) and
// T-36-11-I2 (cross-user context leak) mitigations from the plan-11 threat model.

import (
	"context"
	"errors"
	"iter"
	"sync"
	"testing"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/identityctx"
)

// multiIdentityAccountFake maps a telegram user/chat id to a DISTINCT Aura identity so
// the routing test can prove two Telegram users resolve to isolated identity contexts.
// An unmapped id fails closed (no linked account) — the reject-unlinked shape.
type multiIdentityAccountFake struct {
	ids map[int64]string
}

func (f multiIdentityAccountFake) GetAccountByTelegramID(_ context.Context, telegramUserID int64) (Account, error) {
	id, ok := f.ids[telegramUserID]
	if !ok {
		return Account{}, errors.New("no linked account for telegram user")
	}
	return Account{TelegramUserID: telegramUserID, IdentityID: id}, nil
}

// capturedTurn records the identity the turn context carried when the driver ran, so a
// test can assert per-user isolation (D-23/D-24).
type capturedTurn struct {
	identity string
	msg      string
}

// identityCapturingTurn is a turnDriver that records identityctx.IdentityID(ctx) for
// every turn it drives, then yields a terminal text frame so handleTurn's fanout drains
// goleak-clean (same lifecycle shape as recordingTurn).
type identityCapturingTurn struct {
	mu  sync.Mutex
	got []capturedTurn
}

func (r *identityCapturingTurn) driver() turnDriver {
	return func(ctx context.Context, _ string, userMsg *string) iter.Seq2[*agent.Event, error] {
		msg := "<resume>"
		if userMsg != nil {
			msg = *userMsg
		}
		r.mu.Lock()
		r.got = append(r.got, capturedTurn{identity: identityctx.IdentityID(ctx), msg: msg})
		r.mu.Unlock()
		return func(yield func(*agent.Event, error) bool) {
			yield(textEvent("ok"), nil)
		}
	}
}

func (r *identityCapturingTurn) snapshot() []capturedTurn {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]capturedTurn, len(r.got))
	copy(out, r.got)
	return out
}

// TestTelegramPerUserTurnScopesToOwnIdentity is the D-24 adversarial isolation proof:
// two DISTINCT Telegram users each drive a turn, and each turn context MUST carry its
// OWN identity — never the other user's, never the empty/local-admin fallback
// (T-36-11-I2 cross-user leak). startTurn.scopeTurnToIdentity is the single per-turn
// scoping point every spawn path funnels through.
func TestTelegramPerUserTurnScopesToOwnIdentity(t *testing.T) {
	t.Parallel()

	const (
		identityA  = "aaaaaaaa-0000-4000-8000-00000000000a"
		identityB  = "bbbbbbbb-0000-4000-8000-00000000000b"
		localAdmin = "00000000-0000-0000-0000-000000000001"
	)

	recorder := &identityCapturingTurn{}
	tg := dispatchChannel(t, &recordingTurn{}, func(d *Deps) {
		d.profileAccounts = multiIdentityAccountFake{ids: map[int64]string{
			5551: identityA,
			7772: identityB,
		}}
		d.Turn = recorder.driver()
	})

	bot := &dispatchBot{}
	// User A's chat drives a turn, then user B's — each joined before the next so the
	// captured order is deterministic.
	tg.runTurn(context.Background(), msgContext(bot, chatMsg(5551)), 5551, "ciao da A", false)
	tg.wg.Wait()
	tg.runTurn(context.Background(), msgContext(bot, chatMsg(7772)), 7772, "ciao da B", false)
	tg.wg.Wait()

	got := recorder.snapshot()
	if len(got) != 2 {
		t.Fatalf("want 2 turns driven, got %d (%+v)", len(got), got)
	}
	if got[0].identity != identityA {
		t.Errorf("user A turn identity = %q, want %q (per-user routing)", got[0].identity, identityA)
	}
	if got[1].identity != identityB {
		t.Errorf("user B turn identity = %q, want %q (per-user routing)", got[1].identity, identityB)
	}
	if got[0].identity == got[1].identity {
		t.Fatalf("two users shared one identity context %q — cross-user leak (T-36-11-I2)", got[0].identity)
	}
	for _, turn := range got {
		if turn.identity == "" {
			t.Errorf("turn %q ran UNSCOPED — would fall to the local admin downstream (D-24 fail-open)", turn.msg)
		}
		if turn.identity == localAdmin {
			t.Errorf("turn %q leaked to the local admin identity %q (T-36-11-I2)", turn.msg, localAdmin)
		}
	}
}

// TestTelegramUnlinkedChatDrivesNoTurnAndPromptsWebLinking proves the D-24 reject-unlinked
// gate: an unknown chat-id runs NO agent (T-36-11-S) and is pointed to the web-initiated
// linking flow (activationRequiredMsg).
func TestTelegramUnlinkedChatDrivesNoTurnAndPromptsWebLinking(t *testing.T) {
	t.Parallel()

	recorder := &identityCapturingTurn{}
	tg := dispatchChannel(t, &recordingTurn{}, func(d *Deps) {
		d.profileAccounts = profileAccountFake{err: errors.New("no linked account")}
		d.Turn = recorder.driver()
	})

	bot := &dispatchBot{}
	msg := chatMsg(9993)
	msg.Sender = &tele.User{ID: 9993}
	msg.Text = "ciao, sei sveglia?"
	if err := tg.onText(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText(unlinked): %v", err)
	}
	tg.wg.Wait()

	if got := recorder.snapshot(); len(got) != 0 {
		t.Fatalf("an unlinked chat-id must drive NO turn (no agent run), got %d", len(got))
	}
	texts := bot.sentTexts()
	if len(texts) != 1 || texts[0] != activationRequiredMsg {
		t.Fatalf("unlinked chat must be pointed to web linking, got %v", texts)
	}
}
