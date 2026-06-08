package telegram

import (
	"context"
	"strings"
	"testing"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/askuser"
)

// TestOnCallbackRendersNextFifoPause proves a button answer that still leaves a FIFO
// pause pending (remaining>0) renders the NEXT pause rather than leaving the user
// silently waiting — and does NOT drive a resume turn (that waits for remaining==0).
func TestOnCallbackRendersNextFifoPause(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{
		remaining: 1, // answering tok-1 still leaves one pause pending
		pending:   []askuser.Pending{{Token: "tok-2", Kind: "clarification", Question: "Come ti chiami?", ToolCallID: "tc-2"}},
	}
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) { d.Resume = rs })

	bot := &dispatchBot{}
	cb := &tele.Callback{Message: chatMsg(77), Data: callbackData("tok-1", askuser.ActionAccept, "x")}
	if err := tg.onCallback(context.Background())(tele.NewContext(bot, tele.Update{Callback: cb})); err != nil {
		t.Fatalf("onCallback: %v", err)
	}

	var rendered bool
	for _, s := range bot.sentTexts() {
		if strings.Contains(s, "Come ti chiami?") {
			rendered = true
		}
	}
	if !rendered {
		t.Errorf("a remaining>0 callback must render the next FIFO pause question, sent=%v", bot.sentTexts())
	}
	if calls, _ := rt.snapshot(); calls != 0 {
		t.Errorf("remaining>0 must NOT drive a resume turn, got %d", calls)
	}
}
