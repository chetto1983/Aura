package telegramadapter

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tgmd "github.com/eekstunt/telegramify-markdown-go"
	tele "gopkg.in/telebot.v4"
)

// editThrottle bounds Telegram editMessageText to the same message. Mirrors
// streamingEditThrottle in outbound.go — the two writers (statusPane and
// Outbound.ConsumeStream) share a single budget once wired in slice 2.
const editThrottle = 600 * time.Millisecond

// status pane copy (Italian — locked).
const (
	statusHeader        = "🛠 Sto lavorando…"
	statusGlyphRunning  = "🟡"
	statusGlyphDone     = "✅"
	statusGlyphFailed   = "❌"
	statusCollapseArrow = "▸"
	statusMaxNamesShown = 3 // collapsed summary truncates after N tool names
	statusMaxHistory    = 3 // roundHistory cap; older rounds collapse to "… N round precedenti"
	statusPreviewMaxLen = 80
)

// toolState is the lifecycle state of a single tool call inside a round.
type toolState uint8

const (
	toolRunning toolState = iota
	toolDone
	toolFailed
)

// toolEntry is one tool call inside the current (active) round.
type toolEntry struct {
	callID    string
	name      string
	argKeys   []string
	startedAt time.Time
	state     toolState
	elapsedMs int64
	preview   string // failure preview — empty on success (privacy: never shown on success)
}

// roundSummary is a completed round in roundHistory. Previews are cleared on
// archive (we render only glyph + name for archived rounds).
type roundSummary struct {
	totalMs int64
	entries []toolEntry
}

// statusPane renders in-flight tool calls into the existing Telegram
// placeholder message. Lifecycle:
//
//	┌─ OnToolStart/OnToolEnd ──► compose() ─► editFn(text, entities)
//	│                                              │
//	│  contentMode=false: full body  ───────────── │
//	│                                              │
//	└─ EnterContentMode() ──► Outbound owns body,  │
//	                          pane provides Footer()
//
// Throttle: 600ms shared with Outbound (slice 2 hooks markEdited from both
// sides; for now editFn is invoked directly when within budget, otherwise
// the next call coalesces).
type statusPane struct {
	mu  sync.Mutex
	now func() time.Time

	activeRound   []toolEntry
	roundHistory  []roundSummary
	contentMode   bool
	finalized     bool
	turnStartedAt time.Time

	// throttle state
	lastEdit time.Time
	lastBody string // last body sent; suppress edit when rebuilt body is identical

	// editFn is called with (text, entities) whenever the pane wants to
	// write to the placeholder. Nil = no-op (slice 1 default for tests
	// that exercise state only).
	editFn func(text string, ents tele.Entities) error

	// cotProvider lets Outbound inject a thunk returning the current
	// reasoning buffer. The pane reads it each flush and renders
	// "🧠 _reasoning_" above the status block so a reasoning LLM's
	// pre-tool thought is visible to the user (previously this was
	// Outbound's job, but Outbound's first reasoning flush used to
	// fire EnterContentMode prematurely — see the fix doc).
	cotProvider func() string
}

func newStatusPane(now func() time.Time, editFn func(string, tele.Entities) error) *statusPane {
	if now == nil {
		now = time.Now
	}
	return &statusPane{
		now:           now,
		turnStartedAt: now(),
		editFn:        editFn,
	}
}

// OnToolStart records a tool dispatch and triggers a throttled flush.
//
// State must accumulate even after contentMode flips so footer counts stay
// accurate — the contentMode early-return USED to live here too, but that
// caused silent data loss when reasoning fired EnterContentMode before the
// first OnToolStart. Edit suppression lives in flushLocked instead, which
// is the right place because it's the side effect (writing to Telegram)
// that needs gating, not the state machine.
func (p *statusPane) OnToolStart(callID, name string, argKeys []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finalized {
		return
	}
	p.activeRound = append(p.activeRound, toolEntry{
		callID:    callID,
		name:      name,
		argKeys:   append([]string(nil), argKeys...),
		startedAt: p.now(),
		state:     toolRunning,
	})
	p.flushLocked()
}

