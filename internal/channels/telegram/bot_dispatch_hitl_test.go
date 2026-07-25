package telegram

import (
	"context"
	"strings"
	"testing"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/runner"
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

func TestOnCallbackToastsAndDisarmsPromptKeyboard(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{remaining: 0}
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) { d.Resume = rs })

	bot := &dispatchBot{}
	cb := &tele.Callback{
		Message: chatMsg(78),
		Data:    callbackData("tok-1", askuser.ActionAccept, "x"),
	}
	if err := tg.onCallback(context.Background())(tele.NewContext(bot, tele.Update{Callback: cb})); err != nil {
		t.Fatalf("onCallback: %v", err)
	}

	responses := bot.responseTexts()
	if len(responses) != 1 || !strings.Contains(responses[0], "Confermato") {
		t.Fatalf("callback must answer with a confirmation toast, got %v", responses)
	}
	edits := bot.recordedEdits()
	if len(edits) == 0 {
		t.Fatalf("callback must edit the prompt markup away after a valid tap")
	}
	if edits[0].markup != nil {
		t.Fatalf("callback must remove the inline keyboard markup, got %+v", edits[0].markup)
	}
}

// TestOnCallbackScheduledApprovalEditsOutcome replaces the deleted aura_sappr dispatch tests:
// the SAME unified callback now renders the scheduled-approval verdict as a persistent in-chat
// confirmation (a transient toast alone read as "nothing happened" — fix-plan 1.7 defect E) and
// drives no continuation turn.
func TestOnCallbackScheduledApprovalEditsOutcome(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		action  string
		outcome runner.ResolveOutcome
		want    string
	}{
		{"approved", askuser.ActionAccept, runner.OutcomeApproved, "approvato"},
		{"rejected", askuser.ActionDecline, runner.OutcomeRejected, "rifiutato"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rs := &fakeResume{outcome: tc.outcome}
			rt := &recordingTurn{}
			tg := dispatchChannel(t, rt, func(d *Deps) { d.Resume = rs })

			bot := &dispatchBot{}
			cb := &tele.Callback{Message: chatMsg(41), Data: callbackData("tok-1", tc.action, "")}
			if err := tg.onCallback(context.Background())(tele.NewContext(bot, tele.Update{Callback: cb})); err != nil {
				t.Fatalf("onCallback: %v", err)
			}
			if calls := rs.calls(); len(calls) != 1 || calls[0].action != tc.action {
				t.Fatalf("want 1 SubmitAnswer(%s), got %+v", tc.action, calls)
			}
			var outcomeEdits int
			for _, e := range bot.recordedEdits() {
				if s, ok := e.what.(string); ok && strings.Contains(strings.ToLower(s), tc.want) {
					outcomeEdits++
				}
			}
			if outcomeEdits != 1 {
				t.Errorf("want exactly one %q outcome edit, got %d (edits=%+v)", tc.want, outcomeEdits, bot.recordedEdits())
			}
			if calls, _ := rt.snapshot(); calls != 0 {
				t.Errorf("a scheduled approval must NOT drive a continuation turn, got %d", calls)
			}
		})
	}
}

// TestOnCallbackDetailsRevealsQuestionKeepingKeyboard proves the Dettagli tap edits the prompt to
// the pause's full question and leaves the keyboard armed — the operator reads, then decides —
// without resolving anything.
func TestOnCallbackDetailsRevealsQuestionKeepingKeyboard(t *testing.T) {
	t.Parallel()
	const question = "Attivo il job giornaliero delle 7:00 che invia il report?"
	rs := &fakeResume{pending: []askuser.Pending{{Token: "tok-1", Kind: "approval", Question: question}}}
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) { d.Resume = rs })

	bot := &dispatchBot{}
	cb := &tele.Callback{Message: chatMsg(41), Data: callbackData("tok-1", actionDetails, "")}
	if err := tg.onCallback(context.Background())(tele.NewContext(bot, tele.Update{Callback: cb})); err != nil {
		t.Fatalf("onCallback: %v", err)
	}
	if calls := rs.calls(); len(calls) != 0 {
		t.Fatalf("details must submit nothing, got %+v", calls)
	}
	edits := bot.recordedEdits()
	if len(edits) != 1 {
		t.Fatalf("want exactly one reveal edit, got %d (%+v)", len(edits), edits)
	}
	if s, _ := edits[0].what.(string); s != question {
		t.Errorf("reveal text = %q, want the pause question", s)
	}
	// Decision buttons stay armed; Dettagli is consumed — the question is on screen now, so a
	// second tap would re-edit identical content (a 400 from Telegram, seen live 2026-07-25).
	if edits[0].markup == nil || len(edits[0].markup.InlineKeyboard) != 1 {
		t.Errorf("reveal must re-arm with the decision row only, got %+v", edits[0].markup)
	}
	if calls, _ := rt.snapshot(); calls != 0 {
		t.Errorf("details must not drive a turn, got %d", calls)
	}
}

