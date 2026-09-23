// Package telegram — this file is the status pane consumer (msg #1, status-pane-B).
// It maintains a single message edited IN PLACE as the turn progresses: an ordered
// activity list (reasoning/tool/answer lifecycle) and a running-cost footer.
// Edits coalesce to the status throttle (a coalescing editor) so a fast event stream
// does not exceed the Bot-API edit rate.
package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/chetto1983/aura/internal/reasoningtrace"
	tele "gopkg.in/telebot.v4"
)

// Status-pane glyphs (status-pane-B, PRD §Slice 9).
const (
	glyphRunning = "🟡"
	glyphOK      = "✅"
	glyphFail    = "❌"
	glyphThink   = "💭"

	statusCancelUnique = "aura_cancel"
	statusCancelData   = "cancel"
)

// hitlPauseToolName is the ask_user pause primitive's tool name. ask_user is a HITL
// control rendered as an inline keyboard / ForceReply (hitl.go), NOT a work tool, so
// the status pane suppresses it from the tool list. A CLEAN pause never reaches the
// pane anyway (the translator emits RUN_FINISHED-interrupt, no TOOL_CALL events); the
// only way ask_user surfaces here is a MALFORMED call (validation-failed → dispatched
// as a RoleTool error), and that must not flash a confusing "❌ ask_user" while the
// model self-corrects on the next round. Mirrors tools.AskUser{}.Spec().Name.
const hitlPauseToolName = "ask_user"

// toolState tracks one tool call's lifecycle in the pane.
type toolState struct {
	name     string
	glyph    string // running → ok/fail
	failed   bool
	activity *activityState
}

// activityState is one chronological status-pane row. Rows are appended on first
// sight and then updated in place so the pane preserves the turn's event order.
type activityState struct {
	glyph string
	label string
	state string
}

// statusPane manages msg #1 for one turn: opened on RUN_STARTED, edited in place as
// tool/reasoning/cost events arrive, coalesced to the status throttle.
type statusPane struct {
	bot      botSender
	to       tele.Recipient
	throttle time.Duration

	now   func() time.Time
	sleep func(time.Duration)

	msg      *tele.Message
	tools    []*toolState
	byID     map[string]*toolState
	hidden   map[string]struct{} // tool_call ids suppressed from the pane (HITL ask_user)
	activity []*activityState
	byAct    map[string]*activityState
	answer   *activityState
	thinking string
	cost     string
	// limit / limitSteps carry the terminal STATE_DELTA's limit_hit / steps_consumed
	// (amendment #188) so a turn cut by the loop budget says so under the answer
	// instead of reading as a finished one.
	limit      string
	limitSteps string
	failed     bool
	done       bool

	lastEdit time.Time
	dirty    bool

	// actions owns the turn's chat action for as long as consume runs (media_action.go).
	// The status consumer is the SINGLE owner — no other pulse competes for the chat —
	// and it is nil for a pane driven straight through handle().
	actions *mediaActionController
}

// newStatusPane builds a status pane bound to a chat with the status throttle,
// using the real wall clock.
func newStatusPane(bot botSender, to tele.Recipient, throttle time.Duration) *statusPane {
	return &statusPane{
		bot:      bot,
		to:       to,
		throttle: throttle,
		now:      time.Now,
		sleep:    time.Sleep,
		byID:     make(map[string]*toolState),
		hidden:   make(map[string]struct{}),
		byAct:    make(map[string]*activityState),
	}
}

// consume drains the status subscriber channel, updating the pane per event family
// (RUN_STARTED open; TOOL_CALL_*/REASONING_*/TEXT_MESSAGE_* activity rows;
// STATE_DELTA cost footer; RUN_FINISHED/RUN_ERROR finalize). The channel is closed
// by the Fanout producer. For the same span it owns the turn's chat action, so a
// closed channel or a cancelled ctx stops the pulse and joins it.
func (p *statusPane) consume(ctx context.Context, ch <-chan events.Event) {
	if p.actions == nil { // the composition root pre-wires one it shares with the artifact consumer
		p.actions = newMediaActionController(notifierFor(p.bot), p.to)
	}
	p.actions.start(ctx)
	defer p.actions.Stop()
	for ev := range ch {
		if ctx.Err() != nil {
			return
		}
		p.handle(ev)
		p.render(ctx, false)
	}
	p.render(ctx, true) // final flush of any coalesced state
}

