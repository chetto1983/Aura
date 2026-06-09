package telegram

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"
)

// sendCall records one Send/Edit invocation on the bot double for assertion.
type sendCall struct {
	text      string
	parseMode tele.ParseMode
	markup    *tele.ReplyMarkup
	photo     *tele.Photo
	edit      bool
}

// fakeBot is the botSender double: it records every Send/Edit and can be told to
// reject a parsed send with a Bot-API 400 "can't parse entities" (the
// plain-text fallback trigger). Assertions read the recorded calls (the spike
// ground truth: the Send RESPONSE, never getUpdates).
type fakeBot struct {
	mu    sync.Mutex
	calls []sendCall

	// fail400OnParsed makes the FIRST parsed Send/Edit return a 400
	// can't-parse-entities; subsequent (plain-text) sends succeed.
	fail400OnParsed bool
	tripped         bool

	// failPhoto makes every *tele.Photo send return a hard (non-400) error so the
	// sendTable path returns false and the caller falls back to a text send.
	failPhoto bool

	nextID  int
	deletes int
}

func newFakeBot() *fakeBot { return &fakeBot{} }

func (f *fakeBot) record(edit bool, what any, opts []any) (*tele.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	mode := parseModeOf(opts)
	c := sendCall{parseMode: mode, markup: markupOf(opts), edit: edit}
	switch w := what.(type) {
	case string:
		c.text = w
	case *tele.Photo:
		c.photo = w
		c.text = w.Caption
	}

	if f.fail400OnParsed && !f.tripped && mode != tele.ModeDefault {
		f.tripped = true
		f.calls = append(f.calls, c)
		return nil, &tele.Error{Code: 400, Description: "Bad Request: can't parse entities: bad escape"}
	}

	if f.failPhoto {
		if _, isPhoto := what.(*tele.Photo); isPhoto {
			f.calls = append(f.calls, c)
			// A non-400 hard error: NOT a parse-entities fallback, so sendTable bails.
			return nil, &tele.Error{Code: 413, Description: "Request Entity Too Large"}
		}
	}

	f.calls = append(f.calls, c)
	f.nextID++
	return &tele.Message{ID: f.nextID}, nil
}

func (f *fakeBot) Send(_ tele.Recipient, what any, opts ...any) (*tele.Message, error) {
	return f.record(false, what, opts)
}

func (f *fakeBot) Edit(_ tele.Editable, what any, opts ...any) (*tele.Message, error) {
	return f.record(true, what, opts)
}

func (f *fakeBot) recorded() []sendCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]sendCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// Delete records a message deletion (the streamed text msg #2 superseded by a table
// PNG). Satisfies botDeleter so the renderer's cleanup runs under test.
func (f *fakeBot) Delete(_ tele.Editable) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes++
	return nil
}

func (f *fakeBot) deleteCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deletes
}

// parseModeOf extracts the parse mode from a variadic opts slice carrying a
// *tele.SendOptions.
func parseModeOf(opts []any) tele.ParseMode {
	for _, o := range opts {
		if so, ok := o.(*tele.SendOptions); ok {
			return so.ParseMode
		}
		if pm, ok := o.(tele.ParseMode); ok {
			return pm
		}
	}
	return tele.ModeDefault
}

// driveRenderer feeds a slice of events into a renderer over a buffered channel,
// closing the channel so consume terminates.
func driveRenderer(r *renderer, evs []events.Event) {
	ch := make(chan events.Event, len(evs))
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	r.consume(context.Background(), ch)
}

// newTestRenderer builds a renderer with the throttle disabled (zero) and a manual
// clock so every flush fires immediately (timing is tested separately with synctest).
func newTestRenderer(bot botSender) *renderer {
	r := newRenderer(bot, tele.ChatID(99), 0, 0)
	r.now = func() time.Time { return time.Unix(0, 0) }
	r.sleep = func(time.Duration) {}
	return r
}

