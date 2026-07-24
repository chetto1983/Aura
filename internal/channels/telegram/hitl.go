// Package telegram — this file is the HITL surface over the Runner pause backend
// (UX-02). It is RENDER-ONLY: an ask_user pause is rendered as an InlineKeyboard
// (discrete options / approval) or a ForceReply (free-text clarification); the
// button callback / reply answer is fed back to runner.SubmitAnswer, and a fresh
// runner.Turn(convID, nil) resumes the loop once nothing is unresolved. The
// channel NEVER writes paused_states — the Runner stays the sole writer
// (T-13-06-PauseHijack); HITL only reads pending via PendingFor and resolves via
// SubmitAnswer with the three-action accept/decline/cancel model.
package telegram

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/runner"
)

// resumeRunner is the consumer-side seam over the Runner's pause surface
// (runner_resume.go). Declaring it here (not importing *runner.Runner) keeps HITL
// render-only and unit-testable, and makes the contract explicit: the channel
// reads pending and resolves via SubmitAnswer — it never touches askuser.Store.
type resumeRunner interface {
	PendingFor(ctx context.Context, convID string) ([]askuser.Pending, error)
	SubmitAnswer(ctx context.Context, token string, resp runner.ResponseInput) (runner.ResolveDirective, error)
}

// resumeFunc drives a continuation turn (runner.Turn(ctx, convID, nil)) after the
// pause(s) resolved. Injected so the resume is the SAME per-turn fanout path the
// channel uses for a fresh message (the composition root wires handleTurn).
type resumeFunc func(ctx context.Context, convID string)

// callbackUnique is the inline-button endpoint name the channel registers the HITL
// callback handler under (telebot routes `\f<unique>` callbacks to it).
const callbackUnique = "aura_hitl"

// callbackSep delimits the token|action|value payload carried in a button's
// callback_data. Telegram caps callback_data at 64 bytes; tokens are UUIDs (36) +
// a short action, so a discrete option's value is kept compact (its index-derived
// value, not free prose).
const callbackSep = "|"

const callbackDataMaxBytes = 64

// hitl renders ask_user pauses and resolves them through the Runner.
type hitl struct {
	runner resumeRunner
	resume resumeFunc
}

type callbackOutcome struct {
	action    string
	content   string
	submitted bool
	resumed   bool
}

// newHitl builds the HITL surface over the Runner pause backend + the resume
// driver.
func newHitl(r resumeRunner, resume resumeFunc) *hitl {
	return &hitl{runner: r, resume: resume}
}

// prompt renders the FIRST unresolved pause for a conversation (FIFO — the
// Runner's PendingFor order is authoritative) as an InlineKeyboard or a
// ForceReply. A conversation with no pending pause is a no-op. It returns the sent
// prompt message (nil on no-pause/send-fail) so the caller can later disarm its
// keyboard when a /cancel resolves the pause (M-e).
func (h *hitl) prompt(ctx context.Context, bot botSender, chatID int64, convID string) *tele.Message {
	pending, err := h.runner.PendingFor(ctx, convID)
	if err != nil {
		slog.Warn("telegram hitl: pending lookup failed", "conv", convID, "err", err)
		return nil
	}
	if len(pending) == 0 {
		return nil
	}
	p := pending[0]
	to := tele.ChatID(chatID)
	switch p.Kind {
	case "approval":
		return h.send(bot, to, p.Question, approvalMarkup(p.Token))
	case "choice":
		return h.send(bot, to, p.Question, choiceMarkup(p.Token, p.Options))
	default: // clarification (free-text): force the client into a reply
		return h.send(bot, to, p.Question, &tele.ReplyMarkup{ForceReply: true})
	}
}

// send renders one prompt message with a reply markup (best-effort: a failed send
// must not wedge the turn). It returns the sent message so the caller can track it
// for a later keyboard disarm.
func (h *hitl) send(bot botSender, to tele.Recipient, text string, markup *tele.ReplyMarkup) *tele.Message {
	out, err := bot.Send(to, text, &tele.SendOptions{ReplyMarkup: markup})
	if err != nil {
		slog.Warn("telegram hitl: prompt send failed", "err", err)
		return nil
	}
	return out
}

