package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/messagedrafts"
	"github.com/chetto1983/aura/internal/runner"
)

// Measured 2026-11-12 on the lab VM: after Approva the card lost its buttons but kept the review
// text, so nothing on screen said the message was approved for its time.
func TestAResolvedReviewIsRedrawnWithItsStatus(t *testing.T) {
	t.Parallel()
	draft := messagedrafts.Draft{
		ID: "draft-1", Status: messagedrafts.StatusPending, SendAt: time.Now().Add(time.Hour),
		Target:       messagedrafts.Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"},
		OriginalArgs: json.RawMessage(`{"action":"send_email","accountId":"acc-1","to":["someone@example.com"],"subject":"Hi","body":"Text"}`),
	}
	rs := &fakeResume{
		pending: []askuser.Pending{reviewPending(draft.ID)}, drafts: []messagedrafts.Draft{draft},
		outcome: runner.OutcomeContinue, resolvedStatus: messagedrafts.StatusApproved,
	}
	tg := dispatchChannel(t, &recordingTurn{}, func(d *Deps) { d.Resume = rs })
	bot := &dispatchBot{}
	cb := &tele.Callback{Message: chatMsg(77), Data: callbackData(reviewToken, askuser.ActionAccept, "yes")}
	if err := tg.onCallback(ownerReviewCtx())(tele.NewContext(bot, tele.Update{Callback: cb})); err != nil {
		t.Fatalf("onCallback: %v", err)
	}
	var redrawn bool
	for _, e := range bot.recordedEdits() {
		text, _ := e.what.(string)
		if text == "📨 Email da approvare\nApprovato: partirà all'ora prevista." {
			redrawn = true
			if e.markup != nil && len(e.markup.InlineKeyboard) != 0 {
				t.Fatalf("the redrawn card keeps a keyboard: %+v", e.markup.InlineKeyboard)
			}
		}
	}
	if !redrawn {
		t.Fatalf("the approved card was not redrawn with its status, edits = %+v", bot.recordedEdits())
	}
	if got := bot.responseTexts(); len(got) != 1 || got[0] != "Confermato ✓" {
		t.Fatalf("toasts = %v", got)
	}
}

// Measured 2026-11-12: an expired card kept Approva and Rifiuta, and pressing one answered
// "Confermato ✓" while nothing was resolved.
func TestADeadButtonSaysItIsNoLongerActive(t *testing.T) {
	t.Parallel()
	for name, cause := range map[string]error{"resolved": askuser.ErrPauseNotFound, "expired": askuser.ErrPauseExpired} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rs := &fakeResume{submitErr: fmt.Errorf("submit answer: %w", cause)}
			rt := &recordingTurn{}
			tg := dispatchChannel(t, rt, func(d *Deps) { d.Resume = rs })
			bot := &dispatchBot{}
			cb := &tele.Callback{Message: chatMsg(77), Data: callbackData("11112222-3333-4444-5555-000000000000", askuser.ActionAccept, "yes")}
			if err := tg.onCallback(context.Background())(tele.NewContext(bot, tele.Update{Callback: cb})); err != nil {
				t.Fatalf("onCallback: %v", err)
			}
			if got := bot.responseTexts(); len(got) != 1 || got[0] != staleCallbackToast {
				t.Fatalf("toasts = %v, want only %q", got, staleCallbackToast)
			}
			edits := bot.recordedEdits()
			if len(edits) == 0 || (edits[0].markup != nil && len(edits[0].markup.InlineKeyboard) != 0) {
				t.Fatalf("the dead keyboard was not removed, edits = %+v", edits)
			}
			if calls, _ := rt.snapshot(); calls != 0 {
				t.Fatalf("a dead button drove %d turns", calls)
			}
		})
	}
}

// A press that fails for another reason must not claim success either.
func TestAFailedPressDoesNotConfirm(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{submitErr: fmt.Errorf("submit answer: %w", context.DeadlineExceeded)}
	tg := dispatchChannel(t, &recordingTurn{}, func(d *Deps) { d.Resume = rs })
	bot := &dispatchBot{}
	cb := &tele.Callback{Message: chatMsg(77), Data: callbackData("11112222-3333-4444-5555-000000000000", askuser.ActionAccept, "yes")}
	if err := tg.onCallback(context.Background())(tele.NewContext(bot, tele.Update{Callback: cb})); err != nil {
		t.Fatalf("onCallback: %v", err)
	}
	if got := bot.responseTexts(); len(got) != 1 || strings.Contains(got[0], "Confermato") {
		t.Fatalf("toasts = %v", got)
	}
}
