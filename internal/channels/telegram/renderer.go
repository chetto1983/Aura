// Package telegram — this file is the content renderer consumer (msg #2, the
// streamed answer). It consumes the TEXT_MESSAGE_* / TOOL_CALL_RESULT family the
// AG-UI translator produces and renders them to Telegram with three guarantees:
//
//   - HTML parse-mode sends (html.go) with a PLAIN-TEXT FALLBACK on a Bot-API
//     400 "can't parse entities" — the SC#2 "no can't parse entities" guarantee:
//     a rejected parsed send is resent WITHOUT ParseMode.
//   - markdown tables in the content render to a gridded PNG (tables.go) and go out
//     via sendPhoto (caption capped at 1024).
//   - content edits coalesce to the content throttle and per-chat sends are bounded
//     by the chat rate limit; text is capped at the 4096 Bot-API ceiling.
package telegram

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/agui"
)

// Bot-API constraints (spike telegram-channel.md §Constraints).
const (
	telegramTextCap    = 4096
	telegramCaptionCap = 1024
)

// runErrorPrefix labels the sanitized failure reason a RUN_ERROR renders to the
// user (always Italian — the output-language directive). Without a reason the user
// only sees the status pane's bare "Stato: errore" glyph (H2).
const runErrorPrefix = "❌ Errore: "

// canParseEntitiesMarker is the Bot-API 400 description substring that signals a
// MarkdownV2 entity-parse rejection — the plain-text fallback trigger.
const canParseEntitiesMarker = "can't parse entities"

// renderer streams msg #2 for one turn: TEXT_MESSAGE_* deltas accumulate into the
// content message, sent as Telegram HTML with a plain-text fallback, throttled
// and rate-limited. A detected markdown table renders to a PNG sendPhoto.
type renderer struct {
	bot      botSender
	to       tele.Recipient
	throttle time.Duration
	chatRate time.Duration

	// now/sleep are injectable clocks so the throttle/rate-limit timing is
	// deterministic under testing/synctest (real time in production).
	now   func() time.Time
	sleep func(time.Duration)

	buf       strings.Builder // accumulated content for the open text message
	msg       *tele.Message   // msg #2 once first sent; edited in place thereafter
	lastSend  time.Time       // last successful send/edit (throttle + rate-limit gate)
	plain     bool            // once a 400 forced the plain-text fallback, stay plain for this message
	tableSent bool            // a table PNG finalized this turn — ignore further flushes (no duplicate photos)
	textDone  bool            // final text chunks were already sent; ignore duplicate final flushes
}

// botDeleter is the optional telebot surface for removing a superseded message —
// the streamed text msg #2 once a table PNG replaces it. *tele.Bot satisfies it; a
// double that omits Delete makes the cleanup a no-op.
type botDeleter interface {
	Delete(msg tele.Editable) error
}

// newRenderer builds a content renderer bound to a chat with the content throttle
// and the per-chat rate limit, using the real wall clock.
func newRenderer(bot botSender, to tele.Recipient, throttle, chatRate time.Duration) *renderer {
	return &renderer{
		bot:      bot,
		to:       to,
		throttle: throttle,
		chatRate: chatRate,
		now:      time.Now,
		sleep:    time.Sleep,
	}
}

// consume drains the content subscriber channel: it accumulates TEXT_MESSAGE_*
// deltas, flushing on a throttle, and finalizes on TEXT_MESSAGE_END / RUN_FINISHED.
// The channel is closed by the Fanout producer on source-end/ctx-cancel, so the
// range terminates cleanly.
func (r *renderer) consume(ctx context.Context, ch <-chan events.Event) {
	for ev := range ch {
		if ctx.Err() != nil {
			return
		}
		switch e := ev.(type) {
		case *events.TextMessageContentEvent:
			r.buf.WriteString(e.Delta)
			r.flush(ctx, false)
		case *events.TextMessageEndEvent:
			r.flush(ctx, true)
		case *events.RunFinishedEvent:
			// Finalize any buffered content the END frame did not flush (a run that
			// ends without a trailing TEXT_MESSAGE_END still delivers its text).
			r.flush(ctx, true)
		case *events.RunErrorEvent:
			// The turn failed: surface the reason instead of dropping it (H2). The
			// status pane only flips to a bare "Stato: errore" glyph; without this the
			// user never learns WHY. The message is sanitized through the same
			// agui.SanitizeString chokepoint the HTTP RUN_ERROR path uses so a DSN /
			// token embedded in the error never reaches the user (T-19-11).
			r.flush(ctx, true) // deliver any partial answer streamed before the error
			r.sendError(ctx, e.Message)
		}
	}
	// The producer closed the channel (source end or ctx-cancel) — make sure any
	// trailing buffered content reaches the user.
	r.flush(ctx, true)
}