// handle folds one event into the pane state (no I/O — render does the editing).
func (p *statusPane) handle(ev events.Event) {
	switch e := ev.(type) {
	case *events.RunStartedEvent:
		p.dirty = true // open the pane on first render
	case *events.ToolCallStartEvent:
		p.actions.Start(e.ToolCallID, e.ToolCallName)
		p.startTool(e.ToolCallID, e.ToolCallName)
	case *events.ToolCallResultEvent:
		p.actions.Finish(e.ToolCallID)
		p.finishTool(e.ToolCallID, e.Content)
	case *events.ToolCallEndEvent:
		// TOOL_CALL_END is emitted after the tool EXECUTED (agui.emitToolInvocation on
		// agent.ToolInvocationEnd), not when its streamed arguments completed, so it is a
		// valid terminal for the chat action even without a RESULT.
		p.actions.Finish(e.ToolCallID)
		// END without a RESULT (rare) still resolves the spinner to OK.
		if ts, ok := p.byID[e.ToolCallID]; ok && ts.glyph == glyphRunning {
			ts.glyph = glyphOK
			if ts.activity != nil {
				ts.activity.glyph = glyphOK
				ts.activity.state = "completato"
			}
			p.dirty = true
		}
	case *events.ReasoningStartEvent:
		p.startActivity("reason:"+e.MessageID, glyphThink, "Ragionamento", "in corso")
		p.thinking = "in corso"
		p.dirty = true
	case *events.ReasoningMessageContentEvent:
		// The row only: the text itself never reaches Telegram, whatever AURA_SHOW_REASONING
		// says -- the operator found it noise (2026-09-23). The cockpit keeps it.
		reasoningtrace.Record("telegram_status_reasoning_delta", map[string]any{
			"message_id": e.MessageID,
			"chars":      reasoningtrace.RuneLen(e.Delta),
		})
		p.startActivity("reason:"+e.MessageID, glyphThink, "Ragionamento", "in corso")
		if p.thinking == "" {
			p.thinking = "in corso"
		}
		p.dirty = true
	case *events.ReasoningEndEvent:
		p.thinking = "completato"
		p.updateActivity("reason:"+e.MessageID, glyphThink, "Ragionamento", "completato")
		p.dirty = true
	case *events.TextMessageStartEvent:
		p.startAnswer()
	case *events.TextMessageContentEvent:
		p.startAnswer()
	case *events.TextMessageEndEvent:
		p.finishAnswer()
	case *events.StateDeltaEvent:
		p.applyCost(e.Delta)
	case *events.RunErrorEvent:
		p.actions.Stop()
		p.failed = true
		p.done = true
		p.dirty = true
	case *events.RunFinishedEvent:
		p.actions.Stop()
		// msg #2 carries the actual answer; the status pane is a lifecycle indicator.
		p.finishAnswer()
		p.done = true
		p.dirty = true
	}
}

// startTool registers a tool call as in-flight (🟡), ordered by first-seen. The
// ask_user HITL pause primitive is suppressed (its UI is the inline keyboard, not a
// pane row) — its id is remembered so the matching result is dropped too.
func (p *statusPane) startTool(id, name string) {
	if name == hitlPauseToolName {
		p.hidden[id] = struct{}{}
		return
	}
	if _, ok := p.byID[id]; ok {
		return
	}
	activity := p.startActivity("tool:"+id, glyphRunning, name, "in corso")
	ts := &toolState{name: name, glyph: glyphRunning, activity: activity}
	p.byID[id] = ts
	p.tools = append(p.tools, ts)
	p.dirty = true
}