// handleCallback resolves a button press: it parses token|action|value, submits
// the answer through the Runner, and drives a resume Turn ONLY when nothing is
// unresolved AND the action was not a cancel (a cancel terminates the turn — the
// Runner already injected the terminating answers + auto-resolved). It returns
// whether a resume was driven.
func (h *hitl) handleCallback(ctx context.Context, data, convID string) (resumed bool) {
	return h.handleCallbackResult(ctx, data, convID, nil).resumed
}

func (h *hitl) handleCallbackResult(
	ctx context.Context,
	data, convID string,
	afterSubmit func(callbackOutcome),
) callbackOutcome {
	token, action, value, ok := parseCallback(data)
	if !ok {
		return callbackOutcome{}
	}
	out := callbackOutcome{action: action}
	if action == askuser.ActionAccept {
		value = h.resolveChoiceValue(ctx, convID, token, value)
	}
	out.content = value
	directive, err := h.runner.SubmitAnswer(ctx, token, runner.ResponseInput{Action: action, Content: value})
	if err != nil {
		slog.Warn("telegram hitl: submit answer failed", "conv", convID, "err", err)
		return out
	}
	out.submitted = true
	if afterSubmit != nil {
		afterSubmit(out)
	}
	if action == askuser.ActionCancel || directive.Remaining > 0 {
		return out
	}
	if h.resume != nil {
		h.resume(ctx, convID)
	}
	out.resumed = true
	return out
}

// resolveChoiceValue maps a choice button's compact option INDEX (carried in
// callback_data to stay under Telegram's 64-byte cap — embedding a long option value
// 400s BUTTON_DATA_INVALID) back to the option's full value, which is the answer the
// Runner records. A non-numeric value (an approval "yes") or a token whose pause has
// no matching option passes through unchanged, so approval/clarification are untouched.
func (h *hitl) resolveChoiceValue(ctx context.Context, convID, token, value string) string {
	idx, err := strconv.Atoi(value)
	if err != nil {
		return value
	}
	pending, err := h.runner.PendingFor(ctx, convID)
	if err != nil {
		return value
	}
	for _, p := range pending {
		if p.Token != token {
			continue
		}
		opts := decodeOptions(p.Options)
		if idx >= 0 && idx < len(opts) {
			return opts[idx].Value
		}
		break
	}
	return value
}

// handleTextReply resolves a free-text ForceReply answer: it reads the first
// pending pause and, when there is one, submits the text as an accept answer. It
// returns whether a resume was driven. With no pending pause it is a no-op
// (handled==false) so an ordinary message is not stolen by HITL.
func (h *hitl) handleTextReply(ctx context.Context, convID, text string) (resumed bool, err error) {
	pending, perr := h.runner.PendingFor(ctx, convID)
	if perr != nil || len(pending) == 0 {
		return false, nil
	}
	return h.submit(ctx, convID, pending[0].Token, askuser.ActionAccept, text)
}

// submit resolves ONE pause via the Runner (the sole paused_states writer) and
// drives a resume Turn when remaining==0 and the action was not a cancel. A submit
// error is returned (not just logged) so the channel can tell the user their answer
// did not register — the pause stays open, so silent swallowing would strand them.
func (h *hitl) submit(ctx context.Context, convID, token, action, content string) (resumed bool, err error) {
	directive, err := h.runner.SubmitAnswer(ctx, token, runner.ResponseInput{Action: action, Content: content})
	if err != nil {
		slog.Warn("telegram hitl: submit answer failed", "conv", convID, "err", err)
		return false, err
	}
	// A cancel terminates the turn (the Runner injected the terminating answers and
	// auto-resolved) — never drive a continuation Turn.
	if action == askuser.ActionCancel {
		return false, nil
	}
	if directive.Remaining > 0 {
		return false, nil // more pauses to answer first (FIFO)
	}
	if h.resume != nil {
		h.resume(ctx, convID)
	}
	return true, nil
}

