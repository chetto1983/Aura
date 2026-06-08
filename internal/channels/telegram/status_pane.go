// Package telegram — this file is the status pane consumer (msg #1, status-pane-B).
// It maintains a single message edited IN PLACE as the turn progresses: a tool list
// (🟡 in-flight → ✅/❌ on result), a 💭 reasoning line, and a running-cost footer.
// Edits coalesce to the status throttle (a coalescing editor) so a fast event stream
// does not exceed the Bot-API edit rate.
package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"
)

// Status-pane glyphs (status-pane-B, PRD §Slice 9).
const (
	glyphRunning = "🟡"
	glyphOK      = "✅"
	glyphFail    = "❌"
	glyphThink   = "💭"
)

// toolState tracks one tool call's lifecycle in the pane (ordered by first-seen).
type toolState struct {
	name   string
	glyph  string // running → ok/fail
	failed bool
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
	thinking string
	cost     string
	failed   bool

	lastEdit time.Time
	dirty    bool
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
	}
}

// consume drains the status subscriber channel, updating the pane per event family
// (RUN_STARTED open; TOOL_CALL_* tool list; REASONING_* 💭; STATE_DELTA cost footer;
// RUN_FINISHED/RUN_ERROR finalize). The channel is closed by the Fanout producer.
func (p *statusPane) consume(ctx context.Context, ch <-chan events.Event) {
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
		p.startTool(e.ToolCallID, e.ToolCallName)
	case *events.ToolCallResultEvent:
		p.finishTool(e.ToolCallID, e.Content)
	case *events.ToolCallEndEvent:
		// END without a RESULT (rare) still resolves the spinner to OK.
		if ts, ok := p.byID[e.ToolCallID]; ok && ts.glyph == glyphRunning {
			ts.glyph = glyphOK
			p.dirty = true
		}
	case *events.ReasoningMessageContentEvent:
		p.thinking += e.Delta // accumulate raw; collapse for display in text()
		p.dirty = true
	case *events.StateDeltaEvent:
		p.applyCost(e.Delta)
	case *events.RunErrorEvent:
		p.failed = true
		p.dirty = true
	case *events.RunFinishedEvent:
		// The 💭 reasoning is transient live-progress: once the turn finishes (the
		// final answer is in msg #2) drop it so the pane settles to just the durable
		// tool list + cost footer, not a stale wall of thinking.
		p.thinking = ""
		p.dirty = true
	}
}

// startTool registers a tool call as in-flight (🟡), ordered by first-seen.
func (p *statusPane) startTool(id, name string) {
	if _, ok := p.byID[id]; ok {
		return
	}
	ts := &toolState{name: name, glyph: glyphRunning}
	p.byID[id] = ts
	p.tools = append(p.tools, ts)
	p.dirty = true
}

// finishTool resolves a tool's spinner to ✅ or ❌ based on the result preview. A
// result whose preview carries an error marker resolves to ❌.
func (p *statusPane) finishTool(id, preview string) {
	ts, ok := p.byID[id]
	if !ok {
		// A RESULT without a START (preview-only) still appears as a resolved row.
		ts = &toolState{name: id, glyph: glyphRunning}
		p.byID[id] = ts
		p.tools = append(p.tools, ts)
	}
	if looksLikeToolError(preview) {
		ts.glyph = glyphFail
		ts.failed = true
	} else {
		ts.glyph = glyphOK
	}
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
		}
	}
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
	if p.msg == nil {
		out, err := p.bot.Send(p.to, text)
		if err != nil {
			return
		}
		p.msg = out
	} else {
		out, err := p.bot.Edit(p.msg, text)
		if err == nil && out != nil {
			p.msg = out
		}
	}
	p.lastEdit = p.now()
	p.dirty = false
}

// text renders the pane body: a status header, the tool list, an optional 💭
// reasoning line, and an optional running-cost footer. Plain text (no MarkdownV2) —
// the status pane uses glyphs, not entities, so it never risks a parse-entity 400.
func (p *statusPane) text() string {
	var b strings.Builder
	if p.failed {
		b.WriteString(glyphFail + " Errore")
	} else {
		b.WriteString("Aura")
	}
	for _, ts := range p.tools {
		b.WriteString("\n")
		b.WriteString(ts.glyph + " " + ts.name)
	}
	if reasoning := collapseWhitespace(p.thinking); reasoning != "" {
		b.WriteString("\n" + glyphThink + " " + capRunes(reasoning, 160))
	}
	if p.cost != "" {
		b.WriteString("\n— " + p.cost)
	}
	return b.String()
}

// looksLikeToolError reports whether a tool-result preview indicates a failure (the
// ❌ trigger). Tool results stamp an "error" prefix/marker on failure; a clean
// result does not.
func looksLikeToolError(preview string) bool {
	lp := strings.ToLower(strings.TrimSpace(preview))
	return strings.HasPrefix(lp, "error") || strings.HasPrefix(lp, "❌") || strings.Contains(lp, "\"error\"")
}

// collapseWhitespace squeezes runs of whitespace to single spaces so a streamed
// reasoning delta renders as one compact 💭 line.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