// OnToolEnd mutates the matching entry to done/failed. If all entries in the
// active round are now terminal, archives the round into roundHistory.
func (p *statusPane) OnToolEnd(callID string, success bool, elapsed time.Duration, preview string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finalized {
		return
	}
	// Even in contentMode, we still mutate state so Footer() reports
	// accurate counts. We just don't emit edits — Outbound owns the body.
	for i := range p.activeRound {
		if p.activeRound[i].callID == callID {
			p.activeRound[i].elapsedMs = elapsed.Milliseconds()
			if success {
				p.activeRound[i].state = toolDone
				p.activeRound[i].preview = "" // privacy: never surface success previews
			} else {
				p.activeRound[i].state = toolFailed
				p.activeRound[i].preview = sanitizePreview(preview)
			}
			break
		}
	}
	if p.roundComplete() {
		p.archiveRound()
	}
	if !p.contentMode {
		p.flushLocked()
	}
}

// EnterContentMode flips the pane: from this point Outbound owns the message
// body. The pane stops issuing edits and instead exposes Footer() for Outbound
// to prepend above streaming content.
func (p *statusPane) EnterContentMode() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.contentMode {
		return
	}
	p.contentMode = true
	// Archive any straggling running entries — they'll be counted in the
	// footer but won't get a per-call render.
	if len(p.activeRound) > 0 {
		p.archiveRound()
	}
}

// Finalize is called from EventFinal. Any still-running entries get marked as
// failed("(no end signal)") so footer counts are accurate.
func (p *statusPane) Finalize() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finalized {
		return
	}
	for i := range p.activeRound {
		if p.activeRound[i].state == toolRunning {
			p.activeRound[i].state = toolFailed
			if p.activeRound[i].preview == "" {
				p.activeRound[i].preview = "(no end signal)"
			}
		}
	}
	if len(p.activeRound) > 0 {
		p.archiveRound()
	}
	p.finalized = true
}

// FooterMarkdown returns the Layout D demoted footer as a CommonMark string
// (wrapped in `_..._` italics), suitable for embedding in the body that
// Outbound passes to tgtelegram.RenderForEntities. Empty when no tools have
// run yet.
//
// Why a markdown string and not (text, entities): once content streaming
// starts, Outbound composes a single body of "footer + \n\n + content" and
// runs the whole thing through the MarkdownV2 parser. Using markdown here
// keeps the rendering path uniform with composeStreamingMessage's CoT
// handling.
func (p *statusPane) FooterMarkdown() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	text := p.composeFooterTextLocked()
	if text == "" {
		return ""
	}
	return "_" + text + "_"
}

// MarkEdited resets the throttle clock. Outbound calls this after its own
// edits so both writers respect the same 600ms budget (slice 2).
func (p *statusPane) MarkEdited() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastEdit = p.now()
}

// SetCoTProvider lets Outbound inject a thunk that returns the current
// reasoning buffer. The pane calls it under its own mutex each flush, so
// the provider must be safe to read concurrently with Outbound's writes
// to the underlying buffer — Outbound satisfies this via an atomic.Value
// snapshot. Pass nil at turn end to sever the pointer.
func (p *statusPane) SetCoTProvider(provider func() string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cotProvider = provider
}

// Refresh re-composes and edits the placeholder if the body has changed.
// Called by Outbound whenever it updates external state (e.g. reasoning
// snapshot) that the pane reads but doesn't own. No-op in contentMode or
// after Finalize. Throttle + identical-body guard apply as for any other
// trigger.
func (p *statusPane) Refresh() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.flushLocked()
}

// --- internal ---

func (p *statusPane) roundComplete() bool {
	if len(p.activeRound) == 0 {
		return false
	}
	for _, e := range p.activeRound {
		if e.state == toolRunning {
			return false
		}
	}
	return true
}

func (p *statusPane) archiveRound() {
	if len(p.activeRound) == 0 {
		return
	}
	// totalMs = max elapsed across parallel entries (round wall-clock).
	var maxMs int64
	archived := make([]toolEntry, len(p.activeRound))
	for i, e := range p.activeRound {
		if e.elapsedMs > maxMs {
			maxMs = e.elapsedMs
		}
		e.preview = "" // clear preview on archive (already shown live)
		archived[i] = e
	}
	p.roundHistory = append(p.roundHistory, roundSummary{
		totalMs: maxMs,
		entries: archived,
	})
	// Cap history; oldest rounds collapse into a count-only line.
	// (We keep the raw slice unbounded; rendering caps to statusMaxHistory.)
	p.activeRound = p.activeRound[:0]
}

