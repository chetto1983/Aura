package agui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/google/uuid"
	"go.uber.org/goleak"
	"pgregory.net/rapid"
)

var errAGUITest = errors.New("agui-test: simulated infra error")

// fixedIDGen is a deterministic IDGenerator so golden compares are stable: every
// message run gets msg-1, msg-2, …, reasoning runs rsn-1, rsn-2, … and tool
// results msg-tool-<callID>.
type fixedIDGen struct {
	n   int
	rsn int
}

func (g *fixedIDGen) NewMessageID() string {
	g.n++
	return "msg-" + strconv.Itoa(g.n)
}

func (g *fixedIDGen) NewReasoningID() string {
	g.rsn++
	return "rsn-" + strconv.Itoa(g.rsn)
}

func (g *fixedIDGen) NewToolResultID(toolCallID string) string {
	return "msg-tool-" + toolCallID
}

func collect(t *testing.T, threadID, runID string, gen IDGenerator, evs []*agent.Event) []events.Event {
	t.Helper()
	seq := func(yield func(*agent.Event, error) bool) {
		for _, ev := range evs {
			if !yield(ev, nil) {
				return
			}
		}
	}
	var out []events.Event
	for ev, err := range Translate(threadID, runID, gen, seq, false) {
		if err != nil {
			t.Fatalf("translate yielded error: %v", err)
		}
		out = append(out, ev)
	}
	return out
}

func chunk(text string) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		LLMResponse: &agent.LLMResponse{Content: text}}
}

func finalChunk(text, finish string) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		LLMResponse: &agent.LLMResponse{Content: text, FinishReason: finish}}
}

func toolStart(callID, name, args string) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
			Event: agent.ToolInvocationStart, ToolCallID: callID, ToolName: name, Arguments: args}}}
}

func toolEnd(callID, name, preview string) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
			Event: agent.ToolInvocationEnd, ToolCallID: callID, ToolName: name, ResultPreview: preview}}}
}

func toolResultPreview(callID, preview string) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		LLMResponse: &agent.LLMResponse{Content: preview},
		Actions:     agent.Actions{StateDelta: map[string]any{"tool_call_id": callID}}}
}

func stateDelta(d map[string]any) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		Actions: agent.Actions{StateDelta: d}}
}

func pauseEvent(callID, question, kind string) *agent.Event {
	return &agent.Event{RequestID: uuid.Must(uuid.NewV7()), Author: "aura", Timestamp: time.Now(),
		Actions: agent.Actions{AwaitingInput: &agent.AwaitingInput{
			Question: question, Kind: kind, ToolCallID: callID,
			Options: []agent.PauseOption{{Label: "Yes", Value: "yes"}}}}}
}

func typesOf(evs []events.Event) []string {
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[i] = string(ev.Type())
	}
	return out
}

func countType(evs []events.Event, t string) int {
	n := 0
	for _, ev := range evs {
		if string(ev.Type()) == t {
			n++
		}
	}
	return n
}

// TestTranslatorContiguousDeltasCoalesce: a contiguous run of non-empty chunk
// deltas (including an empty one that must be skipped) collapses into ONE
// TEXT_MESSAGE lifecycle.
func TestTranslatorContiguousDeltasCoalesce(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		chunk("Ci"), chunk(""), chunk("ao"), chunk(" mondo"),
	})
	if got := countType(evs, "TEXT_MESSAGE_START"); got != 1 {
		t.Fatalf("TEXT_MESSAGE_START = %d, want 1 (%v)", got, typesOf(evs))
	}
	if got := countType(evs, "TEXT_MESSAGE_END"); got != 1 {
		t.Fatalf("TEXT_MESSAGE_END = %d, want 1 (%v)", got, typesOf(evs))
	}
	if got := countType(evs, "TEXT_MESSAGE_CONTENT"); got != 3 {
		t.Fatalf("TEXT_MESSAGE_CONTENT = %d, want 3 (empty delta skipped) (%v)", got, typesOf(evs))
	}
	for _, ev := range evs {
		if err := ev.Validate(); err != nil {
			t.Fatalf("emitted %s failed Validate: %v", ev.Type(), err)
		}
	}
}