// TestOnCallbackDetailsOnAlreadyShownQuestionDoesNotEdit is the live-found regression: the in-turn
// relay renders the pause question as the message text, so a Dettagli tap there would edit the
// message to exactly what it already says. Telegram rejects that with
// "message is not modified (400)" — the guard must skip the edit instead of provoking it.
func TestOnCallbackDetailsOnAlreadyShownQuestionDoesNotEdit(t *testing.T) {
	t.Parallel()
	const question = "Attivo il job giornaliero delle 7:00 che invia il report?"
	rs := &fakeResume{pending: []askuser.Pending{{Token: "tok-1", Kind: "approval", Question: question}}}
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) { d.Resume = rs })

	bot := &dispatchBot{}
	msg := chatMsg(41)
	msg.Text = question // the relay prompt already shows it
	cb := &tele.Callback{Message: msg, Data: callbackData("tok-1", actionDetails, "")}
	if err := tg.onCallback(context.Background())(tele.NewContext(bot, tele.Update{Callback: cb})); err != nil {
		t.Fatalf("onCallback: %v", err)
	}
	if edits := bot.recordedEdits(); len(edits) != 0 {
		t.Errorf("must not edit a message that already shows the question, got %+v", edits)
	}
	if len(rs.calls()) != 0 {
		t.Errorf("details must submit nothing, got %+v", rs.calls())
	}
}

// TestOnCallbackResumeHonorsBusyGate proves a HITL resume continuation cannot
// bypass the same per-chat busy gate used by ordinary inbound turns.
func TestOnCallbackResumeHonorsBusyGate(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{remaining: 0}
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) { d.Resume = rs })

	const chatID int64 = 80
	if !tg.cmds.registerTurn(chatID, func() {}) {
		t.Fatal("failed to pre-register busy turn")
	}
	defer tg.cmds.unregisterTurn(chatID)

	bot := &dispatchBot{}
	cb := &tele.Callback{
		Message: chatMsg(chatID),
		Data:    callbackData("tok-1", askuser.ActionAccept, "x"),
	}
	if err := tg.onCallback(context.Background())(tele.NewContext(bot, tele.Update{Callback: cb})); err != nil {
		t.Fatalf("onCallback: %v", err)
	}
	tg.wg.Wait()

	if calls, _ := rt.snapshot(); calls != 0 {
		t.Fatalf("HITL resume must not bypass busy gate, got %d turn calls", calls)
	}
	var sawBusy bool
	for _, text := range bot.sentTexts() {
		if strings.Contains(text, turnBusyMessage) {
			sawBusy = true
		}
	}
	if !sawBusy {
		t.Fatalf("busy HITL resume must send busy copy, sent=%v", bot.sentTexts())
	}
}

