package telegram

import (
	"context"
	"fmt"
	"testing"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/runner"
)

// rlsResume puts aura.paused_states' fail-closed owner policy (migration 0089) in front of
// fakeResume: a context that does not carry the owner's identity sees no pause and resolves
// none, which is what the Runner's store does on the daemon context every handler receives.
type rlsResume struct {
	*fakeResume
	owner string
}

func (r rlsResume) PendingFor(ctx context.Context, convID string) ([]askuser.Pending, error) {
	if identityctx.IdentityID(ctx) != r.owner {
		return nil, nil
	}
	return r.fakeResume.PendingFor(ctx, convID)
}

func (r rlsResume) SubmitAnswer(ctx context.Context, token string, resp runner.ResponseInput) (runner.ResolveDirective, error) {
	if identityctx.IdentityID(ctx) != r.owner {
		return runner.ResolveDirective{}, fmt.Errorf("submit answer: get paused state %s: %w", token, askuser.ErrPauseNotFound)
	}
	return r.fakeResume.SubmitAnswer(ctx, token, resp)
}

// TestHITLAnswerResolvesUnderLinkedIdentity is the live regression of 2026-09-25: a delivery
// choice rendered in Telegram could not be answered from Telegram at all. Every tap logged
// "paused state not found or already resumed", and the typed answer fell through to a fresh
// turn that re-rendered the same, still-open question. The cockpit, which scopes its
// principal, resolved the same kind of pause in five seconds. Every way Telegram answers a
// pause must reach the Runner as the chat's linked identity.
func TestHITLAnswerResolvesUnderLinkedIdentity(t *testing.T) {
	t.Parallel()
	const token = "01a0d7a7-f8e2-7b32-a123-160a5281b40b"
	pause := askuser.Pending{
		Token: token, Kind: "choice", Question: "Come preferisci ricevere il promemoria tra 10 minuti?",
		Options: optsJSON(t, [2]string{"telegram", "telegram"}, [2]string{"whatsapp", "whatsapp"}),
	}
	for _, tc := range []struct {
		name   string
		answer func(*Telegram, tele.API, int64) error
		want   submitCall
	}{
		{
			name: "button tap",
			answer: func(tg *Telegram, bot tele.API, chatID int64) error {
				cb := &tele.Callback{Message: chatMsg(chatID), Data: callbackData(token, askuser.ActionAccept, "0")}
				return tg.onCallback(context.Background())(tele.NewContext(bot, tele.Update{Callback: cb}))
			},
			want: submitCall{token: token, action: askuser.ActionAccept, content: "telegram"},
		},
		{
			name: "typed answer",
			answer: func(tg *Telegram, bot tele.API, chatID int64) error {
				msg := chatMsg(chatID)
				msg.Text = "Telegram"
				return tg.onText(context.Background())(msgContext(bot, msg))
			},
			want: submitCall{token: token, action: askuser.ActionAccept, content: "Telegram"},
		},
		{
			name: "reply to the prompt",
			answer: func(tg *Telegram, bot tele.API, chatID int64) error {
				msg := chatMsg(chatID)
				msg.Text = "Telegram"
				msg.ReplyTo = &tele.Message{ID: 1}
				return tg.onReply(context.Background())(msgContext(bot, msg))
			},
			want: submitCall{token: token, action: askuser.ActionAccept, content: "Telegram"},
		},
		{
			name: "/cancel",
			answer: func(tg *Telegram, bot tele.API, chatID int64) error {
				msg := chatMsg(chatID)
				msg.Text = "/cancel"
				return tg.onText(context.Background())(msgContext(bot, msg))
			},
			want: submitCall{token: token, action: askuser.ActionCancel},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rs := rlsResume{fakeResume: &fakeResume{pending: []askuser.Pending{pause}}, owner: profileAccount().IdentityID}
			rt := &recordingTurn{}
			tg := dispatchChannel(t, rt, func(d *Deps) {
				d.Resume = rs
				d.Cost = &fakeCost{}
				d.Search = &fakeSearch{}
			})

			if err := tc.answer(tg, &dispatchBot{}, 91); err != nil {
				t.Fatalf("answer: %v", err)
			}
			tg.wg.Wait()

			if calls := rs.calls(); len(calls) != 1 || calls[0] != tc.want {
				t.Fatalf("submitted %+v, want exactly %+v", calls, tc.want)
			}
			if _, msgs := rt.snapshot(); len(msgs) > 0 && msgs[0] != "<resume>" {
				t.Errorf("the answer drove a fresh turn on %q instead of resolving the pause", msgs[0])
			}
		})
	}
}