// TestTranslator200Deltas: 200 contiguous non-empty deltas → exactly ONE START,
// ONE END, 200 CONTENT, zero empty-delta CONTENT (acceptance criterion).
func TestTranslator200Deltas(t *testing.T) {
	in := make([]*agent.Event, 0, 201)
	for range 200 {
		in = append(in, chunk("x"))
	}
	in = append(in, chunk("")) // a trailing empty delta must NOT add a CONTENT
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, in)
	if got := countType(evs, "TEXT_MESSAGE_START"); got != 1 {
		t.Fatalf("START = %d, want 1", got)
	}
	if got := countType(evs, "TEXT_MESSAGE_END"); got != 1 {
		t.Fatalf("END = %d, want 1", got)
	}
	if got := countType(evs, "TEXT_MESSAGE_CONTENT"); got != 200 {
		t.Fatalf("CONTENT = %d, want 200 (empty delta skipped)", got)
	}
}

// TestTranslatorToolResultIsNotText: a tool-result preview Event (StateDelta with
// tool_call_id) emits TOOL_CALL_RESULT and ZERO TEXT_MESSAGE_CONTENT (Pitfall 2).
func TestTranslatorToolResultIsNotText(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		toolResultPreview("call-1", "12°C sereno"),
	})
	if got := countType(evs, "TOOL_CALL_RESULT"); got != 1 {
		t.Fatalf("TOOL_CALL_RESULT = %d, want 1 (%v)", got, typesOf(evs))
	}
	if got := countType(evs, "TEXT_MESSAGE_CONTENT"); got != 0 {
		t.Fatalf("TEXT_MESSAGE_CONTENT = %d, want 0 — tool preview must NOT stream as prose", got)
	}
	if got := countType(evs, "STATE_DELTA"); got != 0 {
		t.Fatalf("STATE_DELTA = %d, want 0 — the tool_call_id marker is not a state delta", got)
	}
}

// TestTranslatorFinalEventNoDoubleStream: a final Event (FinishReason set) closes
// the open run as END-only and does NOT re-emit its full Content as a CONTENT
// (locked OQ1 policy a — no double-stream).
func TestTranslatorFinalEventNoDoubleStream(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		chunk("Hel"), chunk("lo"),
		finalChunk("Hello", "stop"), // the full answer + finish — must NOT add a CONTENT
	})
	if got := countType(evs, "TEXT_MESSAGE_CONTENT"); got != 2 {
		t.Fatalf("CONTENT = %d, want 2 (only the 2 deltas, NOT the final full answer) (%v)", got, typesOf(evs))
	}
	if got := countType(evs, "TEXT_MESSAGE_START"); got != 1 {
		t.Fatalf("START = %d, want 1", got)
	}
	if got := countType(evs, "TEXT_MESSAGE_END"); got != 1 {
		t.Fatalf("END = %d, want 1", got)
	}
}

func TestTranslatorTerminalEventWithoutStreamEmitsAnswerBeforeState(t *testing.T) {
	final := finalChunk("AURA_OLLAMA_ROUTE_OK", "stop")
	final.Actions.StateDelta = map[string]any{"prompt_tokens": 15_200}
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{final})
	if got := typesOf(evs); !slices.Equal(got, []string{
		"RUN_STARTED",
		"TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END",
		"STATE_DELTA", "RUN_FINISHED",
	}) {
		t.Fatalf("terminal-only answer events = %v", got)
	}
}

