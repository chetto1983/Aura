package agent_test

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// finalizeAnswer is the synthesized prose the scripted finalize turn returns.
// Load-bearing: the assertions below check the terminal Event carries exactly it.
const finalizeAnswer = "Ecco la risposta finale sintetizzata dai risultati raccolti."

// assertFinalizedNonEmpty checks the terminal Event of a forced-finalization run:
// non-empty content (Req#2 — not the prose-less terminalBudgetEvent), NOT
// Escalate-only, and the matching termination_reason/limit_hit observability keys.
func assertFinalizedNonEmpty(t *testing.T, evs []*agent.Event, wantReason string) {
	t.Helper()
	if len(evs) == 0 {
		t.Fatal("no events emitted; want a terminal finalEvent")
	}
	last := evs[len(evs)-1]
	if last.LLMResponse == nil || last.LLMResponse.Content == "" {
		t.Fatalf("terminal event has empty content: %+v (want the non-empty synthesized answer)", last.LLMResponse)
	}
	if last.LLMResponse.Content != finalizeAnswer {
		t.Errorf("terminal content = %q, want the synthesized answer %q", last.LLMResponse.Content, finalizeAnswer)
	}
	if last.Actions.Escalate {
		t.Error("terminal event is Escalate=true; a forced-finalization terminal must be prose, not an Escalate-only signal")
	}
	if got := last.Actions.StateDelta["limit_hit"]; got != wantReason {
		t.Errorf("limit_hit = %v, want %q", got, wantReason)
	}
	if got := last.Actions.StateDelta["termination_reason"]; got != "budget_exhausted" {
		t.Errorf("termination_reason = %v, want budget_exhausted (observability preserved)", got)
	}
}

// TestFinalize_DedupTrip: a run that trips the window-3 dedup ring ends with a
// non-empty finalEvent synthesized from a tool-free turn, NOT a prose-less
// terminalBudgetEvent. echo returns a stable result so the dedup veto fires (the
// progress signal does not suppress it). The finalize turn rides outside the
// budget, so the scripted finalize TextChunks turn is the one the agent consumes.
func TestFinalize_DedupTrip(t *testing.T) {
	recordingProvider(t)
	// The window-3 ring vetoes on the third identical (name,args) call with a stable
	// result (probed deterministically): the third dispatch trips dedup, and finalize
	// then consumes the NEXT scripted turn — so the finalize synthesis turn must
	// immediately follow the three echo turns (finalize is Stream call #4). A trailing
	// guard turn would only be reached on a bug (no trip), where the script-exhausted
	// empty turn already makes the assertion fail loudly.
	const echoTurnsBeforeTrip = 3
	turns := make([]agenttest.FakeTurn, 0, echoTurnsBeforeTrip+1)
	for i := 0; i < echoTurnsBeforeTrip; i++ {
		turns = append(turns, agenttest.ToolCallTurn(agenttest.MakeToolCall("c", "echo", `{"v":"same"}`)))
	}
	turns = append(turns, agenttest.TextChunks("stop", finalizeAnswer))
	fc := agenttest.NewFakeClient(turns...)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(50), DedupWindow: ptr(3)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("dedup trip surfaced an error slot (must finalize as a terminal Event): %v", err)
	}
	assertFinalizedNonEmpty(t, evs, "dedup")
}

// TestFinalize_MaxStepsTrip: a run with MaxSteps=1 that exhausts the step budget
// after one tool call ends with a non-empty finalEvent. The first ConsumeStep
// passes (one echo turn), the second trips max_steps, and finalize emits prose.
func TestFinalize_MaxStepsTrip(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c", "echo", `{"v":"hi"}`)),
		agenttest.TextChunks("stop", finalizeAnswer),
	)
	a := newAgent(t, fc, llm.Config{})
	ic := newIC(t, agent.BudgetOptions{MaxSteps: ptr(1)})

	evs, err := collect(a.Run(ic))
	if err != nil {
		t.Fatalf("max_steps trip surfaced an error slot: %v", err)
	}
	assertFinalizedNonEmpty(t, evs, "max_steps")
}

// TestFinalizeWallclock: an injected clock past the deadline trips wallclock on the
// very first ConsumeStep at :124, so finalize is the first (and only) consumed turn.
// The name MUST match VALIDATION.md's `go test -run 'TestFinalizeWallclock'`.
func TestFinalizeWallclock(t *testing.T) {
	recordingProvider(t)
	fc := agenttest.NewFakeClient(agenttest.TextChunks("stop", finalizeAnswer))
	a := newAgent(t, fc, llm.Config{})

	base := time.Now()
	calls := 0
	clock := func() time.Time {
		calls++
		if calls == 1 {
			return base // construction anchor
		}
		return base.Add(2 * time.Hour) // every ConsumeStep check is past the deadline
	}
	b, err := agent.NewBudget(agent.BudgetOptions{MaxWallclockSec: ptr(1), Now: clock})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	ic := agent.InvocationContext{Ctx: context.Background(), RequestID: uuid.Must(uuid.NewV7()), Budget: b}

	evs, rerr := collect(a.Run(ic))
	if rerr != nil {
		t.Fatalf("wallclock trip surfaced an error slot: %v", rerr)
	}
	assertFinalizedNonEmpty(t, evs, "wallclock")
}
