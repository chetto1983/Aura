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

// TestStatusPaneReasoningClearedOnFinish proves the 💭 reasoning is auto-deleted
// once the turn finishes (the final answer has arrived), leaving only the durable
// tool list + cost footer — not a stale wall of thinking.
func TestStatusPaneReasoningClearedOnFinish(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	drivePane(bot, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewReasoningMessageContentEvent("rsn1", "Sto ragionando sul meteo…"),
		events.NewStateDeltaEvent([]events.JSONPatchOperation{
			{Op: "replace", Path: "/cost_usd", Value: "0.0012"},
		}),
		events.NewRunFinishedEvent("t", "r"),
	})
	got := lastText(bot)
	if containsRune(got, '💭') {
		t.Errorf("reasoning 💭 must be gone after RUN_FINISHED, got: %q", got)
	}
	if !strings.Contains(got, "0.0012") {
		t.Errorf("cost footer must survive the reasoning clear, got: %q", got)
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

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}