// TestTranslatorInterruptOutcome: an AwaitingInput Event yields RUN_FINISHED with
// an interrupt outcome carrying a non-empty Interrupt slice, then STOPS (no
// trailing success RUN_FINISHED).
func TestTranslatorInterruptOutcome(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		chunk("thinking"),
		pauseEvent("call-ask-1", "Confermi?", "ask_user"),
		chunk("never reached"),
	})
	last := evs[len(evs)-1]
	if string(last.Type()) != "RUN_FINISHED" {
		t.Fatalf("last event = %s, want RUN_FINISHED", last.Type())
	}
	if got := countType(evs, "RUN_FINISHED"); got != 1 {
		t.Fatalf("RUN_FINISHED = %d, want 1 (no trailing success after interrupt)", got)
	}
	raw, err := json.Marshal(last)
	if err != nil {
		t.Fatalf("marshal RUN_FINISHED: %v", err)
	}
	var fin struct {
		Outcome struct {
			Type       string `json:"type"`
			Interrupts []struct {
				ID      string `json:"id"`
				Message string `json:"message"`
			} `json:"interrupts"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(raw, &fin); err != nil {
		t.Fatalf("unmarshal RUN_FINISHED: %v", err)
	}
	if fin.Outcome.Type != "interrupt" {
		t.Fatalf("outcome.type = %q, want interrupt", fin.Outcome.Type)
	}
	if len(fin.Outcome.Interrupts) == 0 {
		t.Fatalf("interrupt slice empty; want non-empty")
	}
	if fin.Outcome.Interrupts[0].Message != "Confermi?" {
		t.Fatalf("interrupt message = %q, want Confermi?", fin.Outcome.Interrupts[0].Message)
	}
}

// TestTranslatorErrorStops: an error in the iter.Seq2 slot becomes RUN_ERROR and
// stops the stream (no RUN_FINISHED after).
func TestTranslatorErrorStops(t *testing.T) {
	seq := func(yield func(*agent.Event, error) bool) {
		if !yield(chunk("partial"), nil) {
			return
		}
		yield(nil, errAGUITest)
	}
	var out []events.Event
	for ev, err := range Translate("thread-1", "run-1", &fixedIDGen{}, seq, false) {
		if err != nil {
			t.Fatalf("Translate must not propagate the error in the err slot; it maps to RUN_ERROR: %v", err)
		}
		out = append(out, ev)
	}
	last := out[len(out)-1]
	if string(last.Type()) != "RUN_ERROR" {
		t.Fatalf("last event = %s, want RUN_ERROR", last.Type())
	}
	if countType(out, "RUN_FINISHED") != 0 {
		t.Fatalf("RUN_FINISHED present after RUN_ERROR; want none")
	}
}

// TestTranslatorErrorPathHonorsConsumerStop pins WR-02: on the error path the translator
// closes any open run (which yields a TEXT_MESSAGE_END) BEFORE emitting RUN_ERROR. If the
// consumer returns false on that END frame, the translator must NOT yield RUN_ERROR after —
// a yield-after-false violates the iter.Seq2 contract (go test panics with "range function
// continued iteration after function returned false"). The error branch must be guarded
// like every other branch (`if !closeRuns() { return }`), not discard the close result.
func TestTranslatorErrorPathHonorsConsumerStop(t *testing.T) {
	seq := func(yield func(*agent.Event, error) bool) {
		if !yield(chunk("partial"), nil) { // opens a text run
			return
		}
		yield(nil, errAGUITest) // error path closes the open run, then RUN_ERROR
	}

	var got []events.Event
	// A strict consumer that STOPS at the first END frame — the very frame closeRuns emits
	// on the error path. A yield after this returns-false is the contract violation.
	for ev, err := range Translate("thread-1", "run-1", &fixedIDGen{}, seq, false) {
		if err != nil {
			t.Fatalf("Translate must not propagate the error in the err slot: %v", err)
		}
		got = append(got, ev)
		if string(ev.Type()) == "TEXT_MESSAGE_END" {
			break // returns false to the range function
		}
	}

	if last := string(got[len(got)-1].Type()); last != "TEXT_MESSAGE_END" {
		t.Fatalf("consumer stopped at %s, want TEXT_MESSAGE_END (the close frame)", last)
	}
	// The guard means no RUN_ERROR is yielded after the stop — proven by reaching here
	// without the range-function panic and by the absence of RUN_ERROR in the collected
	// frames (the loop broke before it could be appended).
	if countType(got, "RUN_ERROR") != 0 {
		t.Fatalf("RUN_ERROR yielded after consumer stop: %v", typesOf(got))
	}
}

// TestTranslatorToolCallLifecycle: a tool start (with args) + end emits
// START/ARGS/END/RESULT in order; a start with empty args omits ARGS.
func TestTranslatorToolCallLifecycle(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		toolStart("call-1", "web_search", `{"q":"x"}`),
		toolEnd("call-1", "web_search", "result preview"),
	})
	want := []string{"RUN_STARTED", "TOOL_CALL_START", "TOOL_CALL_ARGS", "TOOL_CALL_END", "TOOL_CALL_RESULT", "RUN_FINISHED"}
	got := typesOf(evs)
	if len(got) != len(want) {
		t.Fatalf("seq = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("seq[%d] = %s, want %s (full %v)", i, got[i], want[i], got)
		}
	}

	noArgs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		toolStart("call-2", "current_time", ""),
	})
	if countType(noArgs, "TOOL_CALL_ARGS") != 0 {
		t.Fatalf("empty Arguments must NOT emit TOOL_CALL_ARGS (%v)", typesOf(noArgs))
	}
}

// TestTranslatorStateDeltaSortedKeys: a multi-key StateDelta builds a
// JSONPatchOperation slice over SORTED keys (deterministic golden compares).
func TestTranslatorStateDeltaSortedKeys(t *testing.T) {
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		stateDelta(map[string]any{"zeta": 1, "alpha": 2, "mid": 3}),
	})
	var sd events.Event
	for _, ev := range evs {
		if string(ev.Type()) == "STATE_DELTA" {
			sd = ev
		}
	}
	if sd == nil {
		t.Fatalf("no STATE_DELTA emitted (%v)", typesOf(evs))
	}
	raw, err := json.Marshal(sd)
	if err != nil {
		t.Fatalf("marshal STATE_DELTA: %v", err)
	}
	var got struct {
		Delta []struct {
			Path string `json:"path"`
		} `json:"delta"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal STATE_DELTA: %v", err)
	}
	wantPaths := []string{"/alpha", "/mid", "/zeta"}
	if len(got.Delta) != len(wantPaths) {
		t.Fatalf("delta len = %d, want %d", len(got.Delta), len(wantPaths))
	}
	for i, p := range wantPaths {
		if got.Delta[i].Path != p {
			t.Fatalf("delta[%d].path = %q, want %q (keys must be sorted)", i, got.Delta[i].Path, p)
		}
	}
}

