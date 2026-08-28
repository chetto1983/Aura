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
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/approvaltext"
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

// callbackTelebotFrameBytes is telebot's per-button on-wire overhead beyond Data: the leading
// "\f" (1) + Unique + the "|" (1) joining Unique to Data (processButtons, options.go). Telegram
// caps the FRAMED payload at 64 bytes, so the budget must be checked with the framing included —
// checking Data alone is exactly what shipped BUTTON_DATA_INVALID(400) on the 19-char
// scheduled-approval endpoint this callback absorbed (fix-plan 1.7 defect B).
const callbackTelebotFrameBytes = len("\f") + len(callbackUnique) + len(callbackSep)

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
	// outcome is the runner's ResolveDirective verdict — the channel renders it, it never
	// re-derives it (Phase A: one resolver, transports only render).
	outcome runner.ResolveOutcome
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
		question := localizedApprovalQuestion(p)
		// An approval that carries options is one the gateway attached approval SCOPES to
		// (amendment #127): the operator picks how long their yes lasts, not merely whether
		// it is a yes. Without this branch the fixed Sì/No keyboard answered with an empty
		// content, which the gateway resolves to the narrowest scope — correct, but it
		// silently withheld a choice the operator had been offered. A bare approval keeps
		// the fixed keyboard.
		if len(decodeOptions(p.Options)) > 0 {
			return h.send(bot, to, question, approvalScopeMarkup(p.Token, p.Options))
		}
		return h.send(bot, to, question, approvalMarkup(p.Token))
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
	// Dettagli only reveals; resolving here would answer a pause the operator has not decided on.
	if action == actionDetails {
		return out
	}
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
	out.outcome = directive.Outcome
	if afterSubmit != nil {
		afterSubmit(out)
	}
	// A continuation turn is driven ONLY for OutcomeContinue. Terminated (cancel — the Runner
	// already injected the terminating answers), Pending (more FIFO pauses first), Approved and
	// Rejected (the scheduled-task ResumeHook already activated/cancelled the task; there is no
	// live turn to continue) are rendered by the caller instead.
	if directive.Outcome != runner.OutcomeContinue || h.resume == nil {
		return out
	}
	h.resume(ctx, convID)
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

// questionFor returns the bounded question of the pause a token addresses, or "" when that pause
// is gone (already resolved / stale button). The text is the server-sanitized one the Runner
// stored — the channel reveals it, it never composes detail of its own.
func (h *hitl) questionFor(ctx context.Context, convID, token string) string {
	pending, err := h.runner.PendingFor(ctx, convID)
	if err != nil {
		return ""
	}
	for _, p := range pending {
		if p.Token == token {
			return localizedApprovalQuestion(p)
		}
	}
	return ""
}