// finishTool resolves a tool's spinner to ✅ or ❌ based on the result preview. A
// result whose preview carries an error marker resolves to ❌.
func (p *statusPane) finishTool(id, preview string) {
	if _, suppressed := p.hidden[id]; suppressed {
		return // ask_user pause primitive — never a pane row (start suppressed it)
	}
	ts, ok := p.byID[id]
	if !ok {
		// A RESULT without a START (preview-only) still appears as a resolved row.
		activity := p.startActivity("tool:"+id, glyphRunning, id, "in corso")
		ts = &toolState{name: id, glyph: glyphRunning, activity: activity}
		p.byID[id] = ts
		p.tools = append(p.tools, ts)
	}
	if looksLikeToolError(preview) {
		ts.glyph = glyphFail
		ts.failed = true
		if ts.activity != nil {
			ts.activity.glyph = glyphFail
			ts.activity.state = "errore"
		}
	} else {
		ts.glyph = glyphOK
		if ts.activity != nil {
			ts.activity.glyph = glyphOK
			ts.activity.state = "completato"
		}
	}
	p.dirty = true
}

func (p *statusPane) startActivity(key, glyph, label, state string) *activityState {
	if act, ok := p.byAct[key]; ok {
		if glyph != "" {
			act.glyph = glyph
		}
		if label != "" {
			act.label = label
		}
		if state != "" {
			act.state = state
		}
		p.dirty = true
		return act
	}
	act := &activityState{glyph: glyph, label: label, state: state}
	p.byAct[key] = act
	p.activity = append(p.activity, act)
	p.dirty = true
	return act
}

func (p *statusPane) updateActivity(key, glyph, label, state string) {
	p.startActivity(key, glyph, label, state)
}

func (p *statusPane) startAnswer() {
	if p.answer == nil {
		p.answer = p.startActivity("answer", glyphRunning, "Risposta", "in scrittura")
		return
	}
	if p.answer.glyph == glyphOK {
		return
	}
	p.answer.glyph = glyphRunning
	p.answer.state = "in scrittura"
	p.dirty = true
}

func (p *statusPane) finishAnswer() {
	if p.answer == nil {
		return
	}
	p.answer.glyph = glyphOK
	p.answer.state = "completata"
	p.dirty = true
}

// applyCost updates the running-cost footer from a STATE_DELTA carrying usage/cost
// keys (e.g. cost_usd, total_cost_usd, usage). The footer shows the most specific
// cost key present.
func (p *statusPane) applyCost(ops []events.JSONPatchOperation) {
	for _, op := range ops {
		key := strings.TrimPrefix(op.Path, "/")
		switch key {
		case "cost_usd", "total_cost_usd":
			p.cost = fmt.Sprintf("$%v", op.Value)
			p.dirty = true
		case "usage":
			if p.cost == "" {
				p.cost = fmt.Sprintf("%v", op.Value)
				p.dirty = true
			}
		case "limit_hit":
			if reason, ok := op.Value.(string); ok && reason != "" {
				p.limit = reason
				p.dirty = true
			}
		case "steps_consumed":
			p.limitSteps = fmt.Sprintf("%v", op.Value)
			p.dirty = true
		}
	}
}

// limitText is the budget-trip line under the cost footer: which cap cut the turn,
// how many steps it spent, and the one thing the user can do about it.
func (p *statusPane) limitText() string {
	if p.limit == "" {
		return ""
	}
	label := "budget esaurito (" + p.limit + ")"
	switch p.limit {
	case "max_steps":
		label = "limite di passi raggiunto"
	case "wallclock":
		label = "limite di tempo raggiunto"
	}
	if p.limitSteps != "" {
		label += " · " + p.limitSteps + " passi"
	}
	return "\n⚠️ Turno interrotto: " + label + ". Scrivi «continua» per proseguire."
}

