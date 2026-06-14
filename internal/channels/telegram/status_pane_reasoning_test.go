package telegram

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"
)

// drivePaneReasoning drives events through a pane with live CoT enabled (the
// AURA_TELEGRAM_SHOW_REASONING path). started is pre-seeded so elapsedText is
// deterministic; the clock is fixed so the elapsed reads a stable value. Throttle is
// zero so every event renders and lastText is the live (not coalesced) window.
func drivePaneReasoning(bot *fakeBot, evs []events.Event, fifoRunes int, started, nowAt time.Time) {
	p := newStatusPane(bot, tele.ChatID(7), 0, true, fifoRunes)
	p.now = func() time.Time { return nowAt }
	p.sleep = func(time.Duration) {}
	p.started = started
	ch := make(chan events.Event, len(evs))
	for _, e := range evs {
		ch <- e
	}
	close(ch)
	p.consume(context.Background(), ch)
}

// While reasoning is in flight the pane surfaces the live CoT text (not the redacted
// "in corso" placeholder) under the 💭 header.
func TestStatusPaneSurfacesLiveReasoningWhenEnabled(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	drivePaneReasoning(bot, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewReasoningStartEvent("rsn1"),
		events.NewReasoningMessageContentEvent("rsn1", "Sto valutando le brocche da 12 e 7 litri"),
	}, 4096, time.Unix(30, 0), time.Unix(30, 0))

	got := lastText(bot)
	if !containsRune(got, '💭') {
		t.Fatalf("expected the 💭 reasoning header, got %q", got)
	}
	if !strings.Contains(got, "valutando le brocche") {
		t.Fatalf("live reasoning text should surface when enabled, got %q", got)
	}
}

// The window is rune-capped to the configured FIFO size and keeps the NEWEST runes
// (tail-keep): an early marker is evicted, the latest is retained.
func TestStatusPaneReasoningWindowTailCapped(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	drivePaneReasoning(bot, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewReasoningStartEvent("rsn1"),
		events.NewReasoningMessageContentEvent("rsn1", "OLDEST-MARKER "+strings.Repeat("x", 200)),
		events.NewReasoningMessageContentEvent("rsn1", " NEWEST-MARKER"),
	}, 64, time.Unix(30, 0), time.Unix(30, 0))

	got := lastText(bot)
	if strings.Contains(got, "OLDEST-MARKER") {
		t.Fatalf("front-evicted reasoning must not survive, got %q", got)
	}
	if !strings.Contains(got, "NEWEST-MARKER") {
		t.Fatalf("newest reasoning must stay visible (tail-keep), got %q", got)
	}
}

// Multibyte deltas straddling the FIFO cap must never render a split rune.
func TestStatusPaneReasoningRuneSafeAtBoundary(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	drivePaneReasoning(bot, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewReasoningStartEvent("rsn1"),
		events.NewReasoningMessageContentEvent("rsn1", strings.Repeat("à", 30)),
		events.NewReasoningMessageContentEvent("rsn1", "é😀ç🚀"),
	}, 5, time.Unix(30, 0), time.Unix(30, 0))

	got := lastText(bot)
	if !utf8.ValidString(got) {
		t.Fatalf("status text is not valid UTF-8: %q", got)
	}
	if strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("status text contains the replacement rune (split multibyte): %q", got)
	}
}

// The elapsed-time cue appears in the live reasoning header.
func TestStatusPaneReasoningShowsElapsed(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	drivePaneReasoning(bot, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewReasoningStartEvent("rsn1"),
		events.NewReasoningMessageContentEvent("rsn1", "ragionando"),
	}, 4096, time.Unix(30, 0), time.Unix(42, 0)) // 42 - 30 = 12s

	got := lastText(bot)
	if !strings.Contains(got, "12s") {
		t.Fatalf("expected the elapsed-time cue (12s) in the reasoning header, got %q", got)
	}
}

// Even with live CoT enabled, the window clears on the final answer: the pane collapses
// to "completato" and never leaves raw reasoning frozen on the last edit.
func TestStatusPaneLiveReasoningClearsOnFinal(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	drivePaneReasoning(bot, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewReasoningStartEvent("rsn1"),
		events.NewReasoningMessageContentEvent("rsn1", "RAW-CHAIN-OF-THOUGHT detail detail"),
		events.NewReasoningEndEvent("rsn1"),
		events.NewRunFinishedEvent("t", "r"),
	}, 4096, time.Unix(30, 0), time.Unix(45, 0))

	got := lastText(bot)
	if !strings.Contains(got, "completato") {
		t.Fatalf("final pane must collapse to the safe label, got %q", got)
	}
	if strings.Contains(got, "RAW-CHAIN-OF-THOUGHT") {
		t.Fatalf("live reasoning must not stay frozen on the final pane, got %q", got)
	}
}

