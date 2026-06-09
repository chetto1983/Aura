package telegram

import (
	"strings"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

// TestRendererStripsProtocolToolBlocks proves that literal model-emitted tool
// scaffolding never reaches Telegram msg #2. Structured tool events still render
// through the status pane; raw XML-ish text is protocol noise.
func TestRendererStripsProtocolToolBlocks(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)

	raw := "<tool_call>\n<tool_name>shell_exec</tool_name>\n<arguments>{\"command\":\"curl -s https://example.test\"}</arguments>\n</tool_call>\nRisposta finale."
	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m1", raw[:40]),
		events.NewTextMessageContentEvent("m1", raw[40:]),
		events.NewTextMessageEndEvent("m1"),
	})

	if got := r.finalText(); got != "Risposta finale." {
		t.Fatalf("finalText = %q, want only the visible answer", got)
	}
	calls := bot.recorded()
	if len(calls) == 0 {
		t.Fatal("renderer did not send the visible answer")
	}
	for i, c := range calls {
		if strings.Contains(c.text, "tool_call") || strings.Contains(c.text, "shell_exec") || strings.Contains(c.text, "curl -s") {
			t.Fatalf("call %d leaked protocol text: %q", i, c.text)
		}
	}
	last := calls[len(calls)-1]
	if !strings.Contains(last.text, `Risposta finale.`) {
		t.Fatalf("final send missing answer: %q", last.text)
	}
}

func TestRendererSuppressesSplitProtocolOpenTag(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)

	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m1", "<to"),
		events.NewTextMessageContentEvent("m1", "ol_call>\n<tool_name>shell_exec</tool_name>\n</tool_call>\nVisible."),
		events.NewTextMessageEndEvent("m1"),
	})

	if got := r.finalText(); got != "Visible." {
		t.Fatalf("finalText = %q, want only the visible answer", got)
	}
	for i, c := range bot.recorded() {
		if strings.Contains(c.text, "&lt;to") || strings.Contains(c.text, "tool_call") || strings.Contains(c.text, "shell_exec") {
			t.Fatalf("call %d leaked a split protocol tag: %q", i, c.text)
		}
	}
}

func TestRendererSuppressesOpenProtocolToolBlock(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)

	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m1", "<tool_exec>\n<tool_name>shell_exec</tool_name>"),
		events.NewTextMessageEndEvent("m1"),
	})

	if got := r.finalText(); got != "" {
		t.Fatalf("finalText = %q, want empty for an unterminated protocol block", got)
	}
	if calls := bot.recorded(); len(calls) != 0 {
		t.Fatalf("unterminated protocol block must not be sent; calls=%+v", calls)
	}
}

func TestRendererKeepsFinalPartialProtocolPrefixAsText(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)

	driveRenderer(r, []events.Event{
		events.NewTextMessageContentEvent("m1", "Use the literal prefix <tool"),
		events.NewTextMessageEndEvent("m1"),
	})

	calls := bot.recorded()
	if len(calls) == 0 {
		t.Fatal("renderer did not send final literal text")
	}
	last := calls[len(calls)-1]
	if !strings.Contains(last.text, "&lt;tool") {
		t.Fatalf("final partial protocol prefix was not preserved/escaped: %q", last.text)
	}
}