// finalText returns the full accumulated answer text for this turn (the buffer the
// content deltas streamed into). handleTurn reads it after consume returns to feed
// the optional TTS-out path (ShouldSpeak); it is the rendered answer, not a
// re-derivation. Empty when the turn produced no text content.
func (r *renderer) finalText() string {
	return stripProtocolToolBlocks(r.buf.String())
}

// flush sends/edits msg #2 with the accumulated content. final forces a send even
// if the throttle window has not elapsed (the END/RUN_FINISHED finalization). A
// non-final flush inside the throttle window is skipped (coalesced).
func (r *renderer) flush(ctx context.Context, final bool) {
	content := stripProtocolToolBlocks(r.buf.String())
	if !final {
		content = suppressPartialProtocolOpen(content)
	}
	if content == "" || r.tableSent || (final && r.textDone) {
		return // nothing buffered, or the turn already finalized as a table PNG
	}
	if !final && r.now().Sub(r.lastSend) < r.throttle {
		return // coalesce: within the throttle window, wait for the next delta/END
	}
	r.rateLimit(ctx)
	if r.send(content, final) && final {
		r.textDone = true
	}
	r.lastSend = r.now()
}

// sendError delivers a sanitized turn-failure reason as a FRESH user-facing
// message (H2). It routes msg through agui.SanitizeString — the one redaction
// contract the HTTP RUN_ERROR path uses — so a credential-bearing error never
// leaks (T-19-11). An empty reason still sends the generic failure copy so the
// user is never left with only the status pane's bare glyph. forceNew=true so the
// streamed answer msg #2 is never overwritten by the error notice.
func (r *renderer) sendError(ctx context.Context, msg string) {
	reason := strings.TrimSpace(agui.SanitizeString(msg))
	text := runErrorPrefix + reason
	if reason == "" {
		text = strings.TrimSpace(runErrorPrefix)
	}
	r.rateLimit(ctx)
	out, err := r.deliver(RenderTelegramHTML(text), tele.ModeHTML, true)
	if isCantParseEntities(err) {
		out, err = r.deliver(PlainTextFallback(text), tele.ModeDefault, true)
	}
	if err == nil && out != nil {
		r.msg = out
	}
	r.lastSend = r.now()
}

// rateLimit bounds per-chat sends to the chat rate limit by sleeping for the
// remainder of the window since the last send (a no-op when the window elapsed).
func (r *renderer) rateLimit(ctx context.Context) {
	if r.chatRate <= 0 || r.lastSend.IsZero() {
		return
	}
	wait := r.chatRate - r.now().Sub(r.lastSend)
	if wait > 0 && ctx.Err() == nil {
		r.sleep(wait)
	}
}

// send renders content to Telegram: a markdown table goes out as a PNG sendPhoto;
// otherwise the text is converted to Telegram HTML and sent/edited, with the plain-text
// fallback on a 400 can't-parse-entities.
func (r *renderer) send(content string, final bool) bool {
	if grid, ok := ParseMarkdownTable(content); ok {
		// A markdown table renders to a PNG ONCE, on the FINAL flush only. Sending it
		// on every streamed flush posts duplicate photos (the 4× bug); deferring all
		// table output to final also avoids a mid-stream text→PNG flicker.
		if !final {
			return false
		}
		if r.sendTable(grid, content) {
			r.tableSent = true
			return true
		}
		// table render failed → fall through to text rendering of the raw content
	}
	return r.sendText(content, final)
}

