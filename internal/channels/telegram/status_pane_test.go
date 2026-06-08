package telegram

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"
)

// updateGolden regenerates the status-pane golden fixtures (go test -run StatusPane
// -update). Off by default so a CI run compares against the checked-in goldens.
var updateGolden = flag.Bool("update", false, "update golden fixtures")

// drivePane feeds events into a status pane over a buffered channel and returns the
// LAST rendered pane text (the final coalesced state). Throttle is disabled (zero)
// and the clock is fixed so every event renders.
func drivePane(bot *fakeBot, evs []events.Event) {
	p := newStatusPane(bot, tele.ChatID(7), 0)
	p.now = func() time.Time { return time.Unix(0, 0) }
	p.sleep = func(time.Duration) {}
	ch := make(chan events.Event, len(evs))
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	p.consume(context.Background(), ch)
}

// lastText returns the text of the last Send/Edit the pane made.
func lastText(bot *fakeBot) string {
	calls := bot.recorded()
	if len(calls) == 0 {
		return ""
	}
	return calls[len(calls)-1].text
}

// goldenCases enumerates one render per AG-UI event family the status pane handles,
// plus the microcompact-pointer tool result. Each compares the final pane text to a
// checked-in golden so a render regression is caught.
func goldenCases() map[string][]events.Event {
	return map[string][]events.Event{
		"run_started": {
			events.NewRunStartedEvent("t", "r"),
		},
		"tool_running": {
			events.NewRunStartedEvent("t", "r"),
			events.NewToolCallStartEvent("c1", "web_search"),
		},
		"tool_ok": {
			events.NewRunStartedEvent("t", "r"),
			events.NewToolCallStartEvent("c1", "web_search"),
			events.NewToolCallResultEvent("m", "c1", "3 results found"),
		},
		"tool_fail": {
			events.NewRunStartedEvent("t", "r"),
			events.NewToolCallStartEvent("c1", "web_fetch"),
			events.NewToolCallResultEvent("m", "c1", "error: connection refused"),
		},
		"reasoning": {
			events.NewRunStartedEvent("t", "r"),
			events.NewReasoningMessageContentEvent("rsn1", "Sto cercando "),
			events.NewReasoningMessageContentEvent("rsn1", "i dati meteo."),
		},
		"cost_footer": {
			events.NewRunStartedEvent("t", "r"),
			events.NewStateDeltaEvent([]events.JSONPatchOperation{
				{Op: "replace", Path: "/cost_usd", Value: "0.0012"},
			}),
		},
		"run_error": {
			events.NewRunStartedEvent("t", "r"),
			events.NewToolCallStartEvent("c1", "web_search"),
			events.NewRunErrorEvent("model unavailable"),
		},
		// Microcompact pointer: a tool result preview carrying the read_tool_output
		// truncation footer is a SUCCESSFUL (just-truncated) result → ✅, NOT ❌. The
		// pointer text must not be misread as an error (PRD acceptance fixture).
		"microcompact_pointer": {
			events.NewRunStartedEvent("t", "r"),
			events.NewToolCallStartEvent("c1", "fs_read"),
			events.NewToolCallResultEvent("m", "c1",
				"file head...\n\n[output truncated: showing bytes 0-2048 of 99999; read more via read_tool_output(tool_call_id=\"c1\", offset=2048, limit=2048)]"),
		},
	}
}

// TestStatusPaneGoldenPerEvent renders one fixture per event family and compares the
// final pane text to a checked-in golden (UX-02 "AG-UI events → status-pane-B render
// ... incl. microcompact pointer").
func TestStatusPaneGoldenPerEvent(t *testing.T) {
	for name, evs := range goldenCases() {
		t.Run(name, func(t *testing.T) {
			bot := newFakeBot()
			drivePane(bot, evs)
			got := lastText(bot)

			golden := filepath.Join("testdata", "statuspane_"+name+".golden")
			if *updateGolden {
				if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden %s (run -update to create): %v", golden, err)
			}
			if got != string(want) {
				t.Errorf("pane text mismatch\n got: %q\nwant: %q", got, string(want))
			}
		})
	}
}

// TestStatusPaneMicrocompactPointerIsOK asserts directly (golden-independent) that
// the microcompact pointer resolves the tool to ✅, never ❌.
func TestStatusPaneMicrocompactPointerIsOK(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	drivePane(bot, goldenCases()["microcompact_pointer"])
	got := lastText(bot)
	if !containsRune(got, '✅') {
		t.Errorf("microcompact pointer tool not marked OK: %q", got)
	}
	if containsRune(got, '❌') {
		t.Errorf("microcompact pointer tool wrongly marked failed: %q", got)
	}
}

func TestStatusPaneCancelButtonLifecycle(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	drivePane(bot, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewRunFinishedEvent("t", "r"),
	})
	calls := bot.recorded()
	if len(calls) < 2 {
		t.Fatalf("status pane should send then finalize, got %d calls", len(calls))
	}
	start := calls[0].markup
	if start == nil || len(start.InlineKeyboard) == 0 {
		t.Fatalf("running status pane must include a cancel inline keyboard, got %+v", start)
	}
	if got := start.InlineKeyboard[0][0].Unique; got != statusCancelUnique {
		t.Fatalf("cancel button unique = %q, want %q", got, statusCancelUnique)
	}
	final := calls[len(calls)-1].markup
	if final == nil {
		t.Fatalf("final status pane edit must carry empty markup to remove the cancel button")
	}
	if len(final.InlineKeyboard) != 0 {
		t.Fatalf("final status pane markup must be empty, got %+v", final.InlineKeyboard)
	}
}

