package agui

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/google/uuid"
	"pgregory.net/rapid"
)

func reasoning(text string) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		LLMResponse: &agent.LLMResponse{Reasoning: text}}
}

// TestTranslatorReasoningRunCoalesces: a contiguous run of non-empty reasoning
// deltas (with an empty one that must be skipped) collapses into ONE REASONING
// lifecycle (START / MESSAGE_START / N*MESSAGE_CONTENT / MESSAGE_END / END), with
// a separate rsn- messageId — mirror of the TEXT_MESSAGE coalescing.
func TestTranslatorReasoningRunCoalesces(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		reasoning("Let"), reasoning(""), reasoning(" me"), reasoning(" think"),
	})
	for _, pair := range [][2]int{
		{countType(evs, "REASONING_START"), 1},
		{countType(evs, "REASONING_MESSAGE_START"), 1},
		{countType(evs, "REASONING_MESSAGE_END"), 1},
		{countType(evs, "REASONING_END"), 1},
		{countType(evs, "REASONING_MESSAGE_CONTENT"), 3},
	} {
		if pair[0] != pair[1] {
			t.Fatalf("reasoning lifecycle count = %d, want %d (%v)", pair[0], pair[1], typesOf(evs))
		}
	}
	for _, ev := range evs {
		if err := ev.Validate(); err != nil {
			t.Fatalf("emitted %s failed Validate: %v", ev.Type(), err)
		}
	}
}

// TestTranslator200ReasoningDeltas: 200 contiguous non-empty reasoning deltas →
// exactly ONE START/MESSAGE_START/MESSAGE_END/END quartet, 200 MESSAGE_CONTENT,
// zero CONTENT for the trailing empty delta (acceptance criterion).
func TestTranslator200ReasoningDeltas(t *testing.T) {
	in := make([]*agent.Event, 0, 201)
	for i := 0; i < 200; i++ {
		in = append(in, reasoning("x"))
	}
	in = append(in, reasoning("")) // a trailing empty delta must NOT add a CONTENT
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, in)
	for name, want := range map[string]int{
		"REASONING_START": 1, "REASONING_MESSAGE_START": 1,
		"REASONING_MESSAGE_END": 1, "REASONING_END": 1, "REASONING_MESSAGE_CONTENT": 200,
	} {
		if got := countType(evs, name); got != want {
			t.Fatalf("%s = %d, want %d (empty delta skipped)", name, got, want)
		}
	}
}

// TestTranslatorReasoningInterleavesBeforeText: a turn with reasoning THEN content
// emits the FULL reasoning lifecycle (including REASONING_END) BEFORE the first
// TEXT_MESSAGE_START (interleave-before-text).
func TestTranslatorReasoningInterleavesBeforeText(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		reasoning("hmm"), chunk("Hello"),
	})
	order := typesOf(evs)
	rsnEnd, textStart := -1, -1
	for i, ty := range order {
		if ty == "REASONING_END" {
			rsnEnd = i
		}
		if ty == "TEXT_MESSAGE_START" && textStart == -1 {
			textStart = i
		}
	}
	if rsnEnd == -1 || textStart == -1 {
		t.Fatalf("missing REASONING_END or TEXT_MESSAGE_START (%v)", order)
	}
	if rsnEnd >= textStart {
		t.Fatalf("REASONING_END (idx %d) must precede TEXT_MESSAGE_START (idx %d): %v", rsnEnd, textStart, order)
	}
}

// TestTranslatorReasoningClosesOnToolCall: a reasoning run interrupted by a tool
// call closes cleanly (REASONING_MESSAGE_END + REASONING_END) before TOOL_CALL_START.
func TestTranslatorReasoningClosesOnToolCall(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		reasoning("planning"), toolStart("call-1", "web_search", `{"q":"x"}`),
	})
	order := typesOf(evs)
	rsnEnd, toolStartIdx := -1, -1
	for i, ty := range order {
		if ty == "REASONING_END" {
			rsnEnd = i
		}
		if ty == "TOOL_CALL_START" {
			toolStartIdx = i
		}
	}
	if rsnEnd == -1 || toolStartIdx == -1 {
		t.Fatalf("missing REASONING_END or TOOL_CALL_START (%v)", order)
	}
	if rsnEnd >= toolStartIdx {
		t.Fatalf("REASONING_END (idx %d) must precede TOOL_CALL_START (idx %d): %v", rsnEnd, toolStartIdx, order)
	}
	if got := countType(evs, "REASONING_MESSAGE_END"); got != 1 {
		t.Fatalf("REASONING_MESSAGE_END = %d, want 1 (clean close)", got)
	}
}