// TestTranslatorReasoningRunCoalesces: a contiguous run of non-empty reasoning
// deltas (with an empty one that must be skipped) collapses into ONE REASONING
// lifecycle (START / MESSAGE_START / N*MESSAGE_CONTENT / MESSAGE_END / END), with
// a separate rsn- messageId — mirror of the TEXT_MESSAGE coalescing.
// TestTranslatorGoldenShapes asserts the translator's output for representative
// inputs marshals to the golden wire shapes (golden ⊆ emitted on shared keys,
// ignoring the auto-attached volatile timestamp).
func TestTranslatorGoldenShapes(t *testing.T) {
	golden := loadGolden(t)
	evs := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{
		chunk("Ciao"),
		toolStart("call-1", "web_search", `{"query":"meteo`),
		toolEnd("call-1", "web_search", "result preview + footer"),
		stateDelta(map[string]any{"cost_usd": 0.0042}),
	})
	byType := map[string]events.Event{}
	for _, ev := range evs {
		byType[string(ev.Type())] = ev
	}
	for _, name := range []string{
		"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END",
		"TOOL_CALL_START", "TOOL_CALL_ARGS", "TOOL_CALL_END", "TOOL_CALL_RESULT", "STATE_DELTA",
	} {
		ev, ok := byType[name]
		if !ok {
			t.Fatalf("translator did not emit %s (got %v)", name, typesOf(evs))
		}
		assertGoldenShape(t, name, golden[name], ev)
	}
	successFin := collect(t, "thread-1", "run-1", &fixedIDGen{}, []*agent.Event{chunk("ok")})
	assertGoldenShape(t, "RUN_FINISHED(success)", golden["RUN_FINISHED(success)"], successFin[len(successFin)-1])
}

