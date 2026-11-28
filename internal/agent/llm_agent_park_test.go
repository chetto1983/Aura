package agent

import (
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/google/uuid"
)

const (
	readA = `{"note":"a"}`
	readB = `{"note":"b"}`
)

// eventKind names an event for an order assertion: parked, start:<id>, end:<id>, pause:<id>,
// final, or other.
func eventKind(ev *Event) string {
	switch {
	case ev.Actions.ParkedTurn != nil:
		return "parked"
	case ev.Actions.AwaitingInput != nil:
		return "pause:" + ev.Actions.AwaitingInput.ToolCallID
	case ev.Actions.ToolInvocation != nil:
		return ev.Actions.ToolInvocation.Event + ":" + ev.Actions.ToolInvocation.ToolCallID
	case ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "":
		return "final"
	}
	return "other"
}

func eventKinds(evs []*Event) []string {
	var out []string
	for _, ev := range evs {
		if kind := eventKind(ev); kind != "other" {
			out = append(out, kind)
		}
	}
	return out
}

func assertKinds(t *testing.T, evs []*Event, want ...string) {
	t.Helper()
	if got := eventKinds(evs); !slices.Equal(got, want) {
		t.Fatalf("events = %v\nwant     %v", got, want)
	}
}

// assertHistoryTail checks the history ends with the parked assistant message and then the
// tool messages, in this order.
func assertHistoryTail(t *testing.T, history []llm.Message, calls, answers []string) {
	t.Helper()
	tail := history[len(history)-1-len(answers):]
	if tail[0].Role != llm.RoleAssistant || !slices.Equal(callIDs(tail[0].ToolCalls), calls) {
		t.Fatalf("assistant message = %+v, want the calls %v", tail[0], calls)
	}
	if got := toolAnswers(tail[1:]); !slices.Equal(got, answers) || len(tail[1:]) != len(answers) {
		t.Fatalf("tool messages = %v, want %v", got, answers)
	}
}

func TestSiblingsRunInTheParkRound(t *testing.T) {
	f := newParkFixture(t, true, callsRound(
		toolCall("A", "read_notes", readA),
		toolCall("G", "skill_manage", gatedDeleteArgs),
		toolCall("B", "read_notes", readB),
	))
	evs := f.run(t, personCtx())

	assertKinds(t, evs, "parked", "start:A", "start:B", "end:A", "end:B", "pause:G")
	if ids := callIDs(parkedTurnIn(t, evs).Calls); !slices.Equal(ids, []string{"A", "G", "B"}) {
		t.Fatalf("parked calls = %v, want [A G B]", ids)
	}
	ran := f.log.entries()
	slices.Sort(ran)
	if !slices.Equal(ran, []string{"read_notes " + readA, "read_notes " + readB}) {
		t.Fatalf("ran = %v, want A and B only", ran)
	}
	assertHistoryTail(t, f.agent.HistoryForTest(), []string{"A", "G", "B"}, []string{"A", "B"})
	if f.client.streamCount() != 1 {
		t.Fatalf("model calls = %d, want 1: the park round ends the run", f.client.streamCount())
	}
}

// A batch that writes runs one call after another in the model's order (spec "Siblings"): a
// call that comes after the parked one still runs, and its result follows the parked turn.
func TestSequentialBatchAfterAParkStillRuns(t *testing.T) {
	f := newParkFixture(t, true, callsRound(
		toolCall("A", "read_notes", readA),
		toolCall("G", "skill_manage", gatedDeleteArgs),
		toolCall("W", "notes_write", `{"text":"archived"}`),
	))
	evs := f.run(t, personCtx())

	assertKinds(t, evs, "parked", "start:A", "start:W", "end:A", "end:W", "pause:G")
	if ran := f.log.entries(); !slices.Equal(ran, []string{"read_notes " + readA, `notes_write {"text":"archived"}`}) {
		t.Fatalf("ran = %v, want A then W", ran)
	}
	assertHistoryTail(t, f.agent.HistoryForTest(), []string{"A", "G", "W"}, []string{"A", "W"})
}

