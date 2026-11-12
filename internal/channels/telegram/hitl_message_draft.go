package telegram

// hitl_message_draft.go renders a message review (a host-authored pause over a draft to someone
// else) as a read-only card, and resolves it through the Runner's draft review, never through
// SubmitAnswer, which refuses review pauses (decision 13 of
// docs/superpowers/plans/2026-11-05-scheduled-delivery-defaults-and-messages.md). The text is
// edited only in the cockpit.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/messagedrafts"
)

// reviewTextMaxRunes keeps the card under Telegram's 4096-character message cap with room for
// the lines around the text.
const reviewTextMaxRunes = 3000

const reviewTextCut = "… (testo completo nel cockpit)"

// reviewStatusText is a resolved card's one line.
var reviewStatusText = map[messagedrafts.Status]string{
	messagedrafts.StatusApproved:    "Approvato: partirà all'ora prevista.",
	messagedrafts.StatusDeclined:    "Rifiutato: non parte nulla.",
	messagedrafts.StatusExpired:     "Scaduto: non è partito nulla.",
	messagedrafts.StatusSent:        "Inviato.",
	messagedrafts.StatusFailed:      "Non inviato.",
	messagedrafts.StatusUncertain:   "Consegna incerta: non verrà ritentato.",
	messagedrafts.StatusDispatching: "Invio in corso…",
}

// messageReviewText is the card: who, when (a scheduled draft only), and the text, or one status
// line once the review is no longer pending.
func messageReviewText(d messagedrafts.Draft, loc *time.Location) string {
	email := d.Target.Recipe == "recipe:calendar"
	title := "📨 Messaggio WhatsApp da approvare"
	if email {
		title = "📨 Email da approvare"
	}
	if d.Status != messagedrafts.StatusPending {
		return title + "\n" + reviewStatusText[d.Status]
	}
	recipient, subject, text := d.Review()
	var b strings.Builder
	b.WriteString(title + "\n")
	if name, address := d.Sender(); address != "" {
		fmt.Fprintf(&b, "Da: %s <%s>\n", name, address)
	} else if name != "" {
		fmt.Fprintf(&b, "Da: %s\n", name)
	}
	fmt.Fprintf(&b, "A: %s\n", recipient)
	if email {
		fmt.Fprintf(&b, "Oggetto: %s\n", subject)
	}
	if d.Scheduled() {
		if loc == nil {
			loc = time.UTC
		}
		fmt.Fprintf(&b, "Quando: %s (%s)\n", d.SendAt.In(loc).Format("02/01 15:04"), loc.String())
	}
	b.WriteString("Testo:\n")
	if runes := []rune(text); len(runes) > reviewTextMaxRunes {
		text = string(runes[:reviewTextMaxRunes]) + reviewTextCut
	}
	b.WriteString(text)
	b.WriteString("\n\nServe il tuo ok perché il messaggio va a un'altra persona. Senza risposta non parte nulla.\n")
	b.WriteString("Per cambiare il testo apri questa conversazione nel cockpit.")
	return b.String()
}

// messageReviewMarkup is Approva/Rifiuta for a pending draft, else one Chiudi that closes the
// review the way its status allows: decline for one that was never sent, accept otherwise.
func messageReviewMarkup(token string, d messagedrafts.Draft) *tele.ReplyMarkup {
	if d.Status == messagedrafts.StatusPending {
		return approvalMarkup(token)
	}
	action := askuser.ActionAccept
	if d.Status == messagedrafts.StatusDeclined || d.Status == messagedrafts.StatusExpired {
		action = askuser.ActionDecline
	}
	mk := &tele.ReplyMarkup{}
	mk.InlineKeyboard = [][]tele.InlineButton{{{Unique: callbackUnique, Text: "Chiudi", Data: callbackData(token, action, "")}}}
	return mk
}

