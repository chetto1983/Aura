package workflow_test

import (
	"context"
	"iter"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/workflow"
	"github.com/chetto1983/aura/internal/llm"
	"pgregory.net/rapid"
)

// plainAgent emits exactly one Event with no tool calls per Run — a sub that
// "does one unit of work and finishes", used to count LoopAgent iterations.
type plainAgent struct {
	name string
	runs int // number of times Run was invoked (mutated in-test, single goroutine)
}

func (a *plainAgent) Name() string           { return a.name }
func (*plainAgent) Description() string      { return "test: one plain event per run" }
func (*plainAgent) SubAgents() []agent.Agent { return nil }
func (a *plainAgent) FindAgent(name string) agent.Agent {
	if a.name == name {
		return a
	}
	return nil
}
func (a *plainAgent) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		a.runs++
		yield(&agent.Event{Author: a.name, Branch: ic.Branch}, nil)
	}
}

// escalateOnNthRun escalates on its Nth Run invocation (1-based) and emits a
// plain event otherwise — drives "escalate at iteration K" (Req#5b).
type escalateOnNthRun struct {
	name string
	n    int
	runs int
}

func (a *escalateOnNthRun) Name() string           { return a.name }
func (*escalateOnNthRun) Description() string      { return "test: escalate on the Nth run" }
func (*escalateOnNthRun) SubAgents() []agent.Agent { return nil }
func (a *escalateOnNthRun) FindAgent(name string) agent.Agent {
	if a.name == name {
		return a
	}
	return nil
}
func (a *escalateOnNthRun) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		a.runs++
		esc := a.runs == a.n
		yield(&agent.Event{
			Author:  a.name,
			Branch:  ic.Branch,
			Actions: agent.Actions{Escalate: esc},
		}, nil)
	}
}

// scriptedToolAgent emits the SAME tool call (fixed name+args) on every Run, with
// a per-emission assistant Content drawn from contents in order (cycling). A
// constant Content drives a real dedup loop; a changing Content drives the
// progress veto (D-18). Used for the dedup window + veto tests.
type scriptedToolAgent struct {
	name     string
	toolName string
	toolArgs string
	contents []string // assistant Content per emission, cycled
	idx      int
}

func (a *scriptedToolAgent) Name() string { return a.name }
func (*scriptedToolAgent) Description() string {
	return "test: same tool call, scripted result content"
}
func (*scriptedToolAgent) SubAgents() []agent.Agent { return nil }
func (a *scriptedToolAgent) FindAgent(name string) agent.Agent {
	if a.name == name {
		return a
	}
	return nil
}
func (a *scriptedToolAgent) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	call := llm.ToolCall{ID: "call-0", Type: "function"}
	call.Function.Name = a.toolName
	call.Function.Arguments = a.toolArgs
	return func(yield func(*agent.Event, error) bool) {
		for {
			content := ""
			if len(a.contents) > 0 {
				content = a.contents[a.idx%len(a.contents)]
				a.idx++
			}
			ev := &agent.Event{
				Author: a.name,
				Branch: ic.Branch,
				LLMResponse: &agent.LLMResponse{
					Content:   content,
					ToolCalls: []llm.ToolCall{call},
				},
			}
			if !yield(ev, nil) {
				return
			}
		}
	}
}

// multiToolAgent emits ONE Event carrying N distinct tool calls per Run, forever
// (the constant content makes results stable; distinct names keep dedup from
// firing before the budget when the tools are exempt). It drives the WR-05
// multi-tool-per-turn budget-accounting test: each tool call is one budgeted step
// and one step Event.
type multiToolAgent struct {
	name      string
	toolNames []string
}

func (a *multiToolAgent) Name() string           { return a.name }
func (*multiToolAgent) Description() string      { return "test: N tool calls in one Event per run" }
func (*multiToolAgent) SubAgents() []agent.Agent { return nil }
func (a *multiToolAgent) FindAgent(name string) agent.Agent {
	if a.name == name {
		return a
	}
	return nil
}
func (a *multiToolAgent) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	calls := make([]llm.ToolCall, len(a.toolNames))
	for i, n := range a.toolNames {
		calls[i] = llm.ToolCall{ID: "call-" + n, Type: "function"}
		calls[i].Function.Name = n
		calls[i].Function.Arguments = "{}"
	}
	return func(yield func(*agent.Event, error) bool) {
		for {
			ev := &agent.Event{
				Author:      a.name,
				Branch:      ic.Branch,
				LLMResponse: &agent.LLMResponse{ToolCalls: calls},
			}
			if !yield(ev, nil) {
				return
			}
		}
	}
}