// cancel resolves a pending pause via the Runner with ActionCancel (the same action
// the "Annulla" button submits). A cancel terminates the turn — the Runner injects
// the terminating answers and auto-resolves the whole FIFO group — so NO continuation
// Turn is driven. The submit error is returned (not swallowed) so the channel can
// tell the user their cancel did not register.
func (h *hitl) cancel(ctx context.Context, convID, token string) (resumed bool, err error) {
	return h.submit(ctx, convID, token, askuser.ActionCancel, "")
}

// resolveScheduled resolves a scheduled_task_approval pause by the operator's on-channel action
// WITHOUT driving a continuation turn: an operator-origin scheduled approval has no live turn to
// continue, and the scheduled-task ResumeHook (accept→activate, decline→cancel) fires inside
// SubmitAnswer. action MUST be askuser.ActionAccept or ActionDecline — never ActionCancel, which
// SubmitAnswer routes to cancelConversation and would abort the whole origin conversation.
func (h *hitl) resolveScheduled(ctx context.Context, token, action string) error {
	directive, err := h.runner.SubmitAnswer(ctx, token, runner.ResponseInput{Action: action})
	_ = directive
	return err
}

// approvalMarkup builds the accept/decline InlineKeyboard for an approval pause.
func approvalMarkup(token string) *tele.ReplyMarkup {
	mk := &tele.ReplyMarkup{}
	mk.InlineKeyboard = [][]tele.InlineButton{{
		{Unique: callbackUnique, Text: "Sì", Data: callbackData(token, askuser.ActionAccept, "yes")},
		{Unique: callbackUnique, Text: "No", Data: callbackData(token, askuser.ActionDecline, "")},
	}}
	return mk
}

// choiceMarkup builds one InlineButton per discrete option, each callback carrying
// the pause token + the option's INDEX (NOT its value): callback_data is capped at 64
// bytes by Telegram, and a free-prose option value (e.g. a full sentence) overflows it
// → BUTTON_DATA_INVALID (400). The index is resolved back to the value on the callback
// (resolveChoiceValue). A malformed/empty options blob degrades to a single cancel
// button so the user is never stuck.
func choiceMarkup(token string, raw json.RawMessage) *tele.ReplyMarkup {
	opts := decodeOptions(raw)
	mk := &tele.ReplyMarkup{}
	rows := make([][]tele.InlineButton, 0, len(opts)+1)
	for i, o := range opts {
		rows = append(rows, []tele.InlineButton{{
			Unique: callbackUnique,
			Text:   o.Label,
			Data:   callbackData(token, askuser.ActionAccept, strconv.Itoa(i)),
		}})
	}
	rows = append(rows, []tele.InlineButton{{
		Unique: callbackUnique, Text: "Annulla", Data: callbackData(token, askuser.ActionCancel, ""),
	}})
	mk.InlineKeyboard = rows
	return mk
}

// pauseOption is the decoded {label,value} an options pause carries.
type pauseOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// decodeOptions parses the paused_states.options jsonb into label/value pairs.
func decodeOptions(raw []byte) []pauseOption {
	if len(raw) == 0 {
		return nil
	}
	var opts []pauseOption
	if err := json.Unmarshal(raw, &opts); err != nil {
		return nil
	}
	return opts
}

// callbackData encodes token|action|value into an inline-button payload. The value
// is kept compact (an option value or a short marker), well under Telegram's
// 64-byte callback_data ceiling.
func callbackData(token, action, value string) string {
	data := strings.Join([]string{token, action, value}, callbackSep)
	if len(data) > callbackDataMaxBytes {
		panic("telegram callback_data exceeds 64 bytes")
	}
	return data
}

// parseCallback splits a token|action|value payload, validating the token+action
// are present. A value may be empty (decline/cancel). A payload missing a
// delimiter is malformed → ok=false (the caller ignores it).
func parseCallback(data string) (token, action, value string, ok bool) {
	parts := strings.SplitN(data, callbackSep, 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", false
	}
	if len(parts) == 3 {
		value = parts[2]
	}
	return parts[0], parts[1], value, true
}