// sendText sends/edits msg #2 as Telegram HTML. During live streaming it caps the
// draft to the first Telegram-sized chunk; on finalization it splits the complete
// answer into Telegram-sized messages so the tail is not silently lost.
func (r *renderer) sendText(content string, final bool) bool {
	chunks := []string{capRunes(content, telegramTextCap)}
	if final {
		chunks = splitTelegramText(content, telegramTextCap)
	}
	for i, chunk := range chunks {
		if chunk == "" {
			continue
		}
		if !r.sendTextChunk(chunk, final && i > 0) {
			return i > 0
		}
	}
	return len(chunks) > 0
}

// sendTextChunk sends one Telegram-sized text chunk. forceNew is true for chunk 2+
// of a finalized long answer; chunk 1 edits the streamed message in place.
func (r *renderer) sendTextChunk(chunk string, forceNew bool) bool {
	// Once a 400 forced the fallback for this message, stay in plain mode: editing
	// it back to parsed HTML would just 400 again (the content only grows).
	if r.plain {
		if out, err := r.deliver(PlainTextFallback(chunk), tele.ModeDefault, forceNew); err == nil && out != nil {
			r.msg = out
			return true
		}
		return false
	}

	out, err := r.deliver(RenderTelegramHTML(chunk), tele.ModeHTML, forceNew)
	if isCantParseEntities(err) {
		// Resend the unescaped original without ParseMode (plain-text fallback) and
		// latch plain mode so subsequent edits of this message stay plain.
		r.plain = true
		out, err = r.deliver(PlainTextFallback(chunk), tele.ModeDefault, forceNew)
	}
	if err != nil {
		return false // best-effort: a failed content send must not wedge the turn
	}
	if out != nil {
		r.msg = out
	}
	return true
}

// deliver sends a fresh msg #2 or edits the existing one with the given parse mode.
func (r *renderer) deliver(text string, mode tele.ParseMode, forceNew bool) (*tele.Message, error) {
	opts := sendOpts(mode)
	if forceNew || r.msg == nil {
		return r.bot.Send(r.to, text, opts)
	}
	return r.bot.Edit(r.msg, text, opts)
}

// sendTable renders the grid to a PNG and sends it via sendPhoto with a capped
// caption (the prose around the table, escaped). Returns false when the PNG could
// not be produced (caller falls back to text).
func (r *renderer) sendTable(grid [][]string, content string) bool {
	png, err := RenderTablePNG(grid)
	if err != nil {
		return false
	}
	caption := RenderTelegramHTML(capRunes(tableCaption(content), telegramCaptionCap))
	photo := &tele.Photo{
		File:    tele.FromReader(bytes.NewReader(png)),
		Caption: caption,
	}
	out, sendErr := r.bot.Send(r.to, photo, sendOpts(tele.ModeHTML))
	if isCantParseEntities(sendErr) {
		photo.Caption = capRunes(PlainTextFallback(tableCaption(content)), telegramCaptionCap)
		out, sendErr = r.bot.Send(r.to, photo, sendOpts(tele.ModeDefault))
	}
	if sendErr != nil {
		return false
	}
	// The streamed text msg #2 (if any) is now superseded by the table PNG — delete
	// it so the user sees the table ONCE (the PNG + prose caption), not the streamed
	// prose/markdown AND the photo. Then reset msg so a later text flush opens fresh.
	r.deleteStreamed()
	r.msg = nil
	_ = out
	return true
}

// deleteStreamed best-effort removes the streamed text msg #2 once a table PNG has
// superseded it. No-op when nothing was streamed or the bot lacks Delete.
func (r *renderer) deleteStreamed() {
	if r.msg == nil {
		return
	}
	if d, ok := r.bot.(botDeleter); ok {
		_ = d.Delete(r.msg)
	}
}

