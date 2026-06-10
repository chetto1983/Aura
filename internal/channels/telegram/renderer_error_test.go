package telegram

import (
	"strings"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
)

// TestRendererRunErrorRendersSanitizedReason proves a RUN_ERROR is surfaced to the
// user as a sanitized failure reason instead of being dropped (H2). BEFORE the fix
// the consume switch had no RunErrorEvent case and the reason vanished, leaving only
// the status pane's bare "Stato: errore". The error carries a DSN, so the assertion
// also pins the agui.SanitizeString redaction (no credential leaks to the user).
func TestRendererRunErrorRendersSanitizedReason(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)

	driveRenderer(r, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewRunErrorEvent("connect failed: postgres://aura:s3cr3t@db:5432/aura"),
	})

	calls := bot.recorded()
	if len(calls) == 0 {
		t.Fatal("a RUN_ERROR must send a user-facing failure message, got no sends")
	}
	last := calls[len(calls)-1]
	if !strings.Contains(last.text, "Errore") {
		t.Errorf("the failure send must carry a reason, got %q", last.text)
	}
	if strings.Contains(last.text, "s3cr3t") || strings.Contains(last.text, "postgres://aura:") {
		t.Errorf("the failure send must be sanitized (no DSN/credential), got %q", last.text)
	}
	if !strings.Contains(last.text, "[redacted]") {
		t.Errorf("the DSN must collapse to a [redacted] marker, got %q", last.text)
	}
}

// TestRendererRunErrorEmptyMessageStillNotifies proves a RUN_ERROR with an empty
// message still sends a generic failure notice (the user is never left with only
// the status pane glyph), and never panics on the empty reason.
func TestRendererRunErrorEmptyMessageStillNotifies(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	r := newTestRenderer(bot)

	driveRenderer(r, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewRunErrorEvent(""),
	})

	calls := bot.recorded()
	if len(calls) == 0 {
		t.Fatal("an empty-message RUN_ERROR must still send a generic failure notice")
	}
	if !strings.Contains(calls[len(calls)-1].text, "Errore") {
		t.Errorf("the generic failure notice must mention the error, got %q", calls[len(calls)-1].text)
	}
}