// flushLocked composes the body and invokes editFn if the throttle allows.
// Caller must hold p.mu.
func (p *statusPane) flushLocked() {
	if p.editFn == nil || p.contentMode || p.finalized {
		return
	}
	text, ents := p.composeLocked()
	if text == "" || text == p.lastBody {
		return
	}
	now := p.now()
	if !p.lastEdit.IsZero() && now.Sub(p.lastEdit) < editThrottle {
		// Coalesce: the next event within the window will rebuild and
		// re-attempt. Slice 2 adds a tail-debounce timer so a single
		// trailing flush fires at lastEdit+throttle even if no further
		// events arrive. For slice 1 the coalescing is event-driven only.
		return
	}
	if err := p.editFn(text, ents); err != nil {
		return // editFn handles its own logging
	}
	p.lastEdit = now
	p.lastBody = text
}

// composeLocked builds the full body: optional CoT prefix (italic) + status
// header + blockquote with active per-call lines and round history. Caller
// must hold p.mu.
//
// Three shapes depending on state:
//
//   - CoT only, no tools yet: "🧠 reasoning" (italic span)
//   - Tools only, no CoT:     "🛠 Sto lavorando…\n[blockquote …]"
//   - Both:                   "🧠 reasoning\n\n🛠 Sto lavorando…\n[blockquote …]"
//
// Returns empty when nothing to render.
func (p *statusPane) composeLocked() (string, tele.Entities) {
	cot := ""
	if p.cotProvider != nil {
		cot = strings.TrimSpace(p.cotProvider())
	}
	hasTools := len(p.activeRound) > 0 || len(p.roundHistory) > 0
	if cot == "" && !hasTools {
		return "", nil
	}

	var sb strings.Builder
	var ents tele.Entities

	// CoT italic prefix: "🧠 " (literal) + italic-span(cot)
	if cot != "" {
		sb.WriteString("🧠 ")
		italicStart := tgmd.UTF16Len(sb.String())
		sb.WriteString(cot)
		italicEnd := tgmd.UTF16Len(sb.String())
		ents = append(ents, tele.MessageEntity{
			Type:   tele.EntityItalic,
			Offset: italicStart,
			Length: italicEnd - italicStart,
		})
	}

	if !hasTools {
		return sb.String(), ents
	}

	if cot != "" {
		sb.WriteString("\n\n")
	}
	sb.WriteString(statusHeader)
	sb.WriteString("\n")

	bqStart := tgmd.UTF16Len(sb.String())

	// Line 1 inside blockquote = collapsed summary (Telegram shows this
	// alone when the user hasn't expanded the blockquote).
	sb.WriteString(statusCollapseArrow)
	sb.WriteString(" ")
	sb.WriteString(p.summaryLineLocked())
	sb.WriteString("\n")

	for _, e := range p.activeRound {
		sb.WriteString(renderActiveLine(e))
		sb.WriteString("\n")
	}

	hist := p.roundHistory
	skipped := 0
	if len(hist) > statusMaxHistory {
		skipped = len(hist) - statusMaxHistory
		hist = hist[len(hist)-statusMaxHistory:]
	}
	if skipped > 0 {
		fmt.Fprintf(&sb, "… %d round precedenti\n", skipped)
	}
	base := len(p.roundHistory) - len(hist) + 1
	for i := len(hist) - 1; i >= 0; i-- {
		sb.WriteString(renderHistoryLine(base+i, hist[i]))
		sb.WriteString("\n")
	}

	body := strings.TrimRight(sb.String(), "\n")
	bqEnd := tgmd.UTF16Len(body)
	ents = append(ents, tele.MessageEntity{
		Type:   tele.EntityEBlockquote,
		Offset: bqStart,
		Length: bqEnd - bqStart,
	})
	return body, ents
}

