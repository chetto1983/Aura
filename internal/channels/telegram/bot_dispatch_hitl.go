// Package telegram — HITL dispatch: the channel-side resolve/render half of an
// ask_user pause. hitlHandlesText/Reply route a free-text answer to the Runner (via
// hitl.handleTextReply) when a pause is pending and surface a submit failure to the
// user; promptPendingPause renders the next FIFO pause; hitlFor builds the per-chat
// HITL surface whose resume drives the continuation turn through the channel fanout.
// Split out of bot_dispatch.go (no-god-class cap).
package telegram

import (
	"context"
	"log/slog"

	tele "gopkg.in/telebot.v4"
)

// hitlHandlesText routes a free-text message to HITL when a pause is pending for
// the chat. It returns true when HITL consumed the message (a pending answer), so
// the caller does NOT also drive an ordinary turn. With no pending pause it returns
// false (handleTextReply is a no-op), so an ordinary message is never stolen.
func (t *Telegram) hitlHandlesText(daemonCtx context.Context, c tele.Context, chatID int64, text string) bool {
	if t.deps.Resume == nil {
		return false
	}
	pending, err := t.deps.Resume.PendingFor(daemonCtx, convID(chatID))
	if err != nil || len(pending) == 0 {
		return false
	}
	resumed, serr := t.hitlFor(c, chatID).handleTextReply(daemonCtx, convID(chatID), text)
	if serr != nil {
		// The Runner submit failed: tell the user to retry instead of silently
		// swallowing the answer while the pause stays open.
		t.notifyHitlSubmitError(c, chatID)
		return true
	}
	if !resumed {
		// The answer left a further FIFO pause unresolved (remaining>0) — render it so
		// the user is prompted for the next one rather than left silently waiting.
		t.promptPendingPause(daemonCtx, t.sender(c), chatID)
	}
	return true
}

// pauseCancelledMsg confirms a /cancel resolved a pending ask_user pause (Italian —
// the output-language directive). Distinct from the turn-cancel confirmation so the
// user knows the pause (not a running turn) was annulled.
const pauseCancelledMsg = "Richiesta annullata."

// cancelPendingPause routes a /cancel through the HITL cancel path when an ask_user
// pause is pending for the chat (M-e). It mirrors the "Annulla" button:
// SubmitAnswer(…ActionCancel) resolves the paused_states row (the Runner injects the
// terminating answers + auto-resolves the whole FIFO group), then the tracked prompt
// keyboard is disarmed so it no longer invites a tap. It returns true when it handled
// a pending pause; false (with no side effects) when nothing was pending, so the
// caller falls back to the ordinary in-flight-turn cancel. The Runner stays the sole
// writer of paused_states — the channel only reads (PendingFor) and resolves
// (SubmitAnswer).
func (t *Telegram) cancelPendingPause(daemonCtx context.Context, c tele.Context, chatID int64) bool {
	if t.deps.Resume == nil {
		return false
	}
	pending, err := t.deps.Resume.PendingFor(daemonCtx, convID(chatID))
	if err != nil || len(pending) == 0 {
		return false
	}
	if _, serr := t.hitlFor(c, chatID).cancel(daemonCtx, convID(chatID), pending[0].Token); serr != nil {
		t.notifyHitlSubmitError(c, chatID)
		return true
	}
	if prompt := t.takePausePrompt(chatID); prompt != nil {
		t.disarmCallbackKeyboard(c.Bot(), prompt)
	}
	t.reply(c, pauseCancelledMsg)
	return true
}

// hitlHandlesReply routes a ForceReply answer (a reply to the pause prompt) to HITL
// the same way as hitlHandlesText, additionally marking the reply message id handled
// so OnText does not double-dispatch it.
func (t *Telegram) hitlHandlesReply(daemonCtx context.Context, c tele.Context, chatID int64, msgID int, text string) bool {
	if t.deps.Resume == nil {
		return false
	}
	pending, err := t.deps.Resume.PendingFor(daemonCtx, convID(chatID))
	if err != nil || len(pending) == 0 {
		return false
	}
	t.markHitlReplyHandled(chatID, msgID)
	resumed, serr := t.hitlFor(c, chatID).handleTextReply(daemonCtx, convID(chatID), text)
	if serr != nil {
		t.notifyHitlSubmitError(c, chatID)
		return true
	}
	if !resumed {
		t.promptPendingPause(daemonCtx, t.sender(c), chatID)
	}
	return true
}