// TestCancelDuringPendingPauseRoutesToHITLCancel proves /cancel during a pending
// ask_user pause routes through SubmitAnswer(…ActionCancel) — resolving the
// paused_states row via the Resume dep — AND clears the rendered prompt's inline
// keyboard (M-e). BEFORE the fix /cancel was intercepted as a plain command that
// only no-op'd the (absent) in-flight turn ctx, orphaning the pause + leaving the
// keyboard live, divergent from the "Annulla" button.
func TestCancelDuringPendingPauseRoutesToHITLCancel(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{
		pending: []askuser.Pending{{Token: "tok-p", Kind: "approval", Question: "Procedo?"}},
	}
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Resume = rs
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
	})

	const chatID int64 = 71
	bot := &dispatchBot{}
	// Render the pause prompt first so the channel tracks its message for the
	// keyboard-disarm (the live flow renders a prompt before the user types /cancel).
	tg.promptPendingPause(context.Background(), tg.sender(msgContext(bot, chatMsg(chatID))), chatID)

	msg := chatMsg(chatID)
	msg.Text = "/cancel"
	if err := tg.onText(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText(/cancel during pause): %v", err)
	}

	calls := rs.calls()
	if len(calls) != 1 {
		t.Fatalf("/cancel during a pause must submit exactly one cancel, got %d", len(calls))
	}
	if calls[0].token != "tok-p" || calls[0].action != askuser.ActionCancel {
		t.Errorf("submit = %+v, want {tok-p cancel}", calls[0])
	}
	// The continuation turn must NOT fire (a cancel terminates the turn).
	if c, _ := rt.snapshot(); c != 0 {
		t.Errorf("a pause-cancel must NOT drive a continuation turn, got %d", c)
	}
	// The tracked prompt's inline keyboard must be cleared (EditReplyMarkup nil).
	var clearedKeyboard bool
	for _, e := range bot.recordedEdits() {
		if e.markup == nil {
			clearedKeyboard = true
		}
	}
	if !clearedKeyboard {
		t.Errorf("/cancel must clear the pause prompt's inline keyboard, edits=%+v", bot.recordedEdits())
	}
	// And a user-facing cancel confirmation is sent (never silent).
	var confirmed bool
	for _, s := range bot.sentTexts() {
		if strings.Contains(s, pauseCancelledMsg) {
			confirmed = true
		}
	}
	if !confirmed {
		t.Errorf("/cancel must confirm the pause was annulled, sent=%v", bot.sentTexts())
	}
}

// TestCancelWithNoPendingPausePreservesTurnCancel proves /cancel with NO pending
// pause keeps the existing in-flight-turn ctx-cancel behavior (M-e must not regress
// the turn-cancel): no SubmitAnswer is called and the registered turn cancel fires.
func TestCancelWithNoPendingPausePreservesTurnCancel(t *testing.T) {
	t.Parallel()
	rs := &fakeResume{} // no pending pause
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, func(d *Deps) {
		d.Resume = rs
		d.Cost = &fakeCost{}
		d.Search = &fakeSearch{}
	})

	const chatID int64 = 72
	var turnCancelled bool
	if !tg.cmds.registerTurn(chatID, func() { turnCancelled = true }) {
		t.Fatal("failed to register the in-flight turn")
	}

	bot := &dispatchBot{}
	msg := chatMsg(chatID)
	msg.Text = "/cancel"
	if err := tg.onText(context.Background())(msgContext(bot, msg)); err != nil {
		t.Fatalf("onText(/cancel no pause): %v", err)
	}

	if len(rs.calls()) != 0 {
		t.Errorf("with no pending pause /cancel must NOT submit a HITL cancel, got %d", len(rs.calls()))
	}
	if !turnCancelled {
		t.Error("with no pending pause /cancel must still fire the in-flight turn cancel")
	}
	var confirmed bool
	for _, s := range bot.sentTexts() {
		if strings.Contains(s, "annullato") {
			confirmed = true
		}
	}
	if !confirmed {
		t.Errorf("/cancel must confirm the turn was annulled, sent=%v", bot.sentTexts())
	}
}

func TestOnStatusCancelCallbackCancelsTurnAndDisarmsButton(t *testing.T) {
	t.Parallel()
	rt := &recordingTurn{}
	tg := dispatchChannel(t, rt, nil)
	var canceled bool
	if !tg.cmds.registerTurn(79, func() { canceled = true }) {
		t.Fatal("failed to register test turn")
	}

	bot := &dispatchBot{}
	cb := &tele.Callback{
		Message: chatMsg(79),
		Data:    statusCancelData,
	}
	if err := tg.onStatusCancelCallback()(tele.NewContext(bot, tele.Update{Callback: cb})); err != nil {
		t.Fatalf("onStatusCancelCallback: %v", err)
	}
	if !canceled {
		t.Fatal("status cancel callback must fire the registered turn cancel")
	}
	responses := bot.responseTexts()
	if len(responses) != 1 || !strings.Contains(responses[0], "annullato") {
		t.Fatalf("status cancel callback must toast the cancel result, got %v", responses)
	}
	edits := bot.recordedEdits()
	if len(edits) == 0 || edits[0].markup != nil {
		t.Fatalf("status cancel callback must remove the inline button markup, edits=%+v", edits)
	}
}
