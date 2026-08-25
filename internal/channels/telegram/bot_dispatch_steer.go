// Package telegram — this file is the busy-turn STEER routing (D-03/D-04): when a
// plain-text message arrives while a turn is already live for the chat, it pushes
// the operator's raw words onto the shared internal/steer.Inbox instead of
// replying busy, and always tells the operator it redirected the running turn. It
// is a WRAPPER over that inbox: no emptiness check, no byte cap, no key
// derivation of its own — Push already classifies a refusal via its own
// sentinels, and convID(chatID) is the ONLY conversation key, the same one the
// runner drives a Telegram turn under (FA-4).
package telegram

import (
	"log/slog"

	tele "gopkg.in/telebot.v4"
)

// turnSteeredMessage is the mandatory redirect echo (D-04): the operator is
// ALWAYS told their message landed on the running turn and takes effect at the
// NEXT step — not instantly, because the current tool keeps running.
const turnSteeredMessage = "↩️ Ho girato la tua correzione al turno in corso: la userò dal prossimo passaggio."

// turnSteerRefusedMessage is sent when Push refuses the steer (queue full,
// oversize, or the inbox closed): the operator is told the redirect did NOT
// happen, never left to infer one that did not occur.
const turnSteerRefusedMessage = "⚠️ Non sono riuscita a inoltrare la correzione al turno in corso: riprova o attendi che finisca."

// steerBusyTurn pushes rawText onto the steer inbox under convID(chatID) — the
// SAME conversation key the runner drives a Telegram turn with (FA-4) — and
// renders Push's own sentinel as a chat reply. It performs no validation of its
// own: internal/steer.Inbox.Push already classifies emptiness, size and
// closed/full refusals, and this function only renders that classification.
func (t *Telegram) steerBusyTurn(c tele.Context, chatID int64, rawText string) {
	if err := t.deps.Steer.Push(convID(chatID), "telegram", rawText); err != nil {
		slog.Warn("telegram: steer push refused", "chat", chatID, "err", err)
		t.reply(c, turnSteerRefusedMessage)
		return
	}
	t.reply(c, turnSteeredMessage)
}
