package telegram

// bot_dispatch_failclosed_test.go locks the HI-03 fail-closed posture on the Telegram turn
// surface (T-36-17-01, Elevation of Privilege): an inbound update whose identity does NOT
// resolve is DROPPED (never runs as the seeded local admin), a linked private chat runs
// scoped to its OWN identity, and a non-private (group) chat is rejected up front so the
// reject-unlinked gate's sender-id key can never diverge from the turn-scope chat-id key.
// The third case reproduces the exact group-chat divergence that ran a linked non-admin's
// turn in the operator (`local`) context.

import (
	"context"
	"testing"

	tele "gopkg.in/telebot.v4"
)

// TestTelegramFailClosedUnresolvedTurnDropped is the HI-03 core proof: when
// GetAccountByTelegramID MISSES for the turn's chat id, scopeTurnToIdentity fails closed and
// startTurn DROPS the turn — the driver is never invoked, so no unscoped ctx reaches a
// downstream resolver's `local` fallback (which would run the turn as the admin identity).
func TestTelegramFailClosedUnresolvedTurnDropped(t *testing.T) {
	t.Parallel()

	recorder := &identityCapturingTurn{}
	tg := dispatchChannel(t, &recordingTurn{}, func(d *Deps) {
		// An empty id map: every GetAccountByTelegramID misses (the reject-unlinked shape).
		d.profileAccounts = multiIdentityAccountFake{ids: map[int64]string{}}
		d.Turn = recorder.driver()
	})

	bot := &dispatchBot{}
	tg.runTurn(context.Background(), msgContext(bot, chatMsg(4242)), 4242, "ciao", false)
	tg.wg.Wait()

	if got := recorder.snapshot(); len(got) != 0 {
		t.Fatalf("an unresolved turn MUST be dropped (never run as local), got %d turn(s): %+v", len(got), got)
	}
}

// TestTelegramScopeLinkedPrivateChatRunsAsIdentity proves the success path is unchanged: a
// linked private chat resolves and the turn ctx carries the account's OWN identity — never
// the empty/unscoped ctx and never the seeded local admin (…001).
func TestTelegramScopeLinkedPrivateChatRunsAsIdentity(t *testing.T) {
	t.Parallel()

	const (
		identityA  = "aaaaaaaa-0000-4000-8000-00000000000a"
		localAdmin = "00000000-0000-0000-0000-000000000001"
	)

	recorder := &identityCapturingTurn{}
	tg := dispatchChannel(t, &recordingTurn{}, func(d *Deps) {
		d.profileAccounts = multiIdentityAccountFake{ids: map[int64]string{5551: identityA}}
		d.Turn = recorder.driver()
	})

	bot := &dispatchBot{}
	tg.runTurn(context.Background(), msgContext(bot, chatMsg(5551)), 5551, "ciao", false)
	tg.wg.Wait()

	got := recorder.snapshot()
	if len(got) != 1 {
		t.Fatalf("a linked private chat must drive exactly 1 turn, got %d", len(got))
	}
	if got[0].identity != identityA {
		t.Fatalf("turn identity = %q, want %q (scoped to the linked account)", got[0].identity, identityA)
	}
	if got[0].identity == "" || got[0].identity == localAdmin {
		t.Fatalf("turn ran unscoped/as local %q — HI-03 fail-open", got[0].identity)
	}
}

// TestTelegramPrivateChatEnforcedNonPrivateRejected reproduces the exact HI-03 divergence: a
// LINKED sender (7001) posts in a GROUP chat (negative chat id -9001). The reject-unlinked
// gate keys on the sender id (linked → would pass), but the turn scope keys on the chat id
// (misses). Enforcing private-only rejects the update up front, so the two keys can never
// diverge and no unscoped turn runs.
func TestTelegramPrivateChatEnforcedNonPrivateRejected(t *testing.T) {
	t.Parallel()

	recorder := &identityCapturingTurn{}
	tg := dispatchChannel(t, &recordingTurn{}, func(d *Deps) {
		d.profileAccounts = multiIdentityAccountFake{ids: map[int64]string{
			7001: "cccccccc-0000-4000-8000-00000000000c",
		}}
		d.Turn = recorder.driver()
	})

	bot := &dispatchBot{}
	msg := &tele.Message{
		Chat:   &tele.Chat{ID: -9001, Type: tele.ChatGroup}, // non-private group chat
		Sender: &tele.User{ID: 7001},                        // a LINKED member (gate would pass on sender id)
		Text:   "ciao dal gruppo",
	}
	if err := tg.onText(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText(group): %v", err)
	}
	tg.wg.Wait()

	if got := recorder.snapshot(); len(got) != 0 {
		t.Fatalf("a non-private chat must drive NO turn, got %d", len(got))
	}
	if texts := bot.sentTexts(); len(texts) != 1 || texts[0] != activationRequiredMsg {
		t.Fatalf("a non-private chat must be rejected with the activation copy, got %v", texts)
	}
}