func loadGolden(t *testing.T) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "golden-events.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var m map[string]map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal golden: %v", err)
	}
	return m
}

func assertGoldenShape(t *testing.T, name string, want map[string]any, got events.Event) {
	t.Helper()
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("%s marshal: %v", name, err)
	}
	var gotMap map[string]any
	if err := json.Unmarshal(raw, &gotMap); err != nil {
		t.Fatalf("%s unmarshal: %v", name, err)
	}
	for k, wv := range want {
		// timestamp is auto-attached (volatile epoch). The TOOL_CALL_RESULT
		// messageId is a translator-minted correlation id (derived from the
		// toolCallId, msg-tool-<id>); the golden's "msg-2" is the spike's arbitrary
		// value, not a wire contract — assert non-empty instead of byte-equal.
		if k == "timestamp" {
			continue
		}
		gv, ok := gotMap[k]
		if !ok {
			t.Fatalf("%s: emitted event missing golden key %q (emitted: %s)", name, k, raw)
		}
		if name == "TOOL_CALL_RESULT" && k == "messageId" {
			if s, _ := gv.(string); s == "" {
				t.Fatalf("%s: messageId is empty (Validate requires a non-empty id)", name)
			}
			continue
		}
		wb, _ := json.Marshal(wv)
		gb, _ := json.Marshal(gv)
		if string(wb) != string(gb) {
			t.Fatalf("%s: key %q = %s, golden = %s", name, k, gb, wb)
		}
	}
}