// reviewDraft is the draft a review pause stands for, among this conversation's reviews.
func (h *hitl) reviewDraft(ctx context.Context, convID string, p askuser.Pending) (messagedrafts.Draft, bool) {
	id, ok := messagedrafts.PauseDraftID(p.ResumeContext)
	if !ok {
		return messagedrafts.Draft{}, false
	}
	drafts, err := h.runner.ListMessageDrafts(ctx, convID)
	if err != nil {
		slog.Warn("telegram review: drafts lookup failed", "conv", convID, "err", err)
		return messagedrafts.Draft{}, false
	}
	for _, d := range drafts {
		if d.ID == id {
			return d, true
		}
	}
	return messagedrafts.Draft{}, false
}

// reviewPause is the review pause a callback's token addresses in THIS chat's conversation: a
// button from another chat finds nothing here.
func (h *hitl) reviewPause(ctx context.Context, convID, token string) (askuser.Pending, bool) {
	pending, err := h.runner.PendingFor(ctx, convID)
	if err != nil {
		return askuser.Pending{}, false
	}
	for _, p := range pending {
		if p.Token == token {
			return p, messagedrafts.IsReviewPause(p.ResumeContext)
		}
	}
	return askuser.Pending{}, false
}

// resolveReview answers a review from its card. Accept sends (or approves a scheduled draft),
// decline and cancel decline; there are no edits on Telegram. It runs under its own approval
// operation, keyed by the draft (decision 34): a callback carries none, and the send it may
// start needs one before the gateway.
func (h *hitl) resolveReview(ctx context.Context, convID string, p askuser.Pending, action string) callbackOutcome {
	out := callbackOutcome{action: action}
	draft, ok := h.reviewDraft(ctx, convID, p)
	if !ok {
		return out
	}
	resolve := "decline"
	if action == askuser.ActionAccept {
		resolve = "send"
	}
	opCtx, err := reviewOperation(ctx, draft.ID)
	if err != nil {
		slog.Warn("telegram review: operation not rooted", "draft_id", draft.ID, "err", err)
		return out
	}
	res, err := h.runner.ResolveMessageDraft(opCtx, draft.ID, resolve, nil)
	if errors.Is(err, messagedrafts.ErrUnavailable) {
		// A card whose state moved (a send still in flight) is shown as it is now, rather than
		// left with a button that can no longer act.
		if current, found := h.reviewDraft(ctx, convID, p); found {
			out.rerender, out.rerenderMarkup = messageReviewText(current, h.runner.Location(ctx)), messageReviewMarkup(p.Token, current)
		}
		return out
	}
	if err != nil {
		slog.Warn("telegram review: resolve failed", "draft_id", draft.ID, "err", err)
		return out
	}
	out.submitted = true
	out.outcome = res.Directive.Outcome
	// The card becomes its outcome, keyboard gone: measured 2026-11-12, a card that only lost
	// its buttons still read as a pending review. The status comes from the resolve, because a
	// resolved review is no longer listed.
	if res.Status != "" {
		draft.Status = res.Status
		out.rerender, out.rerenderMarkup = messageReviewText(draft, h.runner.Location(ctx)), &tele.ReplyMarkup{}
	}
	return out
}

// reviewOperation roots the approval operation a Telegram review runs under, as the cockpit's
// resolve runs under its HTTP operation.
func reviewOperation(ctx context.Context, draftID string) (context.Context, error) {
	identityID := identityctx.IdentityID(ctx)
	seed := struct {
		Version string `json:"version"`
		DraftID string `json:"draft_id"`
	}{Version: "telegram-review-v1", DraftID: draftID}
	fingerprint, err := idempotency.FingerprintTyped(seed)
	if err != nil {
		return nil, err
	}
	return idempotency.WithOperation(ctx, idempotency.Operation{
		Key:         idempotency.OperationKey{IdentityID: identityID, Scope: idempotency.ScopeApproval, Key: "telegram-review:" + draftID},
		Fingerprint: fingerprint,
	})
}