// TestRendererStreamsAndFinalizes proves TEXT_MESSAGE_* content is sent as
// Telegram HTML and finalized on RUN_FINISHED.
func TestRendererStreamsAndFinalizes(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)

	driveRenderer(r, []events.Event{
		events.NewTextMessageStartEvent("m1"),
		events.NewTextMessageContentEvent("m1", "Ciao "),
		events.NewTextMessageContentEvent("m1", "mondo."),
		events.NewTextMessageEndEvent("m1"),
		events.NewRunFinishedEvent("t", "r"),
	})

	calls := bot.recorded()
	if len(calls) == 0 {
		t.Fatal("renderer made no send")
	}
	last := calls[len(calls)-1]
	if last.parseMode != tele.ModeHTML {
		t.Errorf("final send parse mode = %q, want HTML", last.parseMode)
	}
	if !strings.Contains(last.text, `mondo.`) {
		t.Errorf("final text missing plain punctuation in HTML mode: %q", last.text)
	}
}

// TestRendererPlainTextFallbackOn400 proves a parsed HTML send that 400s with
// "can't parse entities" is resent WITHOUT ParseMode carrying the ORIGINAL
// (unescaped) text — the SC#2 guarantee. Asserts on the Send RESPONSE, not
// getUpdates.
func TestRendererPlainTextFallbackOn400(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	bot.fail400OnParsed = true
	r := newTestRenderer(bot)

	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m1", "1 - 2 = -1."),
		events.NewTextMessageEndEvent("m1"),
	})

	calls := bot.recorded()
	if len(calls) < 2 {
		t.Fatalf("expected a failed parsed send + a plain-text resend, got %d calls", len(calls))
	}
	// First call: HTML (the one that 400s).
	if calls[0].parseMode != tele.ModeHTML {
		t.Errorf("first send parse mode = %q, want HTML", calls[0].parseMode)
	}
	// The fallback resend: ModeDefault (no ParseMode) carrying the ORIGINAL text.
	fb := calls[len(calls)-1]
	if fb.parseMode != tele.ModeDefault {
		t.Errorf("fallback parse mode = %q, want ModeDefault (no ParseMode)", fb.parseMode)
	}
	if fb.text != "1 - 2 = -1." {
		t.Errorf("fallback text = %q, want the ORIGINAL unescaped text", fb.text)
	}
	if strings.Contains(fb.text, `\`) {
		t.Errorf("fallback text must not be escaped: %q", fb.text)
	}
}

// TestRendererSplitsOver4096PreservesTail proves an over-long answer is split
// into Telegram-sized messages instead of silently truncating the tail.
func TestRendererSplitsOver4096PreservesTail(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)

	big := strings.Repeat("a", telegramTextCap) + "TAIL"
	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m1", big),
		events.NewTextMessageEndEvent("m1"),
	})

	calls := bot.recorded()
	var sawTail bool
	for _, c := range calls {
		if n := len([]rune(c.text)); n > telegramTextCap {
			t.Errorf("sent text length = %d runes, want <= %d", n, telegramTextCap)
		}
		if strings.Contains(c.text, "TAIL") {
			sawTail = true
		}
	}
	if !sawTail {
		t.Fatalf("over-long answer tail was lost; calls=%+v", calls)
	}
}

// rendererAt builds a renderer with a fixed clock and a recording sleep so flush()
// and rateLimit() can be unit-tested deterministically at exact clock offsets.
func rendererAt(throttle, chatRate time.Duration, nowT, lastSend time.Time, buf string) (*renderer, *fakeBot, *[]time.Duration) {
	bot := newFakeBot()
	r := newRenderer(bot, tele.ChatID(1), throttle, chatRate)
	var slept []time.Duration
	r.now = func() time.Time { return nowT }
	r.sleep = func(d time.Duration) { slept = append(slept, d) }
	r.lastSend = lastSend
	if buf != "" {
		r.buf.WriteString(buf)
	}
	return r, bot, &slept
}

// TestRendererFlushThrottle exercises the coalescing window at the boundary: a
// non-final flush strictly inside the window is dropped, one exactly AT the window
// edge sends (kills `<`→`<=`), a FINAL flush inside the window still sends (kills
// the `!final` guard), and empty content never sends (kills the `content==""` guard).
func TestRendererFlushThrottle(t *testing.T) {
	t.Parallel()
	const throttle = 100 * time.Millisecond
	base := time.Unix(1000, 0)
	ctx := context.Background()

	r, bot, _ := rendererAt(throttle, 0, base.Add(50*time.Millisecond), base, "hi")
	r.flush(ctx, false)
	if n := len(bot.recorded()); n != 0 {
		t.Errorf("within-window non-final flush must coalesce; got %d sends", n)
	}

	r, bot, _ = rendererAt(throttle, 0, base.Add(throttle), base, "hi")
	r.flush(ctx, false)
	if n := len(bot.recorded()); n != 1 {
		t.Errorf("at-boundary non-final flush must send; got %d", n)
	}

	r, bot, _ = rendererAt(throttle, 0, base.Add(10*time.Millisecond), base, "hi")
	r.flush(ctx, true)
	if n := len(bot.recorded()); n != 1 {
		t.Errorf("final flush must send even inside the window; got %d", n)
	}

	r, bot, _ = rendererAt(throttle, 0, base.Add(throttle), base, "")
	r.flush(ctx, true)
	if n := len(bot.recorded()); n != 0 {
		t.Errorf("empty content must not send; got %d", n)
	}
}

// TestRendererRateLimit exercises the per-chat rate gate: it sleeps the remaining
// window inside it, never sleeps exactly at the edge (kills `>0`→`>=0`), never
// sleeps once the window elapsed (kills `>0`→`true`), and never sleeps under a
// cancelled context (kills the `&& ctx.Err()==nil` guard).
func TestRendererRateLimit(t *testing.T) {
	t.Parallel()
	const rate = 200 * time.Millisecond
	base := time.Unix(2000, 0)
	ctx := context.Background()

	r, _, slept := rendererAt(0, rate, base.Add(50*time.Millisecond), base, "")
	r.rateLimit(ctx)
	if len(*slept) != 1 || (*slept)[0] != 150*time.Millisecond {
		t.Errorf("inside window must sleep the 150ms remainder; got %v", *slept)
	}

	r, _, slept = rendererAt(0, rate, base.Add(rate), base, "")
	r.rateLimit(ctx)
	if len(*slept) != 0 {
		t.Errorf("at the window edge wait==0 must not sleep; got %v", *slept)
	}

	r, _, slept = rendererAt(0, rate, base.Add(2*rate), base, "")
	r.rateLimit(ctx)
	if len(*slept) != 0 {
		t.Errorf("elapsed window must not sleep; got %v", *slept)
	}

	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, _, slept = rendererAt(0, rate, base.Add(50*time.Millisecond), base, "")
	r.rateLimit(cctx)
	if len(*slept) != 0 {
		t.Errorf("cancelled ctx must not sleep; got %v", *slept)
	}
}

// TestCapRunesBoundary pins the rune ceiling at exactly n (kills `<= n`→`< n`):
// a string of exactly n multibyte runes is untouched; n+1 truncates to n.
func TestCapRunesBoundary(t *testing.T) {
	t.Parallel()
	exact := strings.Repeat("é", telegramTextCap) // multibyte → also proves rune (not byte) counting
	if got := capRunes(exact, telegramTextCap); got != exact {
		t.Errorf("exactly-n runes must be returned unchanged (len got %d runes)", len([]rune(got)))
	}
	over := strings.Repeat("a", telegramTextCap+1)
	if got := capRunes(over, telegramTextCap); len([]rune(got)) != telegramTextCap {
		t.Errorf("n+1 must truncate to n; got %d runes", len([]rune(got)))
	}
}

// TestIsCantParseEntitiesDiscriminates pins the fallback trigger to a 400 whose
// description contains the marker — a 400 with a different description, a non-400
// carrying the marker, and nil all return false.
func TestIsCantParseEntitiesDiscriminates(t *testing.T) {
	t.Parallel()
	if !isCantParseEntities(&tele.Error{Code: 400, Description: "Bad Request: can't parse entities: bad escape"}) {
		t.Error("a 400 can't-parse-entities must trigger the fallback")
	}
	if isCantParseEntities(&tele.Error{Code: 400, Description: "Bad Request: message is too long"}) {
		t.Error("a 400 with a different description must NOT trigger the fallback")
	}
	if isCantParseEntities(&tele.Error{Code: 500, Description: "can't parse entities"}) {
		t.Error("a non-400 must NOT trigger the fallback even with the marker text")
	}
	if isCantParseEntities(nil) {
		t.Error("nil error must NOT trigger the fallback")
	}
}

// TestTableCaptionFiltering pins the prose extraction: table rows (lines with `|`)
// and blank lines are dropped, surviving prose lines join with a single newline
// (kills the `continue` skips, the `b.WriteByte('\n')`, and `b.Len() > 0`), and a
// table-only content yields an empty caption.
func TestTableCaptionFiltering(t *testing.T) {
	t.Parallel()
	got := tableCaption("Intro line\n\n| a | b |\n|---|---|\n| 1 | 2 |\nOutro line")
	if got != "Intro line\nOutro line" {
		t.Errorf("prose lines must join with one newline, table+blank lines dropped; got %q", got)
	}
	if c := tableCaption("| a | b |\n|---|---|\n| 1 | 2 |"); c != "" {
		t.Errorf("table-only content must yield an empty caption; got %q", c)
	}
}

// TestRendererConsumeFinalizesCoalescedTail proves a delta coalesced inside the
// throttle window is still delivered when the channel closes with NO END/FINISHED
// frame (kills the trailing post-loop flush removal).
func TestRendererConsumeFinalizesCoalescedTail(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newRenderer(bot, tele.ChatID(1), 100*time.Millisecond, 0)
	now := time.Unix(1000, 0)
	r.now = func() time.Time { return now } // constant clock → the 2nd delta coalesces
	r.sleep = func(time.Duration) {}

	ch := make(chan events.Event, 2)
	ch <- events.NewTextMessageContentEvent("m", "head ")
	ch <- events.NewTextMessageContentEvent("m", "tail.")
	close(ch) // no TEXT_MESSAGE_END / RUN_FINISHED
	r.consume(context.Background(), ch)

	calls := bot.recorded()
	if len(calls) == 0 || !strings.Contains(calls[len(calls)-1].text, "tail") {
		t.Errorf("a coalesced tail must be flushed on channel close; calls=%+v", calls)
	}
}

// TestRendererTableCaptionPlainFallback proves a markdown table whose HTML
// caption 400s is resent as a photo with a PLAIN caption (kills the sendTable
// caption-fallback removal).
func TestRendererTableCaptionPlainFallback(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	bot.fail400OnParsed = true
	r := newTestRenderer(bot)

	content := "Prosa con - trattino.\n\n| Città | Temp |\n|-------|------|\n| Cuneo | 12 |"
	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m", content),
		events.NewTextMessageEndEvent("m"),
	})

	var plainPhoto *sendCall
	for i := range bot.recorded() {
		c := bot.recorded()[i]
		if c.photo != nil && c.parseMode == tele.ModeDefault {
			plainPhoto = &c
		}
	}
	if plainPhoto == nil {
		t.Fatalf("a table caption that 400s must resend the photo with a plain caption; calls=%+v", bot.recorded())
	}
	// The resent caption must be the UNESCAPED plain text, not the HTML one
	// (kills the `photo.Caption = capRunes(PlainTextFallback(...))` reassignment).
	if strings.Contains(plainPhoto.text, `\`) {
		t.Errorf("plain-fallback photo caption must be unescaped; got %q", plainPhoto.text)
	}
}

// TestRendererConsumeStopsOnCancelledCtx proves the consume loop returns without
// rendering when the context is already cancelled (kills the `if ctx.Err() != nil
// { return }` guard in the loop).
func TestRendererConsumeStopsOnCancelledCtx(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ch := make(chan events.Event, 2)
	ch <- events.NewTextMessageContentEvent("m", "x")
	ch <- events.NewTextMessageContentEvent("m", "y")
	close(ch)
	r.consume(ctx, ch)

	if n := len(bot.recorded()); n != 0 {
		t.Errorf("a pre-cancelled context must stop the consume loop before any send; got %d sends", n)
	}
}

// TestRendererPhotoResetsMsg proves a table photo resets msg #2 to nil so a later
// text flush opens a FRESH message instead of editing the photo (kills the
// `r.msg = nil` reset in sendTable).
func TestRendererPhotoResetsMsg(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)
	r.msg = &tele.Message{ID: 42} // a prior streamed text msg #2

	r.send("Prosa.\n| a | b |\n|---|---|\n| 1 | 2 |", true) // a table on the final flush → sendPhoto

	if r.msg != nil {
		t.Error("after a table photo, msg must reset to nil so a following text opens a fresh message")
	}
}