func TestLoopAgent_MultiToolPerTurn_StepsConsumedEqualsStepEvents(t *testing.T) {
	// WR-05: a turn that emits 2 tool calls consumes 2 budget steps and yields 2
	// step Events. With an ODD max_steps the budget trips on the SECOND tool call of
	// a turn — the FIRST call's already-consumed step must NOT be discarded, so the
	// count of yielded step Events must equal the terminal steps_consumed.
	t.Setenv("AURA_LOOP_MAX_STEPS", "5")
	t.Setenv("AURA_LOOP_DEDUP_EXEMPT_TOOLS", "noop1,noop2") // budget, not dedup, is the stop
	b, err := agent.NewBudgetFromEnv()
	if err != nil {
		t.Fatalf("NewBudgetFromEnv: %v", err)
	}
	ic := agent.InvocationContext{Ctx: context.Background(), Branch: "root", Budget: b}

	sub := &multiToolAgent{name: "multi", toolNames: []string{"noop1", "noop2"}}
	lp := workflow.NewLoop("loop", 0, sub)

	evs, err := drain(lp.Run(ic))
	if err != nil {
		t.Fatalf("termination must be Event-only (D-04): %v", err)
	}

	final := evs[len(evs)-1]
	if !final.Actions.Escalate {
		t.Fatalf("last Event must be the terminal escalate Event")
	}
	stepsConsumed, ok := final.Actions.StateDelta["steps_consumed"].(int)
	if !ok {
		t.Fatalf("steps_consumed missing/!int: %#v", final.Actions.StateDelta["steps_consumed"])
	}
	if stepsConsumed != 5 {
		t.Errorf("steps_consumed: want 5 (the hard cap), got %d", stepsConsumed)
	}

	stepEvents := evs[:len(evs)-1] // everything before the terminal is a step Event
	if len(stepEvents) != stepsConsumed {
		t.Fatalf("WR-05: steps_consumed=%d must equal yielded step Events=%d (no discarded mid-turn step)",
			stepsConsumed, len(stepEvents))
	}
	// Each step Event must carry exactly one tool call (the per-call scoping).
	for i, ev := range stepEvents {
		if ev.LLMResponse == nil || len(ev.LLMResponse.ToolCalls) != 1 {
			t.Errorf("step Event %d must carry exactly one tool call, got %#v", i, ev.LLMResponse)
		}
	}
	if final.Actions.StateDelta["limit_hit"] != "max_steps" {
		t.Errorf("limit_hit: want max_steps, got %v", final.Actions.StateDelta["limit_hit"])
	}
}

func TestLoopAgent_StopsAtMaxIterations(t *testing.T) {
	sub := &plainAgent{name: "worker"}
	lp := workflow.NewLoop("loop", 3, sub)
	ic := newTestIC(t, "root")

	evs, err := drain(lp.Run(ic))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(evs) != 3 {
		t.Fatalf("event count: want 3 (one per iteration), got %d", len(evs))
	}
	if sub.runs != 3 {
		t.Errorf("sub run count: want 3, got %d", sub.runs)
	}
	// Branch carries the per-iteration .iter-<N> label (D-15).
	wantBranches := []string{"root.iter-0.worker", "root.iter-1.worker", "root.iter-2.worker"}
	for i, ev := range evs {
		if ev.Branch != wantBranches[i] {
			t.Errorf("event %d branch: want %q, got %q", i, wantBranches[i], ev.Branch)
		}
	}
}

func TestLoopAgent_EscalatePropagation(t *testing.T) {
	sub := &escalateOnNthRun{name: "worker", n: 2} // escalate on the 2nd run (iter 1)
	lp := workflow.NewLoop("loop", 10, sub)
	ic := newTestIC(t, "root")

	evs, err := drain(lp.Run(ic))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("event count: want 2 (iter 0 plain + iter 1 escalate), got %d", len(evs))
	}
	if !evs[1].Actions.Escalate {
		t.Errorf("event 1 must be the escalate event")
	}
	if sub.runs != 2 {
		t.Errorf("sub must run exactly twice then loop returns; got %d", sub.runs)
	}
}