// TestTranslatorProperty draws a random []*agent.Event mix and asserts the AG-UI
// invariants hold for every translation (Validate, RUN_STARTED-first, terminal
// RUN_FINISHED/RUN_ERROR, no empty deltas, non-empty ids, contiguous coalescing).
func TestTranslatorProperty(t *testing.T) {
	defer goleak.VerifyNone(t)
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 20).Draw(rt, "n")
		kinds := []string{"chunk", "empty", "tool", "state", "final", "preview", "reasoning", "reasoningEmpty", "artifact"}
		in := make([]*agent.Event, 0, n)
		for i := range n {
			switch rapid.SampledFrom(kinds).Draw(rt, "kind") {
			case "chunk":
				in = append(in, chunk(rapid.StringMatching(`[a-z ]{1,8}`).Draw(rt, "delta")))
			case "empty":
				in = append(in, chunk(""))
			case "reasoning":
				in = append(in, reasoning(rapid.StringMatching(`[a-z ]{1,8}`).Draw(rt, "rdelta")))
			case "reasoningEmpty":
				in = append(in, reasoning(""))
			case "artifact":
				in = append(in, artifactEvent(map[string]any{
					"path": "/abs/f" + strconv.Itoa(i) + ".bin", "filename": "f.bin", "caption": "c"}))
			case "tool":
				id := "call-" + strconv.Itoa(i)
				in = append(in, toolStart(id, "t", `{"a":1}`), toolEnd(id, "t", "p"))
			case "state":
				in = append(in, stateDelta(map[string]any{"cost_usd": 0.01}))
			case "final":
				in = append(in, finalChunk("done", "stop"))
			case "preview":
				in = append(in, toolResultPreview("call-p"+strconv.Itoa(i), "preview"))
			}
		}
		evs := collectRapid(rt, in)

		if len(evs) == 0 || string(evs[0].Type()) != "RUN_STARTED" {
			rt.Fatalf("first event must be RUN_STARTED, got %v", typesOf(evs))
		}
		last := string(evs[len(evs)-1].Type())
		if last != "RUN_FINISHED" && last != "RUN_ERROR" {
			rt.Fatalf("terminal event = %s, want RUN_FINISHED or RUN_ERROR", last)
		}
		var openRuns, openReasoning int
		for _, ev := range evs {
			if err := ev.Validate(); err != nil {
				rt.Fatalf("emitted %s failed Validate: %v", ev.Type(), err)
			}
			assertNonEmptyIDs(rt, ev)
			switch string(ev.Type()) {
			case "TEXT_MESSAGE_START":
				// A text run never opens while a reasoning run is open (never nest):
				// reasoning is always fully closed before TEXT begins.
				if openReasoning != 0 {
					rt.Fatalf("TEXT_MESSAGE_START while a reasoning run is open (must nest never): %v", typesOf(evs))
				}
				openRuns++
			case "TEXT_MESSAGE_END":
				openRuns--
				if openRuns < 0 {
					rt.Fatalf("TEXT_MESSAGE_END without matching START")
				}
			case "REASONING_START":
				// Symmetric: a reasoning run never opens while a text run is open.
				if openRuns != 0 {
					rt.Fatalf("REASONING_START while a text run is open (must nest never): %v", typesOf(evs))
				}
				openReasoning++
			case "REASONING_END":
				openReasoning--
				if openReasoning < 0 {
					rt.Fatalf("REASONING_END without matching START")
				}
			}
		}
		if openRuns != 0 {
			rt.Fatalf("unbalanced TEXT_MESSAGE lifecycle: %d open run(s) (%v)", openRuns, typesOf(evs))
		}
		if openReasoning != 0 {
			rt.Fatalf("unbalanced REASONING lifecycle: %d open run(s) (%v)", openReasoning, typesOf(evs))
		}
		assertReasoningQuartets(rt, evs)
	})
}

func collectRapid(rt *rapid.T, evs []*agent.Event) []events.Event {
	seq := func(yield func(*agent.Event, error) bool) {
		for _, ev := range evs {
			if !yield(ev, nil) {
				return
			}
		}
	}
	var out []events.Event
	for ev, err := range Translate("thread-1", "run-1", &fixedIDGen{}, seq, false) {
		if err != nil {
			rt.Fatalf("translate yielded error: %v", err)
		}
		out = append(out, ev)
	}
	return out
}

func assertNonEmptyIDs(rt *rapid.T, ev events.Event) {
	raw, err := json.Marshal(ev)
	if err != nil {
		rt.Fatalf("marshal %s: %v", ev.Type(), err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		rt.Fatalf("unmarshal %s: %v", ev.Type(), err)
	}
	for _, k := range []string{"messageId", "toolCallId"} {
		if v, ok := m[k]; ok {
			if s, _ := v.(string); s == "" {
				rt.Fatalf("%s carries empty %s", ev.Type(), k)
			}
		}
	}
	if string(ev.Type()) == "TEXT_MESSAGE_CONTENT" {
		if d, _ := m["delta"].(string); d == "" {
			rt.Fatalf("TEXT_MESSAGE_CONTENT carries empty delta (must be skipped)")
		}
	}
	if string(ev.Type()) == "REASONING_MESSAGE_CONTENT" {
		if d, _ := m["delta"].(string); d == "" {
			rt.Fatalf("REASONING_MESSAGE_CONTENT carries empty delta (must be skipped)")
		}
	}
}