// The live window, plus a heavy tool list and a cost footer, never exceeds the
// Telegram text cap.
func TestStatusPaneLiveReasoningRespectsTelegramCap(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	evs := []events.Event{events.NewRunStartedEvent("t", "r")}
	for range 40 {
		id := "tool-call-" + strings.Repeat("x", 30)
		evs = append(evs, events.NewToolCallStartEvent(id, id), events.NewToolCallResultEvent("m", id, "ok"))
	}
	evs = append(evs,
		events.NewReasoningStartEvent("rsn1"),
		events.NewReasoningMessageContentEvent("rsn1", strings.Repeat("ragionamento ", 1000)),
		events.NewStateDeltaEvent([]events.JSONPatchOperation{
			{Op: "replace", Path: "/cost_usd", Value: "0.0099"},
		}),
	)
	drivePaneReasoning(bot, evs, 4096, time.Unix(30, 0), time.Unix(30, 0))

	got := lastText(bot)
	if n := len([]rune(got)); n > telegramTextCap {
		t.Fatalf("status pane exceeded Telegram cap: %d runes > %d", n, telegramTextCap)
	}
	if !strings.Contains(got, "Costo: $0.0099") {
		t.Fatalf("cost footer must survive the live reasoning window, got %q", got)
	}
}

// Reasoning content persists in the FIFO after ReasoningEnd so the user can read it
// while subsequent tools run. The window is not cleared between span end and run done.
func TestStatusPaneReasoningPersistsThroughTurn(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	drivePaneReasoning(bot, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewReasoningStartEvent("rsn1"),
		events.NewReasoningMessageContentEvent("rsn1", "thinking about X"),
		events.NewReasoningEndEvent("rsn1"),
		events.NewToolCallStartEvent("c1", "web_search"),
		events.NewToolCallResultEvent("m", "c1", "results"),
	}, 4096, time.Unix(30, 0), time.Unix(30, 0))

	got := lastText(bot)
	if !strings.Contains(got, "thinking about X") {
		t.Fatalf("reasoning must persist after span end while turn continues, got %q", got)
	}
	if !strings.Contains(got, "web_search") {
		t.Fatalf("tool must also appear alongside live reasoning, got %q", got)
	}
}

// Multi-span turns accumulate all reasoning into the FIFO separated by " · "; neither
// span is lost before the run finishes.
func TestStatusPaneReasoningAccumulatesAcrossSpans(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	drivePaneReasoning(bot, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewReasoningStartEvent("rsn1"),
		events.NewReasoningMessageContentEvent("rsn1", "alpha"),
		events.NewReasoningEndEvent("rsn1"),
		events.NewToolCallStartEvent("c1", "web_search"),
		events.NewToolCallResultEvent("m", "c1", "ok"),
		events.NewReasoningStartEvent("rsn2"),
		events.NewReasoningMessageContentEvent("rsn2", "beta"),
	}, 4096, time.Unix(30, 0), time.Unix(30, 0))

	got := lastText(bot)
	if !strings.Contains(got, "alpha") {
		t.Fatalf("first span must survive into second span, got %q", got)
	}
	if !strings.Contains(got, "beta") {
		t.Fatalf("second span must appear in the window, got %q", got)
	}
	// The separator " · " must join the two spans.
	if !strings.Contains(got, "·") {
		t.Fatalf("spans must be separated by \" · \" in the window, got %q", got)
	}
}

// After RunFinished the live window is cleared: "completato" shows, raw thoughts gone.
func TestStatusPaneReasoningClearsOnRunFinished(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	drivePaneReasoning(bot, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewReasoningStartEvent("rsn1"),
		events.NewReasoningMessageContentEvent("rsn1", "TURN-THOUGHT detail"),
		events.NewReasoningEndEvent("rsn1"),
		events.NewRunFinishedEvent("t", "r"),
	}, 4096, time.Unix(30, 0), time.Unix(30, 0))

	got := lastText(bot)
	if strings.Contains(got, "TURN-THOUGHT") {
		t.Fatalf("reasoning window must be cleared on RunFinished, got %q", got)
	}
	if !strings.Contains(got, "completato") {
		t.Fatalf("final pane must show \"completato\" after run done, got %q", got)
	}
}

// With the flag OFF (default), the pane keeps the redacted lifecycle even when deltas
// stream — the regression guard for the privacy default alongside the new feature.
func TestStatusPaneDefaultStillRedactsReasoning(t *testing.T) {
	t.Parallel()
	bot := newFakeBot()
	drivePane(bot, []events.Event{
		events.NewRunStartedEvent("t", "r"),
		events.NewReasoningStartEvent("rsn1"),
		events.NewReasoningMessageContentEvent("rsn1", "SECRET-RAW-REASONING text"),
	})
	got := lastText(bot)
	if strings.Contains(got, "SECRET-RAW-REASONING") {
		t.Fatalf("default (flag off) must not surface raw reasoning, got %q", got)
	}
	if !strings.Contains(got, "in corso") {
		t.Fatalf("default reasoning line should show the redacted lifecycle, got %q", got)
	}
}