// render edits msg #1 with the current pane text, coalescing to the throttle. final
// forces an edit regardless of the window. A non-final render inside the window with
// pending changes is skipped (the dirty flag survives for the next render).
func (p *statusPane) render(_ context.Context, final bool) {
	if !p.dirty {
		return
	}
	if !final && p.now().Sub(p.lastEdit) < p.throttle {
		return // coalesce: stay dirty, edit on the next event past the window
	}
	text := p.text()
	if text == "" {
		return
	}
	reasoningtrace.Record("telegram_status_render_text", map[string]any{
		"final":          final,
		"has_message":    p.msg != nil,
		"text_chars":     reasoningtrace.RuneLen(text),
		"text":           text,
		"thinking_chars": reasoningtrace.RuneLen(p.thinking),
		"thinking_state": p.thinking,
	})
	opts := &tele.SendOptions{ReplyMarkup: p.markup()}
	if p.msg == nil {
		out, err := p.bot.Send(p.to, text, opts)
		if err != nil {
			reasoningtrace.Record("telegram_status_send_result", map[string]any{
				"op":    "send",
				"ok":    false,
				"error": err.Error(),
				"text":  text,
			})
			return
		}
		reasoningtrace.Record("telegram_status_send_result", map[string]any{
			"op":         "send",
			"ok":         true,
			"message_id": out.ID,
			"text":       text,
		})
		p.msg = out
	} else {
		out, err := p.bot.Edit(p.msg, text, opts)
		if err == nil && out != nil {
			reasoningtrace.Record("telegram_status_send_result", map[string]any{
				"op":         "edit",
				"ok":         true,
				"message_id": out.ID,
				"text":       text,
			})
			p.msg = out
		} else if err != nil {
			reasoningtrace.Record("telegram_status_send_result", map[string]any{
				"op":         "edit",
				"ok":         false,
				"message_id": p.msg.ID,
				"error":      err.Error(),
				"text":       text,
			})
		} else {
			reasoningtrace.Record("telegram_status_send_result", map[string]any{
				"op":         "edit",
				"ok":         false,
				"message_id": p.msg.ID,
				"error":      "nil message",
				"text":       text,
			})
		}
	}
	p.lastEdit = p.now()
	p.dirty = false
}

// text renders the pane body: a status header, chronological activity rows and an
// optional running-cost footer. Plain text (no MarkdownV2) — the status pane uses
// glyphs, not entities, so it never risks a parse-entity 400.
func (p *statusPane) text() string {
	base := p.baseText()
	footer := p.costText() + p.limitText()
	if over := runeLen(base) + runeLen(footer) - telegramTextCap; over > 0 {
		base = capRunes(base, max(0, runeLen(base)-over))
	}
	return base + footer
}

func (p *statusPane) baseText() string {
	var b strings.Builder
	b.WriteString("Aura")
	switch {
	case p.failed:
		b.WriteString("\nStato: errore")
	case p.done:
		b.WriteString("\nStato: completato")
	default:
		b.WriteString("\nStato: in corso")
	}
	if len(p.activity) > 0 {
		b.WriteString("\nSequenza:")
		for _, act := range p.activity {
			b.WriteString("\n")
			b.WriteString(act.glyph + " " + act.label)
			if act.state != "" {
				b.WriteString(" - " + act.state)
			}
		}
	}
	return b.String()
}

func (p *statusPane) costText() string {
	if p.cost == "" {
		return ""
	}
	return "\nCosto: " + p.cost
}

func runeLen(s string) int {
	return len([]rune(s))
}

func (p *statusPane) markup() *tele.ReplyMarkup {
	if p.done {
		return &tele.ReplyMarkup{}
	}
	return &tele.ReplyMarkup{InlineKeyboard: [][]tele.InlineButton{{
		{Unique: statusCancelUnique, Text: "Annulla", Data: statusCancelData},
	}}}
}

// looksLikeToolError reports whether a tool-result preview indicates a failure (the
// ❌ trigger). Tool results stamp an "error" prefix/marker on failure; a clean
// result does not.
func looksLikeToolError(preview string) bool {
	lp := strings.ToLower(strings.TrimSpace(preview))
	return strings.HasPrefix(lp, "error") || strings.HasPrefix(lp, "❌") || strings.Contains(lp, "\"error\"")
}