// composeFooterTextLocked builds the bare text of the Layout D footer (no
// italic markers — wrap with `_..._` for markdown embedding). Empty when the
// turn used no tools.
func (p *statusPane) composeFooterTextLocked() string {
	totalRounds := len(p.roundHistory)
	var totalTools int
	for _, r := range p.roundHistory {
		totalTools += len(r.entries)
	}
	if totalTools == 0 {
		return ""
	}
	elapsed := p.now().Sub(p.turnStartedAt)
	return fmt.Sprintf("🛠 %s in %s · %.1fs",
		pluralIT(totalTools, "strumento usato", "strumenti usati"),
		pluralIT(totalRounds, "round", "round"),
		elapsed.Seconds(),
	)
}

// summaryLineLocked is the single line inside the blockquote that Telegram
// surfaces when the blockquote is collapsed. Caller must hold p.mu.
func (p *statusPane) summaryLineLocked() string {
	roundNum := len(p.roundHistory) + 1
	var running, done, failed int
	for _, e := range p.activeRound {
		switch e.state {
		case toolRunning:
			running++
		case toolDone:
			done++
		case toolFailed:
			failed++
		}
	}
	total := len(p.activeRound)

	var sb strings.Builder
	if roundNum > 1 {
		fmt.Fprintf(&sb, "round %d · ", roundNum)
	}
	switch {
	case total == 0:
		// Should only happen briefly between archive and the next OnToolStart;
		// fall back to a generic count.
		sb.WriteString("in corso")
	case total == 1 && running == 1:
		sb.WriteString(p.activeRound[0].name)
		sb.WriteString(" in corso")
	case running == total:
		fmt.Fprintf(&sb, "%d %s in corso", total, pluralBare(total, "strumento", "strumenti"))
	default:
		parts := []string{fmt.Sprintf("%d %s", total, pluralBare(total, "strumento", "strumenti"))}
		if done > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", done, pluralBare(done, "fatto", "fatti")))
		}
		if failed > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", failed, pluralBare(failed, "errore", "errori")))
		}
		sb.WriteString(strings.Join(parts, " · "))
	}

	// Append names (cap to statusMaxNamesShown).
	if total > 0 {
		names := make([]string, 0, total)
		for _, e := range p.activeRound {
			names = append(names, e.name)
		}
		if len(names) > statusMaxNamesShown {
			extra := len(names) - statusMaxNamesShown
			names = names[:statusMaxNamesShown]
			names = append(names, fmt.Sprintf("… %d altri", extra))
		}
		sb.WriteString(" · ")
		sb.WriteString(strings.Join(names, " · "))
	}
	return sb.String()
}

func renderActiveLine(e toolEntry) string {
	switch e.state {
	case toolRunning:
		if len(e.argKeys) == 0 {
			return fmt.Sprintf("%s %s", statusGlyphRunning, e.name)
		}
		return fmt.Sprintf("%s %s (args: %s)", statusGlyphRunning, e.name, strings.Join(e.argKeys, ", "))
	case toolDone:
		return fmt.Sprintf("%s %s (%.1fs)", statusGlyphDone, e.name, float64(e.elapsedMs)/1000.0)
	case toolFailed:
		preview := e.preview
		if preview == "" {
			preview = "errore"
		}
		return fmt.Sprintf("%s %s — %s", statusGlyphFailed, e.name, preview)
	}
	return ""
}

func renderHistoryLine(roundNum int, r roundSummary) string {
	var parts []string
	for _, e := range r.entries {
		glyph := statusGlyphDone
		if e.state == toolFailed {
			glyph = statusGlyphFailed
		}
		parts = append(parts, fmt.Sprintf("%s %s", glyph, e.name))
	}
	return fmt.Sprintf("— round %d (%dms): %s", roundNum, r.totalMs, strings.Join(parts, " · "))
}

// sanitizePreview keeps only the first line and clips to statusPreviewMaxLen
// runes. Never surface tool result content beyond this — it can carry source
// text, URLs with tokens, base64 (CLAUDE.md privacy rule).
func sanitizePreview(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > statusPreviewMaxLen {
		return string(r[:statusPreviewMaxLen-1]) + "…"
	}
	return s
}

func pluralBare(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func pluralIT(n int, oneForm, manyForm string) string {
	if n == 1 {
		return "1 " + oneForm
	}
	return fmt.Sprintf("%d %s", n, manyForm)
}