// The sibling rule covers every pass-2 source: a draft's siblings run, with the switch off.
func TestDraftSiblingsRun(t *testing.T) {
	f := newParkFixture(t, false, callsRound(
		toolCall("A", "read_notes", readA),
		toolCall("S", "calendar__send_email", `{"to":["anna@example.com"],"subject":"Fattura","body":"In allegato."}`),
		toolCall("B", "read_notes", readB),
	))
	evs := f.run(t, personCtx())

	assertKinds(t, evs, "parked", "start:A", "start:B", "end:A", "end:B", "pause:S")
	if len(f.drafts.inputs) != 1 || f.send.calls != 0 {
		t.Fatalf("drafts %d, sends %d; want one draft and no send", len(f.drafts.inputs), f.send.calls)
	}
	assertHistoryTail(t, f.agent.HistoryForTest(), []string{"A", "S", "B"}, []string{"A", "B"})
}

func TestHostReviewSiblingsRun(t *testing.T) {
	f := newParkFixture(t, false, callsRound(
		toolCall("T", "task", reviewedArgs),
		toolCall("A", "read_notes", readA),
	))
	evs := f.run(t, personCtx())

	assertKinds(t, evs, "parked", "start:A", "end:A", "pause:T")
	if f.review.reviews != 1 || f.review.executions != 0 {
		t.Fatalf("reviews %d, executions %d; want 1 and 0", f.review.reviews, f.review.executions)
	}
	assertHistoryTail(t, f.agent.HistoryForTest(), []string{"T", "A"}, []string{"A"})
}

// Telegram and the REPL stop reading at the first pause. By then every sibling has run and
// streamed, and ParkedTurn has already carried every pause the round made.
func TestConsumerStopAtTheFirstPauseStillRanSiblings(t *testing.T) {
	f := newParkFixture(t, true, callsRound(
		toolCall("A", "read_notes", readA),
		toolCall("G1", "skill_manage", `{"action":"delete","name":"first"}`),
		toolCall("B", "read_notes", readB),
		toolCall("G2", "skill_manage", `{"action":"delete","name":"second"}`),
	))
	var seen []*Event
	for ev, err := range f.agent.Run(InvocationContext{Ctx: personCtx(), RequestID: uuid.Must(uuid.NewV7()), Budget: testBudget(t)}) {
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if ev == nil {
			continue
		}
		seen = append(seen, ev)
		if ev.Actions.AwaitingInput != nil {
			break
		}
	}
	assertKinds(t, seen, "parked", "start:A", "start:B", "end:A", "end:B", "pause:G1")
	turn := parkedTurnIn(t, seen)
	if len(turn.Pauses) != 2 || turn.Pauses[0].ToolCallID != "G1" || turn.Pauses[1].ToolCallID != "G2" {
		t.Fatalf("ParkedTurn pauses = %+v, want G1 and G2", turn.Pauses)
	}
}

// Since #300 the AG-UI translator ends a run at the first event after its pauses, so the
// pauses close the stream, back to back.
func TestPausesAreLastAndContiguous(t *testing.T) {
	f := newParkFixture(t, true, callsRound(
		toolCall("G1", "skill_manage", `{"action":"delete","name":"first"}`),
		toolCall("A", "read_notes", readA),
		toolCall("G2", "skill_manage", `{"action":"delete","name":"second"}`),
	))
	evs := f.run(t, personCtx())

	first := slices.IndexFunc(evs, func(ev *Event) bool { return ev.Actions.AwaitingInput != nil })
	if first < 0 {
		t.Fatal("no pause")
	}
	for i, ev := range evs[first:] {
		if ev.Actions.AwaitingInput == nil {
			t.Fatalf("event %d after the first pause is %q, not a pause", first+i, eventKind(ev))
		}
	}
	if n := len(evs) - first; n != 2 {
		t.Fatalf("pauses at the end = %d, want 2", n)
	}
}