func TestLoopAgent_TerminatesAtMaxSteps_WithExplicitEvent(t *testing.T) {
	// The infinite tool must be EXEMPT from dedup (D-19) so the HARD max_steps cap
	// wins — this test asserts budget exhaustion, not dedup.
	t.Setenv("AURA_LOOP_MAX_STEPS", "25")
	t.Setenv("AURA_LOOP_DEDUP_EXEMPT_TOOLS", "noop")
	b, err := agent.NewBudgetFromEnv()
	if err != nil {
		t.Fatalf("NewBudgetFromEnv: %v", err)
	}
	ic := agent.InvocationContext{Ctx: context.Background(), Branch: "root", Budget: b}

	sub := &agenttest.InfiniteToolCallAgent{AgentName: "infinite", ToolName: "noop", ToolArgs: "{}"}
	lp := workflow.NewLoop("loop", 0, sub) // maxIter=0 → only the budget stops it

	evs, err := drain(lp.Run(ic))
	if err != nil {
		t.Fatalf("termination must be Event-only, never the error slot (D-04); got %v", err)
	}
	// 25 step Events + 1 terminal Event = 26 (SC#2 smoke contract).
	if len(evs) != 26 {
		t.Fatalf("event count: want 26 (25 step + 1 terminal), got %d", len(evs))
	}
	final := evs[len(evs)-1]
	if final.Author != "loop" {
		t.Errorf("terminal Author: want %q, got %q", "loop", final.Author)
	}
	if !final.Actions.Escalate {
		t.Errorf("terminal Event must set Escalate=true")
	}
	sd := final.Actions.StateDelta
	if got := sd["termination_reason"]; got != "budget_exhausted" {
		t.Errorf("termination_reason: want %q, got %v", "budget_exhausted", got)
	}
	if got := sd["limit_hit"]; got != "max_steps" {
		t.Errorf("limit_hit: want %q, got %v", "max_steps", got)
	}
	if got := sd["steps_consumed"]; got != 25 {
		t.Errorf("steps_consumed: want 25, got %v (%T)", got, got)
	}
}

func TestLoopAgent_DedupWindow_TerminatesOn3SameToolCalls(t *testing.T) {
	// Same name+args AND a constant result content → a real period-1 loop. With
	// dedupWindow=3 the 3rd identical call must terminate with limit_hit=dedup.
	sub := &scriptedToolAgent{
		name:     "repeater",
		toolName: "search",
		toolArgs: `{"q":"x"}`,
		contents: []string{"same-result"}, // constant → no progress veto
	}
	lp := workflow.NewLoop("loop", 0, sub)
	ic := newTestIC(t, "root")

	evs, err := drain(lp.Run(ic))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	final := evs[len(evs)-1]
	if got := final.Actions.StateDelta["limit_hit"]; got != "dedup" {
		t.Fatalf("limit_hit: want %q, got %v", "dedup", got)
	}
	if !final.Actions.Escalate {
		t.Errorf("dedup terminal Event must set Escalate=true")
	}
	// It must terminate well before the 25-step budget cap.
	if len(evs) > 5 {
		t.Errorf("dedup should fire within the window, got %d events", len(evs))
	}
}

func TestLoopAgent_DedupVeto_When_ResultChanges(t *testing.T) {
	// Same name+args but a CHANGING result content each time → progress veto (D-18):
	// dedup never fires, so termination is by the hard step budget, NOT dedup.
	t.Setenv("AURA_LOOP_MAX_STEPS", "10")
	b, err := agent.NewBudgetFromEnv()
	if err != nil {
		t.Fatalf("NewBudgetFromEnv: %v", err)
	}
	ic := agent.InvocationContext{Ctx: context.Background(), Branch: "root", Budget: b}

	sub := &scriptedToolAgent{
		name:     "progresser",
		toolName: "search",
		toolArgs: `{"q":"x"}`,
		contents: []string{"r0", "r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8", "r9", "r10"},
	}
	lp := workflow.NewLoop("loop", 0, sub)

	evs, err := drain(lp.Run(ic))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	final := evs[len(evs)-1]
	if got := final.Actions.StateDelta["limit_hit"]; got != "max_steps" {
		t.Fatalf("changing result must veto dedup → terminate by max_steps, got limit_hit=%v", got)
	}
	if got := final.Actions.StateDelta["steps_consumed"]; got != 10 {
		t.Errorf("steps_consumed: want 10, got %v", got)
	}
}

func TestLoopAgent_Property_EscalateYieldedBeforeReturn(t *testing.T) {
	// D-21: whenever a sub escalates, the LoopAgent MUST yield that escalate Event
	// before the iterator returns — the escalate is never swallowed.
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 8).Draw(rt, "escalateOnRun")
		maxIter := rapid.IntRange(1, n+5).Draw(rt, "maxIter")

		sub := &escalateOnNthRun{name: "worker", n: n}
		lp := workflow.NewLoop("loop", uint(maxIter), sub)
		ic := newTestIC(t, "root")

		var sawEscalate bool
		for ev, err := range lp.Run(ic) {
			if err != nil {
				rt.Fatalf("error slot must stay clean (D-04): %v", err)
			}
			if ev != nil && ev.Actions.Escalate {
				sawEscalate = true
			}
		}
		// If maxIter allowed reaching the Nth run, the escalate must have been seen.
		if maxIter >= n && !sawEscalate {
			rt.Fatalf("escalate on run %d with maxIter %d was not yielded before return", n, maxIter)
		}
	})
}
