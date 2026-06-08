// Package telegram — this file is the content renderer consumer (msg #2, the
// streamed answer). It consumes the TEXT_MESSAGE_* / TOOL_CALL_RESULT family the
// AG-UI translator produces and renders them to Telegram with three guarantees:
//
//   - MarkdownV2-escaped sends (mdv2.go) with a PLAIN-TEXT FALLBACK on a Bot-API
//     400 "can't parse entities" — the SC#2 "no can't parse entities" guarantee:
//     a rejected MarkdownV2 send is resent WITHOUT ParseMode.
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
)

// Bot-API constraints (spike telegram-channel.md §Constraints).
const (
	telegramTextCap    = 4096
	telegramCaptionCap = 1024
)

// canParseEntitiesMarker is the Bot-API 400 description substring that signals a
// MarkdownV2 entity-parse rejection — the plain-text fallback trigger.
const canParseEntitiesMarker = "can't parse entities"

// renderer streams msg #2 for one turn: TEXT_MESSAGE_* deltas accumulate into the
// content message, sent MarkdownV2-escaped with a plain-text fallback, throttled
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

	buf      strings.Builder // accumulated content for the open text message
	msg      *tele.Message   // msg #2 once first sent; edited in place thereafter
	lastSend time.Time       // last successful send/edit (throttle + rate-limit gate)
	plain    bool            // once a 400 forced the plain-text fallback, stay plain for this message
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
	return r.buf.String()
}

// flush sends/edits msg #2 with the accumulated content. final forces a send even
// if the throttle window has not elapsed (the END/RUN_FINISHED finalization). A
// non-final flush inside the throttle window is skipped (coalesced).
func (r *renderer) flush(ctx context.Context, final bool) {
	content := r.buf.String()
	if content == "" {
		return
	}
	if !final && r.now().Sub(r.lastSend) < r.throttle {
		return // coalesce: within the throttle window, wait for the next delta/END
	}
	r.rateLimit(ctx)
	r.send(content)
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
// otherwise the text is MarkdownV2-escaped and sent/edited, with the plain-text
// fallback on a 400 can't-parse-entities.
func (r *renderer) send(content string) {
	if grid, ok := ParseMarkdownTable(content); ok {
		if r.sendTable(grid, content) {
			return
		}
		// table render failed → fall through to text rendering of the raw content
	}
	r.sendText(content)
}

// sendText sends or edits msg #2 with MarkdownV2, capping at the 4096 ceiling. On a
// Bot-API 400 "can't parse entities" it resends the ORIGINAL text WITHOUT ParseMode
// (PlainTextFallback) — the SC#2 guarantee.
func (r *renderer) sendText(content string) {
	capped := capRunes(content, telegramTextCap)

	// Once a 400 forced the fallback for this message, stay in plain mode: editing
	// it back to MarkdownV2 would just 400 again (the content only grows).
	if r.plain {
		if out, err := r.deliver(PlainTextFallback(capped), tele.ModeDefault); err == nil && out != nil {
			r.msg = out
		}
		return
	}

	out, err := r.deliver(EscapeMarkdownV2(capped), tele.ModeMarkdownV2)
	if isCantParseEntities(err) {
		// Resend the unescaped original without ParseMode (plain-text fallback) and
		// latch plain mode so subsequent edits of this message stay plain.
		r.plain = true
		out, err = r.deliver(PlainTextFallback(capped), tele.ModeDefault)
	}
	if err != nil {
		return // best-effort: a failed content send must not wedge the turn
	}
	if out != nil {
		r.msg = out
	}
}

// deliver sends a fresh msg #2 or edits the existing one with the given parse mode.
func (r *renderer) deliver(text, mode tele.ParseMode) (*tele.Message, error) {
	opts := sendOpts(mode)
	if r.msg == nil {
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
	caption := capRunes(EscapeMarkdownV2(tableCaption(content)), telegramCaptionCap)
	photo := &tele.Photo{
		File:    tele.FromReader(bytes.NewReader(png)),
		Caption: caption,
	}
	out, sendErr := r.bot.Send(r.to, photo, sendOpts(tele.ModeMarkdownV2))
	if isCantParseEntities(sendErr) {
		photo.Caption = capRunes(PlainTextFallback(tableCaption(content)), telegramCaptionCap)
		out, sendErr = r.bot.Send(r.to, photo, sendOpts(tele.ModeDefault))
	}
	if sendErr != nil {
		return false
	}
	// A photo is a NEW message (not an edit of the streamed text msg #2); reset msg
	// so a following text flush opens a fresh message rather than editing the photo.
	r.msg = nil
	_ = out
	return true
}

// tableCaption returns the prose surrounding a markdown table (the lines that are
// not part of the table grid), so the sendPhoto caption carries the model's framing
// text. Empty when the content is table-only.
func tableCaption(content string) string {
	var b strings.Builder
	for _, line := range strings.Split(content, "\n") {
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
