package telegram

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/askuser"
)

func TestParseScheduledApproval(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		data       string
		wantTok    string
		wantAction string
		wantOK     bool
	}{
		{"tok-1|approve", "tok-1", askuser.ActionAccept, true},
		{"tok-1|reject", "tok-1", askuser.ActionDecline, true},
		{"tok-1|cancel", "", "", false}, // cancel is NOT an allowed action (it would abort the conv)
		{"tok-1", "", "", false},        // no separator
		{"|approve", "", "", false},     // empty token
		{"", "", "", false},
	} {
		tok, action, ok := parseScheduledApproval(tc.data)
		if ok != tc.wantOK || tok != tc.wantTok || action != tc.wantAction {
			t.Errorf("parseScheduledApproval(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.data, tok, action, ok, tc.wantTok, tc.wantAction, tc.wantOK)
		}
	}
}

func TestScheduledApprovalMarkupBindsToken(t *testing.T) {
	t.Parallel()
	const token = "11111111-1111-1111-1111-111111111111"
	mk := scheduledApprovalMarkup(token)
	if len(mk.InlineKeyboard) != 1 || len(mk.InlineKeyboard[0]) != 2 {
		t.Fatalf("want one row of two buttons, got %v", mk.InlineKeyboard)
	}
	yes, no := mk.InlineKeyboard[0][0], mk.InlineKeyboard[0][1]
	if yes.Unique != schedApprovalUnique || no.Unique != schedApprovalUnique {
		t.Errorf("buttons must carry the schedApprovalUnique endpoint")
	}
	if yes.Data != token+"|approve" || no.Data != token+"|reject" {
		t.Errorf("callback data wrong: yes=%q no=%q", yes.Data, no.Data)
	}
	if len(yes.Data) > 64 || len(no.Data) > 64 { // Telegram caps callback_data at 64 bytes
		t.Errorf("callback_data exceeds 64 bytes: yes=%d no=%d", len(yes.Data), len(no.Data))
	}
}

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

func TestResolveScheduledSubmitsWithoutResume(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{remaining: 0}
	h := newHitl(rs, nil)
	if err := h.resolveScheduled(context.Background(), "tok-1", askuser.ActionAccept); err != nil {
		t.Fatalf("resolveScheduled: %v", err)
	}
	calls := rs.calls()
	if len(calls) != 1 || calls[0].token != "tok-1" || calls[0].action != askuser.ActionAccept {
		t.Fatalf("want 1 SubmitAnswer(tok-1, accept), got %+v", calls)
	}
	if rs.resumes != 0 {
		t.Errorf("resolveScheduled must NOT drive a continuation turn, got %d resumes", rs.resumes)
	}
}

// TestOnScheduledApprovalCallbackRejectSubmitsDecline proves the on-channel "No" button submits
// ActionDecline (the ResumeHook cancels the task) — never ActionCancel — and drives no turn.
func TestOnScheduledApprovalCallbackRejectSubmitsDecline(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{remaining: 0}
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) { d.Resume = rs })

	bot := &dispatchBot{}
	cb := &tele.Callback{Message: chatMsg(41), Data: "tok-1|reject"}
	if err := tg.onScheduledApprovalCallback(context.Background())(tele.NewContext(bot, tele.Update{Callback: cb})); err != nil {
		t.Fatalf("onScheduledApprovalCallback: %v", err)
	}
	calls := rs.calls()
	if len(calls) != 1 || calls[0].action != askuser.ActionDecline || calls[0].token != "tok-1" {
		t.Fatalf("reject must submit decline for tok-1, got %+v", calls)
	}
	if bot.responds != 1 {
		t.Errorf("callback must be acked once, got %d", bot.responds)
	}
	if calls2, _ := rt.snapshot(); calls2 != 0 {
		t.Errorf("scheduled approval must NOT drive a continuation turn, got %d", calls2)
	}
}

// TestOnScheduledApprovalCallbackApproveSubmitsAccept mirrors the reject test for the "Sì" button.
func TestOnScheduledApprovalCallbackApproveSubmitsAccept(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{remaining: 0}
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) { d.Resume = rs })

	bot := &dispatchBot{}
	cb := &tele.Callback{Message: chatMsg(41), Data: "tok-2|approve"}
	if err := tg.onScheduledApprovalCallback(context.Background())(tele.NewContext(bot, tele.Update{Callback: cb})); err != nil {
		t.Fatalf("onScheduledApprovalCallback: %v", err)
	}
	calls := rs.calls()
	if len(calls) != 1 || calls[0].action != askuser.ActionAccept || calls[0].token != "tok-2" {
		t.Fatalf("approve must submit accept for tok-2, got %+v", calls)
	}
}

// TestOnScheduledApprovalCallbackMalformedIsNoop proves a malformed payload acks "Non disponibile"
// and submits nothing.
func TestOnScheduledApprovalCallbackMalformedIsNoop(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{}
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) { d.Resume = rs })

	bot := &dispatchBot{}
	cb := &tele.Callback{Message: chatMsg(41), Data: "garbage-no-sep"}
	if err := tg.onScheduledApprovalCallback(context.Background())(tele.NewContext(bot, tele.Update{Callback: cb})); err != nil {
		t.Fatalf("onScheduledApprovalCallback: %v", err)
	}
	if len(rs.calls()) != 0 {
		t.Errorf("malformed payload must submit nothing, got %+v", rs.calls())
	}
}