// localizedApprovalQuestion renders only recognized metadata still bound to the canonical
// question. Ordinary and legacy pauses retain that question verbatim; consent verification still
// uses the canonical persisted value, never this display-only string.
func localizedApprovalQuestion(p askuser.Pending) string {
	if p.Kind != "approval" {
		return p.Question
	}
	presentation, ok := approvaltext.FromContext(p.Question, p.ResumeContext)
	if !ok {
		return p.Question
	}
	return approvaltext.RenderItalian(presentation, p.Question)
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

// submit resolves ONE pause via the Runner (the sole paused_states writer) and drives a resume
// Turn only when the directive says Continue — the SAME rule handleCallbackResult applies, so a
// scheduled approval answered by ForceReply text behaves exactly like one answered by button. A
// submit error is returned (not just logged) so the channel can tell the user their answer did
// not register — the pause stays open, so silent swallowing would strand them.
func (h *hitl) submit(ctx context.Context, convID, token, action, content string) (resumed bool, err error) {
	directive, err := h.runner.SubmitAnswer(ctx, token, runner.ResponseInput{Action: action, Content: content})
	if err != nil {
		slog.Warn("telegram hitl: submit answer failed", "conv", convID, "err", err)
		return false, err
	}
	if directive.Outcome != runner.OutcomeContinue || h.resume == nil {
		return false, nil
	}
	h.resume(ctx, convID)
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

// actionDetails is a channel-local action carried in callback_data that reveals the pause's full
// bounded question. It is NOT an askuser action: it never reaches SubmitAnswer and never resolves
// the pause — the keyboard stays armed so the operator reads, then decides.
const actionDetails = "details"

// approvalDecisionRow is the accept/decline pair every approval keyboard carries. Both keyboards
// below build on it and on the SAME callback endpoint, so a rendering fix can no longer land on
// one leg of the approval surface and miss the other (Phase A consolidation).
func approvalDecisionRow(token string) []tele.InlineButton {
	return []tele.InlineButton{
		{Unique: callbackUnique, Text: "Approva", Data: callbackData(token, askuser.ActionAccept, "yes")},
		{Unique: callbackUnique, Text: "Rifiuta", Data: callbackData(token, askuser.ActionDecline, "")},
	}
}

// approvalMarkup is the keyboard for a prompt whose text ALREADY shows the full question: the
// in-turn relay (hitl.prompt renders p.Question), and a push after its detail was revealed. It
// carries NO Dettagli button — revealing what is already on screen edits the message to identical
// content, which Telegram rejects with a 400, so the button would be a dead affordance.
func approvalMarkup(token string) *tele.ReplyMarkup {
	mk := &tele.ReplyMarkup{}
	mk.InlineKeyboard = [][]tele.InlineButton{approvalDecisionRow(token)}
	return mk
}

// approvalPushMarkup is the keyboard for the sweep push, whose copy is deliberately bounded (kind
// + short id, never the payload): there Dettagli reveals the question the text withholds. One tap
// consumes it — the reveal re-arms with approvalMarkup, so a second tap cannot re-edit identical
// content.
func approvalPushMarkup(token string) *tele.ReplyMarkup {
	mk := &tele.ReplyMarkup{}
	mk.InlineKeyboard = [][]tele.InlineButton{
		approvalDecisionRow(token),
		{{Unique: callbackUnique, Text: "Dettagli", Data: callbackData(token, actionDetails, "")}},
	}
	return mk
}

// approvalScopeMarkup is the keyboard for an approval that carries options: one button per
// option (same INDEX-not-value callback encoding as choiceMarkup — Telegram caps callback_data
// at 64 bytes and a scope label is long), then a Rifiuta button so declining stays one tap
// away. It deliberately does NOT reuse approvalDecisionRow: that row's generic "Approva" would
// be a FOURTH accept beside three that each say how long they last, and the one that says
// least would be the easiest to press.
func approvalScopeMarkup(token string, raw json.RawMessage) *tele.ReplyMarkup {
	opts := decodeOptions(raw)
	mk := &tele.ReplyMarkup{}
	rows := make([][]tele.InlineButton, 0, len(opts)+1)
	for i, o := range opts {
		rows = append(rows, []tele.InlineButton{{
			Unique: callbackUnique,
			Text:   scopeButtonText(o),
			Data:   callbackData(token, askuser.ActionAccept, strconv.Itoa(i)),
		}})
	}
	mk.InlineKeyboard = append(rows, []tele.InlineButton{{
		Unique: callbackUnique, Text: "Rifiuta", Data: callbackData(token, askuser.ActionDecline, ""),
	}})
	return mk
}

// scopeGrammar is this channel's copy for the gateway's approval scopes. The gateway ships
// a stable code (gateway_scope:<scope>:<subject>) plus an English fallback label, because it
// is not locale-aware — every surface writes its own words, the same split ResolveOutcome
// already uses. Telegram's own buttons are Italian, so these are too.
var scopeGrammar = map[string]string{
	"once":    "Approva una volta",
	"session": "Approva %s per questa conversazione",
	"always":  "Approva sempre %s",
}

// scopeButtonText renders one option's button. An option that is not a gateway scope, or a
// scope this build does not know, falls back to the label the server sent — never to a raw
// code on a button the operator has to press.
func scopeButtonText(o pauseOption) string {
	rest, ok := strings.CutPrefix(o.Value, "gateway_scope:")
	if !ok {
		return o.Label
	}
	scope, subject, ok := strings.Cut(rest, ":")
	if !ok {
		return o.Label
	}
	text, known := scopeGrammar[scope]
	if !known {
		return o.Label
	}
	if !strings.Contains(text, "%s") {
		return text
	}
	return fmt.Sprintf(text, subject)
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

// callbackData encodes token|action|value into an inline-button payload, guarding the FULL
// on-wire size (framing included — see callbackTelebotFrameBytes). The value is kept compact
// (an option INDEX or a short marker), so the budget holds for a 36-char UUID token. The panic
// is a programming-invariant guard asserted by the markup tests: it can never fire in
// production and exists so lengthening the endpoint name or an action fails loudly in tests
// instead of silently at send time with a 400.
func callbackData(token, action, value string) string {
	data := strings.Join([]string{token, action, value}, callbackSep)
	if callbackTelebotFrameBytes+len(data) > callbackDataMaxBytes {
		panic("telegram callback_data exceeds Telegram's 64-byte on-wire cap")
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
