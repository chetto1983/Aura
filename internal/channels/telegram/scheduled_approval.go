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
// distinct from the in-turn HITL callbackUnique. callback_data is "<token>|<action>" where action
// is "approve"/"reject" (token uuid 36 + 1 + ≤7 well under Telegram's 64-byte callback_data cap).
const schedApprovalUnique = "aura_sched_approval"

const schedApprovalSep = "|"

// scheduledApprovalMarkup builds the Sì/No inline keyboard for a scheduled-task approval push,
// each button carrying the pause token + the action so the callback resolves the real pause.
func scheduledApprovalMarkup(token string) *tele.ReplyMarkup {
	mk := &tele.ReplyMarkup{}
	mk.InlineKeyboard = [][]tele.InlineButton{{
		{Unique: schedApprovalUnique, Text: "Sì", Data: token + schedApprovalSep + "approve"},
		{Unique: schedApprovalUnique, Text: "No", Data: token + schedApprovalSep + "reject"},
	}}
	return mk
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
		t.disarmCallbackKeyboard(c.Bot(), cb.Message)
		return nil
	}
}

func scheduledApprovalToast(action string) string {
	if action == askuser.ActionAccept {
		return "Approvato ✓"
	}
	return "Rifiutato"
}