// TestTranslatorReasoningGoldenShapes asserts the emitted REASONING_* events
// marshal to the 5 golden shapes (rsn- messageId, MESSAGE_START role "assistant",
// MESSAGE_CONTENT delta) seeded from spike 015 in Plan 01.
func TestTranslatorReasoningGoldenShapes(t *testing.T) {
	golden := loadGolden(t)
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		reasoning("thinking…"),
	})
	byType := map[string]events.Event{}
	for _, ev := range evs {
		byType[string(ev.Type())] = ev
	}
	for _, name := range []string{
		"REASONING_START", "REASONING_MESSAGE_START", "REASONING_MESSAGE_CONTENT",
		"REASONING_MESSAGE_END", "REASONING_END",
	} {
		ev, ok := byType[name]
		if !ok {
			t.Fatalf("translator did not emit %s (got %v)", name, typesOf(evs))
		}
		if name == "REASONING_MESSAGE_CONTENT" {
			msg, ok := ev.(*events.ReasoningMessageContentEvent)
			if !ok {
				t.Fatalf("REASONING_MESSAGE_CONTENT type = %T", ev)
			}
			if msg.Delta != redactedReasoningDelta {
				t.Fatalf("reasoning content delta = %q, want redacted marker (showReasoning off)", msg.Delta)
			}
			continue
		}
		assertGoldenShape(t, name, golden[name], ev)
	}
}

// TestTranslatorReasoningPassthroughWhenEnabled proves that with showReasoning=true the
// translator emits the REAL CoT delta (not the redacted marker) so an opted-in consumer
// — the Telegram live 💭 window — actually sees the reasoning. This is the integration
// half the redaction default deliberately withholds.
func TestTranslatorReasoningPassthroughWhenEnabled(t *testing.T) {
	seq := func(yield func(*agent.Event, error) bool) {
		yield(reasoning("valuto le brocche da 12 e 7"), nil)
	}
	var got string
	for ev := range Translate("thread-1", "run-1", &fixedIDGen{}, seq, true) {
		if msg, ok := ev.(*events.ReasoningMessageContentEvent); ok {
			got = msg.Delta
		}
	}
	if got == redactedReasoningDelta {
		t.Fatalf("showReasoning=true still redacted the delta: %q", got)
	}
	if got != "valuto le brocche da 12 e 7" {
		t.Fatalf("reasoning delta = %q, want the real CoT text", got)
	}
}

// TestHandleRunCockpitStreamsReasoning proves the D-01 call-site flip: the cockpit
// SSE path (handleRun) now streams the REAL reasoning delta, not the redacted marker.
// This asserts the flip at the handler (server.go), distinct from the translator-unit
// TestTranslatorReasoningPassthroughWhenEnabled which proves Translate(…, true) in
// isolation. A scripted reasoning turn flows through POST /agent/run and the SSE body
// must carry the live CoT text, never the "[reasoning redacted]" marker.
func TestHandleRunCockpitStreamsReasoning(t *testing.T) {
	const tid = "11111111-1111-1111-1111-111111111111"
	const cot = "valuto le brocche da 12 e 7"
	run := &scriptedRunner{events: []*agent.Event{
		{Author: "aura", LLMResponse: &agent.LLMResponse{Reasoning: cot}},
		{Author: "aura", LLMResponse: &agent.LLMResponse{Content: "Otto litri.", FinishReason: "stop"}},
	}}
	srv := newTestServer(t, run, &fakeConvStore{known: map[string]bool{tid: true}})

	resp := postRun(t, srv, runPayload(tid))
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read SSE body: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, "REASONING_MESSAGE_CONTENT") {
		t.Fatalf("cockpit SSE missing the REASONING lifecycle: %s", body)
	}
	if strings.Contains(body, redactedReasoningDelta) {
		t.Fatalf("cockpit SSE still redacted the reasoning delta (D-01 flip not wired at handleRun): %s", body)
	}
	if !strings.Contains(body, cot) {
		t.Fatalf("cockpit SSE did not stream the real CoT text %q: %s", cot, body)
	}
}

// assertReasoningQuartets walks the emitted stream and asserts every contiguous
// reasoning run is a well-formed REASONING_START → REASONING_MESSAGE_START →
// N≥0 REASONING_MESSAGE_CONTENT → REASONING_MESSAGE_END → REASONING_END quartet in
// exactly that order, sharing one messageId across the run.
func assertReasoningQuartets(rt *rapid.T, evs []events.Event) {
	const (
		idle = iota
		started
		msgStarted
		msgEnded
	)
	state := idle
	for _, ev := range evs {
		switch string(ev.Type()) {
		case "REASONING_START":
			if state != idle {
				rt.Fatalf("REASONING_START out of order (state %d): %v", state, typesOf(evs))
			}
			state = started
		case "REASONING_MESSAGE_START":
			if state != started {
				rt.Fatalf("REASONING_MESSAGE_START out of order (state %d): %v", state, typesOf(evs))
			}
			state = msgStarted
		case "REASONING_MESSAGE_CONTENT":
			if state != msgStarted {
				rt.Fatalf("REASONING_MESSAGE_CONTENT out of order (state %d): %v", state, typesOf(evs))
			}
		case "REASONING_MESSAGE_END":
			if state != msgStarted {
				rt.Fatalf("REASONING_MESSAGE_END out of order (state %d): %v", state, typesOf(evs))
			}
			state = msgEnded
		case "REASONING_END":
			if state != msgEnded {
				rt.Fatalf("REASONING_END out of order (state %d): %v", state, typesOf(evs))
			}
			state = idle
		}
	}
	if state != idle {
		rt.Fatalf("reasoning run left open (state %d): %v", state, typesOf(evs))
	}
}