// A steer that arrives while the model writes the parked round is not drained into a
// sibling's result: the run ends on the pauses, and the steer waits for the next turn.
func TestParkRoundDrainsNoSteer(t *testing.T) {
	f := newParkFixture(t, true, callsRound(
		toolCall("A", "read_notes", readA),
		toolCall("G", "skill_manage", gatedDeleteArgs),
	))
	inbox := &fakeSteerInbox{byConv: map[string][]steer.Message{}}
	f.agent.steer = inbox
	const redirect = "lascia stare le skill"
	f.client.onStream = func(int) {
		inbox.byConv[f.agent.sessionID] = []steer.Message{{ID: "s1", Source: "cockpit", Text: redirect}}
	}
	evs := f.run(t, personCtx())

	if slices.ContainsFunc(evs, func(ev *Event) bool { return ev.Actions.SteerDelta != nil }) {
		t.Fatal("the park round delivered a steer")
	}
	if queued := inbox.byConv[f.agent.sessionID]; len(queued) != 1 || queued[0].ID != "s1" {
		t.Fatalf("queued steers = %+v, want s1 still queued", queued)
	}
	for _, m := range f.agent.HistoryForTest() {
		if strings.Contains(m.Content, redirect) {
			t.Fatalf("the steer reached history: %+v", m)
		}
	}
	assertKinds(t, evs, "parked", "start:A", "end:A", "pause:G")
}

// assertTerminalDropped checks a park round whose batch also carried the terminal (N2, D14):
// the cards, no final answer, and the terminal gone from ParkedTurn and history.
func assertTerminalDropped(t *testing.T, f *parkFixture, evs []*Event, wantCalls []string) {
	t.Helper()
	if hasFinalAnswer(evs) {
		t.Fatal("a final answer was yielded ahead of the cards")
	}
	for _, ev := range evs {
		if ev.LLMResponse != nil && strings.Contains(ev.LLMResponse.Content, "Fatto.") {
			t.Fatalf("the terminal's text reached the stream: %+v", ev.LLMResponse)
		}
	}
	if ids := callIDs(parkedTurnIn(t, evs).Calls); !slices.Equal(ids, wantCalls) {
		t.Fatalf("parked calls = %v, want %v", ids, wantCalls)
	}
	history := f.agent.HistoryForTest()
	if ids := callIDs(lastAssistantCalls(history)); !slices.Equal(ids, wantCalls) {
		t.Fatalf("assistant calls = %v, want %v", ids, wantCalls)
	}
	if answers := toolAnswers(history); len(answers) != 0 {
		t.Fatalf("history answers %v, want nothing answered", answers)
	}
}

var terminalCallF = toolCall("F", terminalTool, `{"text":"Fatto."}`)

func TestDraftParkWithATextResponseSiblingDropsTheTerminal(t *testing.T) {
	f := newParkFixture(t, false, callsRound(
		toolCall("S", "calendar__send_email", `{"to":["anna@example.com"],"subject":"Fattura","body":"In allegato."}`),
		terminalCallF,
	))
	evs := f.run(t, personCtx())

	assertKinds(t, evs, "parked", "pause:S")
	assertTerminalDropped(t, f, evs, []string{"S"})
}

func TestHostReviewParkWithATextResponseSiblingDropsTheTerminal(t *testing.T) {
	f := newParkFixture(t, false, callsRound(toolCall("T", "task", reviewedArgs), terminalCallF))
	evs := f.run(t, personCtx())

	assertKinds(t, evs, "parked", "pause:T")
	assertTerminalDropped(t, f, evs, []string{"T"})
}