// hitlSubmitErrorMsg is the transient notice sent when a HITL answer fails to
// register (the Runner submit errored), so the user retries instead of being left
// staring at an unanswered pause.
const hitlSubmitErrorMsg = "⚠️ Non sono riuscito a registrare la tua risposta, riprova."

// notifyHitlSubmitError tells the user their HITL answer did not register. Best-
// effort: a failed notice is logged, never fatal to the dispatch.
func (t *Telegram) notifyHitlSubmitError(c tele.Context, chatID int64) {
	if _, err := t.sender(c).Send(tele.ChatID(chatID), hitlSubmitErrorMsg); err != nil {
		slog.Warn("telegram hitl: submit-error notice send failed", "chat", chatID, "err", err)
	}
}

func (t *Telegram) markHitlReplyHandled(chatID int64, msgID int) {
	if msgID == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.hitlRepliesHandled == nil {
		t.hitlRepliesHandled = make(map[hitlReplyKey]struct{})
	}
	t.hitlRepliesHandled[hitlReplyKey{chatID: chatID, msgID: msgID}] = struct{}{}
}

func (t *Telegram) takeHitlReplyHandled(chatID int64, msgID int) bool {
	if msgID == 0 {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	key := hitlReplyKey{chatID: chatID, msgID: msgID}
	if _, ok := t.hitlRepliesHandled[key]; !ok {
		return false
	}
	delete(t.hitlRepliesHandled, key)
	return true
}

// promptPendingPause renders the FIRST unresolved pause for the chat (if any) as an
// inline keyboard / ForceReply — the channel-side RENDER half of HITL, counterpart to
// the resolve half (onCallback/onReply). handleTurn calls it after a turn drains so a
// pause that suspended the loop is shown to the user; the callback/reply legs call it
// to surface the NEXT FIFO pause when one answer still leaves others pending. A nil
// Resume seam or a cancelled ctx makes it inert; with no pending pause hitl.prompt is
// a no-op (the common, non-paused turn), so it is cheap to call unconditionally.
func (t *Telegram) promptPendingPause(ctx context.Context, bot botSender, chatID int64) {
	if t.deps.Resume == nil || ctx.Err() != nil {
		return
	}
	prompt := newHitl(t.deps.Resume, nil).prompt(ctx, bot, chatID, convID(chatID))
	t.trackPausePrompt(chatID, prompt)
}

// trackPausePrompt records the last rendered pause-prompt message for a chat so a
// later /cancel can clear its inline keyboard (M-e). A nil prompt clears the entry.
func (t *Telegram) trackPausePrompt(chatID int64, prompt *tele.Message) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pausePrompts == nil {
		t.pausePrompts = make(map[int64]*tele.Message)
	}
	if prompt == nil {
		delete(t.pausePrompts, chatID)
		return
	}
	t.pausePrompts[chatID] = prompt
}

// takePausePrompt removes and returns the tracked pause-prompt message for a chat
// (nil when none is tracked). Used by the /cancel pause path to disarm the keyboard.
func (t *Telegram) takePausePrompt(chatID int64) *tele.Message {
	t.mu.Lock()
	defer t.mu.Unlock()
	prompt := t.pausePrompts[chatID]
	delete(t.pausePrompts, chatID)
	return prompt
}

// hitlFor builds a HITL surface whose resume renders the continuation turn through
// the channel's per-turn fanout on THIS chat (so a resumed answer reaches the
// user, not just the Runner). The runner seam is Deps.Resume; the resume closure
// captures the bot + chat id available only at dispatch time.
func (t *Telegram) hitlFor(c tele.Context, chatID int64) *hitl {
	sender := t.sender(c)
	notifier, _ := sender.(botNotifier)
	resume := func(ctx context.Context, _ string) {
		// nil userMsg → a continuation turn; inboundWasVoice=false (a resume is a
		// button/text answer, never itself a voice note).
		t.startTurn(ctx, sender, notifier, tele.ChatID(chatID), chatID, nil, false,
			func() { t.sendBusy(sender, chatID) })
	}
	return newHitl(t.deps.Resume, resume)
}
