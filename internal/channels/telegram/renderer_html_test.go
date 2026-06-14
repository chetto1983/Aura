package telegram

import (
	"strings"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"
)

func TestRendererHTMLConvertsMarkdownAndEscapesRawTags(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)

	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m1", "*bold* _italic_ <script>&"),
		events.NewTextMessageEndEvent("m1"),
	})

	calls := bot.recorded()
	if len(calls) == 0 {
		t.Fatal("renderer made no send")
	}
	last := calls[len(calls)-1]
	if last.parseMode != tele.ModeHTML {
		t.Fatalf("parse mode = %q, want HTML", last.parseMode)
	}
	for _, want := range []string{"<b>bold</b>", "<i>italic</i>", "&lt;script&gt;&amp;"} {
		if !strings.Contains(last.text, want) {
			t.Errorf("HTML render missing %q in %q", want, last.text)
		}
	}
}

func TestRendererHTMLLeavesReservedMarkdownV2PunctuationPlain(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)

	content := "1 - 2 = -1. f(x) | {ok}!"
	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m1", content),
		events.NewTextMessageEndEvent("m1"),
	})

	calls := bot.recorded()
	if len(calls) == 0 {
		t.Fatal("renderer made no send")
	}
	last := calls[len(calls)-1]
	if strings.Contains(last.text, `\`) {
		t.Fatalf("HTML mode must not carry MarkdownV2 backslashes, got %q", last.text)
	}
	if !strings.Contains(last.text, content) {
		t.Fatalf("HTML mode changed plain punctuation: got %q want it to contain %q", last.text, content)
	}
}

func TestRendererHTMLNormalizesDeepResearchBlockMarkdown(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)

	content := "Ecco le notizie:\n\n---\n\n### Auto contro un cancello\nDettaglio.\n\n### Meteo\nSole.\n\n---"
	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m1", content),
		events.NewTextMessageEndEvent("m1"),
	})

	calls := bot.recorded()
	if len(calls) == 0 {
		t.Fatal("renderer made no send")
	}
	last := calls[len(calls)-1]
	if last.parseMode != tele.ModeHTML {
		t.Fatalf("parse mode = %q, want HTML", last.parseMode)
	}
	for _, bad := range []string{"###", "---"} {
		if strings.Contains(last.text, bad) {
			t.Fatalf("renderer leaked block markdown marker %q in %q", bad, last.text)
		}
	}
	for _, want := range []string{"<b>Auto contro un cancello</b>", "<b>Meteo</b>"} {
		if !strings.Contains(last.text, want) {
			t.Fatalf("renderer missing normalized heading %q in %q", want, last.text)
		}
	}
}
