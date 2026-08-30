package telegram

import (
	"strings"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"

	"github.com/chetto1983/aura/internal/agui"
)

// TestRendererDiscardDropsStreamedDraft (amendment #191): a vetoed draft was
// streamed and then repudiated with an aura.discard CUSTOM; the answer that follows
// must be the whole of msg #2 (and of the TTS text), never appended to the draft.
func TestRendererDiscardDropsStreamedDraft(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)

	driveRenderer(r, []events.Event{
		events.NewTextMessageStartEvent("m1"),
		events.NewTextMessageContentEvent("m1", "Il budget è esaurito, bozza scartata."),
		events.NewTextMessageEndEvent("m1"),
		events.NewCustomEvent(agui.DiscardEventName, events.WithValue(map[string]any{"message_id": "m1"})),
		events.NewTextMessageStartEvent("m2"),
		events.NewTextMessageContentEvent("m2", "Ecco i risultati."),
		events.NewTextMessageEndEvent("m2"),
		events.NewRunFinishedEvent("t", "r"),
	})

	calls := bot.recorded()
	if len(calls) == 0 {
		t.Fatal("renderer made no send")
	}
	last := calls[len(calls)-1]
	if strings.Contains(last.text, "bozza scartata") || !strings.Contains(last.text, "Ecco i risultati.") {
		t.Fatalf("final msg #2 text = %q, want only the answer that followed the discard", last.text)
	}
	if got := r.finalText(); got != "Ecco i risultati." {
		t.Fatalf("finalText = %q, want the answer alone (it feeds TTS)", got)
	}
}
