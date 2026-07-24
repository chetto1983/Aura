// Package telegram — this file is the scheduler approval-reminder surface: the inline Sì/No push
// (DeliverApproval renders it in deliver.go) and its callback handler. It is DISTINCT from the
// in-turn HITL callback (bot_dispatch.go onCallback): a scheduled-task approval is operator-origin
// with no live turn, so its button press resolves the REAL ask_user pause via SubmitAnswer WITHOUT
// driving a continuation turn (the scheduled-task ResumeHook activates/cancels the task). It is the
// on-channel HITL parity for the deterministic approval-reminder sweep (Amendment #92 revised /
// fix-plan 1.7).
package telegram

import (
	"context"
	"log/slog"
	"strings"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/askuser"
)

// schedApprovalUnique is the inline-button endpoint for the scheduler approval-reminder push,
// distinct from the in-turn HITL callbackUnique. It MUST stay short: telebot frames the wire
// callback_data as "\f<Unique>|<Data>" (processButtons, options.go) and Telegram caps THAT at
// 64 bytes. Data is "<token>|<action>" = a 36-char UUID + "|approve" (44), so only 18 bytes
// remain for "\f"+Unique+"|". The original "aura_sched_approval" (19) overflowed by one →
// BUTTON_DATA_INVALID(400) at send time (see schedCallbackData's guard + the markup test).
const schedApprovalUnique = "aura_sappr"

const schedApprovalSep = "|"

// schedCallbackTelebotFrameBytes is telebot's per-button on-wire overhead beyond Data:
// the leading "\f" (1) + Unique + the "|" (1) that joins Unique to Data.
const schedCallbackTelebotFrameBytes = len("\f") + len(schedApprovalUnique) + len(schedApprovalSep)

// scheduledApprovalMarkup builds the Sì/No inline keyboard for a scheduled-task approval push,
// each button carrying the pause token + the action so the callback resolves the real pause.
func scheduledApprovalMarkup(token string) *tele.ReplyMarkup {
	mk := &tele.ReplyMarkup{}
	mk.InlineKeyboard = [][]tele.InlineButton{{
		{Unique: schedApprovalUnique, Text: "Sì", Data: schedCallbackData(token, "approve")},
		{Unique: schedApprovalUnique, Text: "No", Data: schedCallbackData(token, "reject")},
	}}
	return mk
}

// schedCallbackData builds a scheduled-approval button's Data and guards the FULL on-wire size.
// telebot sends "\f<Unique>|<Data>" and Telegram rejects >64 bytes — so the budget is checked
// WITH the framing, not on Data alone (the exact miss that shipped BUTTON_DATA_INVALID). The
// panic is a programming-invariant guard (token is always a 36-char UUID, Unique/action are
// constants): it can never fire in production and is asserted by the markup unit test — it exists
// so lengthening the Unique or an action fails loudly in tests instead of silently at send time.
func schedCallbackData(token, action string) string {
	data := token + schedApprovalSep + action
	if schedCallbackTelebotFrameBytes+len(data) > 64 {
		panic("telegram scheduled-approval callback_data exceeds Telegram's 64-byte cap")
	}
	return data
}

// scheduledApprovalText is the bounded push copy — task kind + short id only, never the payload
// (no goal/secret leakage beyond what the origin already saw).
func scheduledApprovalText(taskID, kind string) string {
	return "🔔 Il task pianificato " + kind + " " + shortTelegramTaskID(taskID) + " richiede la tua approvazione."
}

// shortTelegramTaskID renders the first 8 chars of a task id for the bounded prompt.
func shortTelegramTaskID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// parseScheduledApproval splits "<token>|<action>" into the pause token + the askuser action.
// approve→ActionAccept, reject→ActionDecline (NEVER ActionCancel — that routes to
// cancelConversation and would abort the whole origin conversation). A malformed payload or an
// unknown action → ok=false.
func parseScheduledApproval(data string) (token, action string, ok bool) {
	parts := strings.SplitN(data, schedApprovalSep, 2)
	if len(parts) != 2 || parts[0] == "" {
		return "", "", false
	}
	switch parts[1] {
	case "approve":
		return parts[0], askuser.ActionAccept, true
	case "reject":
		return parts[0], askuser.ActionDecline, true
	default:
		return "", "", false
	}
}

// onScheduledApprovalCallback resolves a scheduler approval-reminder button press: it
// authenticates the presser (linked account), maps the button to accept/decline, and resolves the
// REAL ask_user pause via SubmitAnswer — the scheduled-task ResumeHook then activates (accept) or
// cancels (decline) the task, owner-scoped on the authenticated origin conversation. It does NOT
// drive a continuation turn. A resolve failure (already-resolved / stale token) toasts and disarms
// the keyboard rather than surfacing an error to the poller.
func (t *Telegram) onScheduledApprovalCallback(daemonCtx context.Context) tele.HandlerFunc {
	return func(c tele.Context) error {
		cb := c.Callback()
		if cb == nil || cb.Message == nil || cb.Message.Chat == nil {
			return nil
		}
		if !t.requireLinkedCallback(daemonCtx, c, cb) {
			return nil
		}
		token, action, ok := parseScheduledApproval(cb.Data)
		if !ok || t.deps.Resume == nil {
			_ = c.Respond(&tele.CallbackResponse{Text: "Non disponibile"})
			return nil
		}
		if err := newHitl(t.deps.Resume, nil).resolveScheduled(daemonCtx, token, action); err != nil {
			slog.Warn("telegram scheduled approval: resolve failed", "err", err)
			_ = c.Respond(&tele.CallbackResponse{Text: "Già gestito"})
			t.disarmCallbackKeyboard(c.Bot(), cb.Message)
			return nil
		}
		_ = c.Respond(&tele.CallbackResponse{Text: scheduledApprovalToast(action)})
		// Rewrite the prompt to the outcome (and drop the keyboard) so the backstop path gives the
		// operator VISIBLE confirmation. resolveScheduled drives no continuation turn — without this
		// the sweep-minted approval resolved silently (only a transient toast), reading as "nothing
		// happened" vs the model-relay path's "Fatto!" message.
		t.editScheduledApprovalOutcome(c.Bot(), cb.Message, action)
		return nil
	}
}

// editScheduledApprovalOutcome replaces the approval prompt with its resolved outcome and clears the
// inline keyboard in one edit. Best-effort: on failure it falls back to clearing the keyboard so a
// stale prompt is never left armed.
func (t *Telegram) editScheduledApprovalOutcome(bot tele.API, msg *tele.Message, action string) {
	if bot == nil || msg == nil {
		return
	}
	if _, err := bot.Edit(msg, scheduledApprovalResolvedText(action), &tele.ReplyMarkup{}); err != nil {
		slog.Warn("telegram scheduled approval: outcome edit failed", "err", err)
		t.disarmCallbackKeyboard(bot, msg)
	}
}

func scheduledApprovalToast(action string) string {
	if action == askuser.ActionAccept {
		return "Approvato ✓"
	}
	return "Rifiutato"
}

// scheduledApprovalResolvedText is the persistent in-chat confirmation the sweep-button press leaves
// in place of the prompt (the backstop path has no agent turn to post a "Fatto!" message).
func scheduledApprovalResolvedText(action string) string {
	if action == askuser.ActionAccept {
		return "✅ Task pianificato approvato — è attivo e partirà all'orario previsto."
	}
	return "❌ Task pianificato rifiutato — annullato."
}