// P9: whether the gated call parks does not depend on where the draft sits in the batch.
func TestDraftAndGatedWithATextResponseParkBothAndDropTheTerminal(t *testing.T) {
	send := toolCall("S", "calendar__send_email", `{"to":["anna@example.com"],"subject":"Fattura","body":"In allegato."}`)
	gated := toolCall("G", "skill_manage", gatedDeleteArgs)
	for name, batch := range map[string][]llm.ToolCall{
		"draft first": {send, gated, terminalCallF},
		"gated first": {gated, send, terminalCallF},
	} {
		t.Run(name, func(t *testing.T) {
			f := newParkFixture(t, true, callsRound(batch...))
			evs := f.run(t, personCtx())

			var want []string
			for _, c := range batch {
				if c.Function.Name != terminalTool {
					want = append(want, c.ID)
				}
			}
			pauses := pausesIn(evs)
			if len(pauses) != 2 || pauses[0].ToolCallID != want[0] || pauses[1].ToolCallID != want[1] {
				t.Fatalf("pauses = %+v, want %v in model order", pauses, want)
			}
			gatedPause := pauses[slices.Index(want, "G")]
			if gatedPause.Approval == nil || gatedPause.Approval.CallIndex != slices.Index(want, "G") {
				t.Fatalf("gated pause = %+v, want an approval at its index in the parked turn", gatedPause)
			}
			assertTerminalDropped(t, f, evs, want)
		})
	}
}

// D14: with no draft or host review parking it, the terminal stays in the batch the gateway
// source reads, and today's terminal-exclusivity rejection answers the round.
func TestGatedCallWithATextResponseKeepsTheRejection(t *testing.T) {
	f := newParkFixture(t, true, callsRound(toolCall("G", "skill_manage", gatedDeleteArgs), terminalCallF))
	evs := f.run(t, personCtx())

	if pauses := pausesIn(evs); len(pauses) != 0 || hasParkedTurn(evs) {
		t.Fatalf("parked: %+v", pauses)
	}
	var rejected bool
	for _, m := range f.agent.HistoryForTest() {
		if m.Role == llm.RoleTool && m.ToolCallID == "G" && strings.Contains(m.Content, "a terminal text_response cannot be combined") {
			rejected = true
		}
	}
	if !rejected || len(f.log.entries()) != 0 {
		t.Fatalf("rejected %v, ran %v; want the rejection and no run", rejected, f.log.entries())
	}
}

// D19: a dedup trip among the siblings answers them through the stream, since the runner
// persists only what it sees, and the round ends on the pauses with no finalize call.
func TestDedupTripInAParkRoundYieldsItsResults(t *testing.T) {
	f := newParkFixture(t, true,
		callsRound(toolCall("A1", "read_notes", readA)),
		callsRound(toolCall("A2", "read_notes", readA), toolCall("G", "skill_manage", gatedDeleteArgs)),
	)
	budget, err := NewBudget(BudgetOptions{DedupWindow: new(2)})
	if err != nil {
		t.Fatal(err)
	}
	evs, err := collectInternal(f.agent.Run(InvocationContext{Ctx: personCtx(), RequestID: uuid.Must(uuid.NewV7()), Budget: budget}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	assertKinds(t, evs, "start:A1", "end:A1", "parked", "end:A2", "pause:G")
	end := evs[slices.IndexFunc(evs, func(ev *Event) bool { return eventKind(ev) == "end:A2" })].Actions.ToolInvocation
	if !strings.Contains(end.ResultPreview, "duplicate call suppressed") {
		t.Fatalf("A2's result = %q, want the suppression text", end.ResultPreview)
	}
	if f.client.streamCount() != 2 || hasFinalAnswer(evs) {
		t.Fatalf("model calls %d, final answer %v; want 2 and none", f.client.streamCount(), hasFinalAnswer(evs))
	}
	assertHistoryTail(t, f.agent.HistoryForTest(), []string{"A2", "G"}, []string{"A2"})
}