// TestRendererFlushAppliesRateLimit proves flush() applies the per-chat rate limit
// (kills the rateLimit() call removal inside flush): a finalize flush whose last
// send was inside the rate window sleeps the remainder before sending.
func TestRendererFlushAppliesRateLimit(t *testing.T) {
	t.Parallel()
	base := time.Unix(3000, 0)
	// lastSend == now → fully inside the 200ms rate window → flush must sleep.
	r, _, slept := rendererAt(0, 200*time.Millisecond, base, base, "hello")
	r.flush(context.Background(), true)
	if len(*slept) == 0 {
		t.Error("flush must apply the per-chat rate limit (sleep inside the window)")
	}
}

// TestRendererPhotoHardFailFallsBackToText proves a table whose photo send hard-fails
// (a non-400 error, so NOT the caption fallback) falls through to a plain text send
// (kills the sendTable `return false` + the send() fallthrough).
func TestRendererPhotoHardFailFallsBackToText(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	bot.failPhoto = true
	r := newTestRenderer(bot)

	content := "Dati:\n| a | b |\n|---|---|\n| 1 | 2 |"
	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m", content),
		events.NewTextMessageEndEvent("m"),
	})

	var sawPhoto, sawText bool
	for _, c := range bot.recorded() {
		if c.photo != nil {
			sawPhoto = true
		} else if c.text != "" {
			sawText = true
		}
	}
	if !sawPhoto {
		t.Fatal("expected a (failed) photo attempt for the table")
	}
	if !sawText {
		t.Error("a hard-failed photo send must fall back to a text send")
	}
}