func TestStatusPaneCollapsesSuccessfulToolListOnFinish(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	drivePane(bot, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewToolCallStartEvent("c1", "web_search"),
		events.NewToolCallResultEvent("m", "c1", "ok"),
		events.NewToolCallStartEvent("c2", "fs_read"),
		events.NewToolCallResultEvent("m", "c2", "ok"),
		events.NewRunFinishedEvent("t", "r"),
	})
	got := lastText(bot)
	if strings.Contains(got, "web_search") || strings.Contains(got, "fs_read") {
		t.Fatalf("successful finished pane should collapse tool names, got %q", got)
	}
	if !strings.Contains(got, "2 strumenti") {
		t.Fatalf("successful finished pane should summarize tool count, got %q", got)
	}
}

func TestStatusPaneKeepsFailedToolDetailOnFinish(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	drivePane(bot, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewToolCallStartEvent("c1", "web_fetch"),
		events.NewToolCallResultEvent("m", "c1", "error: timeout"),
		events.NewRunFinishedEvent("t", "r"),
	})
	got := lastText(bot)
	if !strings.Contains(got, "web_fetch") || !containsRune(got, '❌') {
		t.Fatalf("failed finished pane should keep diagnostic tool detail, got %q", got)
	}
}

// TestStatusPaneReasoningSurvivesFinishWithLatestSnippet proves the reasoning line
// remains useful after the final answer lands: compact, recent, and with the cost
// footer still intact.
func TestStatusPaneReasoningSurvivesFinishWithLatestSnippet(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	oldMarker := "VERY-OLD-MARKER"
	latest := "latest useful detail"
	drivePane(bot, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewReasoningMessageContentEvent("rsn1", oldMarker+" "+strings.Repeat("stale context ", 30)),
		events.NewReasoningMessageContentEvent("rsn1", latest),
		events.NewStateDeltaEvent([]events.JSONPatchOperation{
			{Op: "replace", Path: "/cost_usd", Value: "0.0012"},
		}),
		events.NewRunFinishedEvent("t", "r"),
	})
	got := lastText(bot)
	if !containsRune(got, '💭') {
		t.Errorf("reasoning line must survive RUN_FINISHED, got: %q", got)
	}
	if !strings.Contains(got, latest) {
		t.Errorf("reasoning line must keep the latest useful detail, got: %q", got)
	}
	if strings.Contains(got, oldMarker) {
		t.Errorf("reasoning line must prefer recent context over old opening text, got: %q", got)
	}
	if !strings.Contains(got, "0.0012") {
		t.Errorf("cost footer must survive RUN_FINISHED, got: %q", got)
	}
}

// TestStatusPaneThrottleCoalesces proves the status throttle coalesces edits to the
// 1500ms window: many fast tool events inside one window produce ONE edit, and a
// later event past the window produces another (VALIDATION "throttle via synctest").
func TestStatusPaneThrottleCoalesces(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		bot := newFakeBot()
		throttle := 1500 * time.Millisecond
		p := newStatusPane(bot, tele.ChatID(7), throttle)

		ch := make(chan events.Event)
		done := make(chan struct{})
		go func() {
			defer close(done)
			p.consume(context.Background(), ch)
		}()

		// First event renders immediately (lastEdit was zero).
		ch <- events.NewRunStartedEvent("t", "r")
		ch <- events.NewToolCallStartEvent("c1", "a")
		synctest.Wait()
		first := len(bot.recorded())
		if first != 1 {
			t.Fatalf("expected 1 edit for the first window, got %d", first)
		}

		// Burst of events INSIDE the throttle window: all coalesced, no new edit.
		ch <- events.NewToolCallStartEvent("c2", "b")
		ch <- events.NewToolCallStartEvent("c3", "c")
		synctest.Wait()
		if got := len(bot.recorded()); got != first {
			t.Fatalf("events inside the window were not coalesced: %d edits, want %d", got, first)
		}

		// Advance past the window, then a new event fires exactly one more edit.
		time.Sleep(throttle + time.Millisecond)
		ch <- events.NewToolCallStartEvent("c4", "d")
		synctest.Wait()
		if got := len(bot.recorded()); got != first+1 {
			t.Fatalf("event past the window did not produce a new edit: %d, want %d", got, first+1)
		}

		close(ch)
		<-done
	})
}

// TestStatusPaneSuppressesAskUser proves the ask_user HITL pause primitive is NOT
// shown as a pane tool row. A clean pause never reaches the pane (the translator
// emits RUN_FINISHED-interrupt, no TOOL_CALL events); a MALFORMED ask_user that falls
// through to tool dispatch as an error must not flash "❌ ask_user" while the model
// self-corrects — its UI is the inline keyboard (hitl.go), not a pane row.
func TestStatusPaneSuppressesAskUser(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	drivePane(bot, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewToolCallStartEvent("c1", "ask_user"),
		events.NewToolCallResultEvent("m", "c1", `error: ask_user: kind "" must be one of clarification|approval|choice`),
	})
	got := lastText(bot)
	if strings.Contains(got, "ask_user") {
		t.Errorf("ask_user (a HITL control primitive) must not appear in the status pane, got %q", got)
	}
	if containsRune(got, '❌') {
		t.Errorf("a suppressed ask_user must not render a ❌ tool row, got %q", got)
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
