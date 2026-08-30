// Package telegram — this file is the media QUEUE D-05 requires: a photo, voice
// note or document arriving while a turn is live for the chat is held here and
// delivered as its own turn once the live turn ends — never dropped, and never
// silently swallowed by a /cancel. It has its own mutex (pendingMu) rather than
// reusing cmds' cancels map: cancels is cleared the instant a turn ends, and this
// slot must survive exactly that moment for the delivery check below to find it.
package telegram

import (
	"context"
	"log/slog"
	"strings"
	"time"

	tele "gopkg.in/telebot.v4"
)

// mediaQueueMaxChain bounds media-queue delivery to ONE follow-on turn per
// startTurn goroutine invocation (T-52-36, mirrors runner's own
// steerAutoDeliverMaxChain): a message queued DURING the delivered turn is left
// in the slot for the NEXT startTurn's own delivery check, never chased inside
// THIS one — unbounded recursion is impossible by construction, not merely
// unlikely.
const mediaQueueMaxChain = 1

// mediaQueueMaxPerChat is the explicit small cap on how many media messages one
// chat may hold while a turn is live (T-52-36): past it, an arriving message is
// refused with turnQueueFullMessage rather than dropped silently or queued
// without bound.
const mediaQueueMaxPerChat = 4

const (
	// turnQueuedForNextTurnMessage tells the operator their attachment was
	// accepted and will run once the current request finishes — distinct from
	// both turnSteeredMessage (a redirect, not new material) and turnBusyMessage
	// (today's drop-equivalent copy).
	turnQueuedForNextTurnMessage = "📎 Ho ricevuto l'allegato: lo elaborerò appena finisce la richiesta in corso."
	// turnQueuedNotDeliveredMessage tells the operator a queued attachment did
	// NOT run because the live turn it was waiting on was cancelled — a /cancel
	// must never silently swallow a message the bot said it had accepted
	// (T-52-35).
	turnQueuedNotDeliveredMessage = "⚠️ La richiesta in corso è stata annullata: l'allegato che avevi inviato NON è stato elaborato, rimandalo pure."
	// turnQueueFullMessage tells the operator an attachment was refused because
	// the chat already holds mediaQueueMaxPerChat pending ones.
	turnQueueFullMessage = "⚠️ Ho già troppi allegati in attesa per questa chat: aspetta che finisca la richiesta in corso, poi rimanda questo."
)

// pendingTurn holds what a queued media turn needs and nothing more: the
// ALREADY-COMPOSED text and inboundWasVoice (the echo-modality TTS-out path).
// composeTurnContext already ran on the handler goroutine while the tele.Context
// was live, so the composition is never repeated at delivery — this deliberately
// does NOT hold a tele.Context, which is recycled once the handler returns.
type pendingTurn struct {
	text            string
	inboundWasVoice bool
	arrived         time.Time
}

// enqueueBusyTurn stores a media message's already-composed text in chatID's
// pending slot and tells the operator it will run after the current request.
// Past mediaQueueMaxPerChat it refuses instead of enqueueing.
func (t *Telegram) enqueueBusyTurn(c tele.Context, chatID int64, composedText string, inboundWasVoice bool) {
	if !t.enqueuePendingTurn(chatID, composedText, inboundWasVoice) {
		t.reply(c, turnQueueFullMessage)
		return
	}
	t.reply(c, turnQueuedForNextTurnMessage)
}

// enqueuePendingTurn is the c-free core of enqueueBusyTurn: the deferred document
// turn (bot_dispatch_docwait.go) hits busy AFTER the tele.Context was recycled, so
// the notice goes out via sendPlain there. Returns false when the chat's slot is
// already at mediaQueueMaxPerChat.
func (t *Telegram) enqueuePendingTurn(chatID int64, composedText string, inboundWasVoice bool) bool {
	t.pendingMu.Lock()
	defer t.pendingMu.Unlock()
	if len(t.pendingTurns[chatID]) >= mediaQueueMaxPerChat {
		return false
	}
	if t.pendingTurns == nil {
		t.pendingTurns = make(map[int64][]pendingTurn)
	}
	t.pendingTurns[chatID] = append(t.pendingTurns[chatID], pendingTurn{
		text:            composedText,
		inboundWasVoice: inboundWasVoice,
		arrived:         time.Now(),
	})
	return true
}

// takePendingTurns atomically pops and clears chatID's pending slot. ok is false
// when nothing was queued (the common case — most turns end with nothing
// pending).
func (t *Telegram) takePendingTurns(chatID int64) (msgs []pendingTurn, ok bool) {
	t.pendingMu.Lock()
	defer t.pendingMu.Unlock()
	msgs, ok = t.pendingTurns[chatID]
	if ok {
		delete(t.pendingTurns, chatID)
	}
	return msgs, ok
}

// deliverPendingTurn runs immediately after handleTurn returns, still under
// startTurn's per-chat registration and turnCtx — BEFORE the deferred
// unregisterTurn fires — the only point that can drive a queued media turn on
// the SAME registration/scope the live turn ran on. A cancelled turnCtx (a
// /cancel) means the pending message must NOT run: the slot is cleared and the
// operator is told it did not run (T-52-35). Otherwise it drives at most
// mediaQueueMaxChain follow-on turns — one, by construction — combining every
// queued message into ONE turn's text (FIFO), never one turn per message.
func (t *Telegram) deliverPendingTurn(turnCtx context.Context, sender botSender, chatID int64) {
	if turnCtx.Err() != nil {
		if _, ok := t.takePendingTurns(chatID); ok {
			t.sendPlain(sender, chatID, turnQueuedNotDeliveredMessage)
		}
		return
	}
	for range mediaQueueMaxChain {
		msgs, ok := t.takePendingTurns(chatID)
		if !ok {
			return
		}
		text := joinQueuedTurns(msgs)
		t.handleTurn(turnCtx, sender, chatID, &text, msgs[0].inboundWasVoice)
	}
}

// joinQueuedTurns joins N queued messages' already-composed text, FIFO, into the
// SINGLE turn deliverPendingTurn drives — never N separate follow-on turns.
func joinQueuedTurns(msgs []pendingTurn) string {
	texts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		texts = append(texts, m.text)
	}
	return strings.Join(texts, "\n\n")
}

// sendPlain sends text via sender directly (no tele.Context — the caller runs
// after the handler goroutine returned and the tele.Context was recycled).
// Mirrors sendBusy's shape for a caller-supplied message.
func (t *Telegram) sendPlain(sender botSender, chatID int64, text string) {
	if sender == nil {
		return
	}
	if _, err := sender.Send(tele.ChatID(chatID), text); err != nil {
		slog.Warn("telegram: queued-turn notice send failed", "chat", chatID, "err", err)
	}
}