// tableCaption returns the prose surrounding a markdown table (the lines that are
// not part of the table grid), so the sendPhoto caption carries the model's framing
// text. Empty when the content is table-only.
func tableCaption(content string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(content, "\n") {
		if strings.Contains(line, "|") {
			continue // a table row — rendered in the PNG, not the caption
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return b.String()
}

// stripProtocolToolBlocks prevents model-emitted tool protocol text from leaking
// into Telegram's user-visible answer stream. Structured tool events still render
// through the status pane; this only strips literal XML-ish scaffolding that arrived
// as TEXT_MESSAGE_CONTENT.
func stripProtocolToolBlocks(s string) string {
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	var b strings.Builder
	pos := 0
	stripped := false
	for pos < len(s) {
		start := nextProtocolToolOpen(lower, pos)
		if start < 0 {
			b.WriteString(s[pos:])
			break
		}
		stripped = true
		b.WriteString(s[pos:start])
		end := nextProtocolToolClose(lower, start)
		if end < 0 {
			break
		}
		pos = end
	}
	out := b.String()
	if stripped {
		return strings.TrimSpace(out)
	}
	return out
}

func nextProtocolToolOpen(s string, from int) int {
	return firstIndexFrom(s, from, "<tool_call", "<tool_exec")
}

func nextProtocolToolClose(s string, from int) int {
	best := -1
	for _, tag := range []string{"</tool_call>", "</tool_exec>"} {
		if idx := strings.Index(s[from:], tag); idx >= 0 {
			end := from + idx + len(tag)
			if best < 0 || end < best {
				best = end
			}
		}
	}
	return best
}

func firstIndexFrom(s string, from int, needles ...string) int {
	best := -1
	for _, needle := range needles {
		if idx := strings.Index(s[from:], needle); idx >= 0 {
			idx += from
			if best < 0 || idx < best {
				best = idx
			}
		}
	}
	return best
}

func suppressPartialProtocolOpen(s string) string {
	lower := strings.ToLower(s)
	for _, opener := range []string{"<tool_call", "<tool_exec"} {
		maxSuffix := min(len(lower), len(opener)-1)
		for n := maxSuffix; n > 0; n-- {
			if strings.HasSuffix(lower, opener[:n]) {
				return strings.TrimSpace(s[:len(s)-n])
			}
		}
	}
	return s
}

// sendOpts builds the SendOptions carrying a parse mode (mode "" disables parsing).
func sendOpts(mode tele.ParseMode) *tele.SendOptions {
	return &tele.SendOptions{ParseMode: mode}
}

// isCantParseEntities reports whether err is a Bot-API 400 entity-parse rejection
// (the plain-text fallback trigger). It matches on the structured *tele.Error code
// + description, never a bare string match on an arbitrary error.
func isCantParseEntities(err error) bool {
	if err == nil {
		return false
	}
	var te *tele.Error
	if errors.As(err, &te) {
		return te.Code == 400 && strings.Contains(strings.ToLower(te.Description), canParseEntitiesMarker)
	}
	return strings.Contains(strings.ToLower(err.Error()), canParseEntitiesMarker)
}

// capRunes truncates s to at most n runes (the Bot-API text/caption ceiling),
// counting runes so a multi-byte payload is not cut mid-character.
func capRunes(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func splitTelegramText(s string, n int) []string {
	if s == "" || n <= 0 {
		return nil
	}
	var chunks []string
	rest := s
	for len([]rune(rest)) > n {
		cut := splitBoundary(rest, n)
		chunk := strings.TrimRight(rest[:cut], " \t\r\n")
		if chunk == "" {
			chunk = capRunes(rest, n)
			cut = len(chunk)
		}
		chunks = append(chunks, chunk)
		rest = strings.TrimLeft(rest[cut:], " \t\r\n")
	}
	if rest != "" {
		chunks = append(chunks, rest)
	}
	return chunks
}

func splitBoundary(s string, n int) int {
	runes := []rune(s)
	if len(runes) <= n {
		return len(s)
	}
	prefix := string(runes[:n])
	min := n / 2
	for _, sep := range []string{"\n\n", "\n", ". ", "! ", "? ", " "} {
		if idx := strings.LastIndex(prefix, sep); idx >= min {
			return idx + len(sep)
		}
	}
	return len(prefix)
}
