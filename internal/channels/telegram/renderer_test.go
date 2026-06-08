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
	photo     *tele.Photo
	edit      bool
}

// fakeBot is the botSender double: it records every Send/Edit and can be told to
// reject a MarkdownV2 send with a Bot-API 400 "can't parse entities" (the
// plain-text fallback trigger). Assertions read the recorded calls (the spike
// ground truth: the Send RESPONSE, never getUpdates).
type fakeBot struct {
	mu    sync.Mutex
	calls []sendCall

	// fail400OnMarkdownV2 makes the FIRST MarkdownV2 Send/Edit return a 400
	// can't-parse-entities; subsequent (plain-text) sends succeed.
	fail400OnMarkdownV2 bool
	tripped             bool

	nextID int
}

func newFakeBot() *fakeBot { return &fakeBot{} }

func (f *fakeBot) record(edit bool, what any, opts []any) (*tele.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	mode := parseModeOf(opts)
	c := sendCall{parseMode: mode, edit: edit}
	switch w := what.(type) {
	case string:
		c.text = w
	case *tele.Photo:
		c.photo = w
		c.text = w.Caption
	}

	if f.fail400OnMarkdownV2 && !f.tripped && mode == tele.ModeMarkdownV2 {
		f.tripped = true
		f.calls = append(f.calls, c)
		return nil, &tele.Error{Code: 400, Description: "Bad Request: can't parse entities: bad escape"}
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

// TestRendererStreamsAndFinalizes proves TEXT_MESSAGE_* content is sent
// MarkdownV2-escaped and finalized on RUN_FINISHED.
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
	if last.parseMode != tele.ModeMarkdownV2 {
		t.Errorf("final send parse mode = %q, want MarkdownV2", last.parseMode)
	}
	// "." is reserved → escaped to "\." in MarkdownV2.
	if !strings.Contains(last.text, `mondo\.`) {
		t.Errorf("final text not mdv2-escaped: %q", last.text)
	}
}

// TestRendererPlainTextFallbackOn400 proves a MarkdownV2 send that 400s with
// "can't parse entities" is resent WITHOUT ParseMode carrying the ORIGINAL
// (unescaped) text — the SC#2 guarantee. Asserts on the Send RESPONSE, not
// getUpdates.
func TestRendererPlainTextFallbackOn400(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	bot.fail400OnMarkdownV2 = true
	r := newTestRenderer(bot)

	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m1", "1 - 2 = -1."),
		events.NewTextMessageEndEvent("m1"),
	})

	calls := bot.recorded()
	if len(calls) < 2 {
		t.Fatalf("expected a failed mdv2 send + a plain-text resend, got %d calls", len(calls))
	}
	// First call: MarkdownV2 (the one that 400s).
	if calls[0].parseMode != tele.ModeMarkdownV2 {
		t.Errorf("first send parse mode = %q, want MarkdownV2", calls[0].parseMode)
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

// TestRendererTableToPhoto proves a markdown table in the content renders via
// sendPhoto (msg.Photo non-nil in the bot double) — the table→PNG path.
func TestRendererTableToPhoto(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)

	content := "Ecco i dati:\n\n| Città | Temp |\n|-------|------|\n| Cuneo | 12°C |\n| Roma  | 19°C |\n"
	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m1", content),
		events.NewTextMessageEndEvent("m1"),
	})

	var photoCall *sendCall
	for i := range bot.recorded() {
		c := bot.recorded()[i]
		if c.photo != nil {
			photoCall = &c
			break
		}
	}
	if photoCall == nil {
		t.Fatalf("no sendPhoto for a markdown table; calls=%+v", bot.recorded())
	}
	if photoCall.photo.FileReader == nil {
		t.Error("photo has no file reader (PNG bytes)")
	}
	// The prose framing the table goes into the caption.
	if !strings.Contains(photoCall.text, "Ecco i dati") {
		t.Errorf("caption missing prose framing: %q", photoCall.text)
	}
}

// TestRendererCapsAt4096 proves an over-long content is capped to the Bot-API
// 4096-rune ceiling.
func TestRendererCapsAt4096(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)

	big := strings.Repeat("a", 5000)
	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m1", big),
		events.NewTextMessageEndEvent("m1"),
	})

	calls := bot.recorded()
	last := calls[len(calls)-1]
	if n := len([]rune(last.text)); n > telegramTextCap {
		t.Errorf("sent text length = %d runes, want <= %d", n, telegramTextCap)
	}
}