// TestRendererTracksMsgForEdits proves msg #2 is tracked: the first delivery is a
// fresh Send and every subsequent delivery Edits that message in place (kills the
// `r.msg = out` tracking removal and the deliver Send/Edit branch selection).
func TestRendererTracksMsgForEdits(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot) // throttle 0 → every delta flushes immediately

	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m", "one "),
		events.NewTextMessageContentEvent("m", "two"),
		events.NewTextMessageEndEvent("m"),
	})

	calls := bot.recorded()
	if len(calls) < 2 {
		t.Fatalf("expected at least a Send + an Edit, got %d", len(calls))
	}
	if calls[0].edit {
		t.Error("the first delivery must be a Send (msg #2 opened), not an Edit")
	}
	if !calls[1].edit {
		t.Error("subsequent deliveries must Edit msg #2 in place (msg must be tracked)")
	}
}

// TestRendererPlainLatchAndMsgTracking proves a 400 latches plain mode for the rest
// of the message and that msg #2 is tracked after a successful send (kills the
// `r.plain = true` latch and `r.msg = out` tracking removals).
func TestRendererPlainLatchAndMsgTracking(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	bot.fail400OnParsed = true
	r := newTestRenderer(bot)

	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m", "first - line."),
		events.NewTextMessageContentEvent("m", " more."),
		events.NewTextMessageEndEvent("m"),
	})

	if !r.plain {
		t.Error("plain mode must latch after a 400 can't-parse-entities")
	}
	if r.msg == nil {
		t.Error("msg #2 must be tracked after a successful send")
	}
	// Every send AFTER the latch must be ModeDefault (plain), never parsed HTML.
	calls := bot.recorded()
	for i, c := range calls {
		if i == 0 {
			continue // the first parsed send is the one that 400s
		}
		if c.parseMode != tele.ModeDefault {
			t.Errorf("call %d after the 400 latch used parse mode %q; must stay plain", i, c.parseMode)
		}
	}
}
