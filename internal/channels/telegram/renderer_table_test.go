package telegram

import (
	"strings"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"
)

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

func TestRendererTableCaptionUsesHTML(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)

	content := "*Dati* <ok>&\n\n| Citta | Temp |\n|---|---|\n| Cuneo | 12 |"
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
		t.Fatalf("no sendPhoto for table; calls=%+v", bot.recorded())
	}
	if photoCall.parseMode != tele.ModeHTML {
		t.Fatalf("photo caption parse mode = %q, want HTML", photoCall.parseMode)
	}
	for _, want := range []string{"<b>Dati</b>", "&lt;ok&gt;&amp;"} {
		if !strings.Contains(photoCall.text, want) {
			t.Errorf("caption missing %q in %q", want, photoCall.text)
		}
	}
}

func TestRendererTableCaptionCapsBeforeHTMLConversion(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)

	content := strings.Repeat("<", telegramCaptionCap) + "\n\n| Citta | Temp |\n|---|---|\n| Cuneo | 12 |"
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
		t.Fatalf("no sendPhoto for table; calls=%+v", bot.recorded())
	}
	if got := strings.Count(photoCall.text, "&lt;"); got != telegramCaptionCap {
		t.Fatalf("caption should cap raw text before HTML conversion: got %d escaped brackets, want %d", got, telegramCaptionCap)
	}
}

// TestRendererStreamedTableSentOnce proves a table reply that streams over several
// flushes is sent as EXACTLY ONE photo — not one per flush (the live "4 times same
// message" bug). The streamed prose msg #2 is deleted once the PNG supersedes it.
func TestRendererStreamedTableSentOnce(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot) // throttle 0 → every non-final flush fires

	// Mimics seq 18: prose, then a table, streamed across deltas, with END + a
	// trailing RUN_FINISHED + the channel-close flush (3 final flushes total).
	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m", "Ho ricevuto il percorso, Davide!\n\n"),
		events.NewTextMessageContentEvent("m", "| Dettaglio | Info |\n|---|---|\n"),
		events.NewTextMessageContentEvent("m", "| Itinerario | Anello Monte Tamone |\n"),
		events.NewTextMessageEndEvent("m"),
		events.NewRunFinishedEvent("t", "r"),
	})

	photos := 0
	for _, c := range bot.recorded() {
		if c.photo != nil {
			photos++
		}
	}
	if photos != 1 {
		t.Fatalf("a streamed table must send exactly ONE photo, got %d (the 4× duplicate bug)", photos)
	}
	if bot.deleteCount() < 1 {
		t.Error("the streamed prose msg #2 must be deleted once the table PNG supersedes it")
	}
}
