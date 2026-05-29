// Package agenttest holds the shared Agent mocks (D-07) that the workflow tests
// (Plan 05 Sequential/Loop, Plan 06 Parallel), the CLI dry-run (Plan 07), and
// future Phase 3/9 all reuse — one source of truth, zero inline mock
// duplication (CLAUDE.md "reusable code").
//
// Import direction is ONE-WAY (RESEARCH OQ#2): agenttest imports internal/agent;
// agent never imports agenttest outside _test.go, so there is no cycle. This is
// the standard Go test-helper package layout.
//
// Budget invariant (Pitfall 3 / T-02-09): no mock here calls NewBudgetFromEnv.
// A mock that forked a fresh budget would silently break the shared-counter
// guarantee that makes SC#3 true (depth-3 fan-3 ≤ max_steps, NOT max_steps³).
// The counting mock therefore consumes ONLY ic.Budget, the single shared
// *atomic.Int32 threaded down the tree — and if a future wrapper exposes a mock
// as a tool it MUST thread that same Budget, never construct a new one.
package agenttest

import (
	"iter"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
)

// Compile-time assertions that every mock satisfies the real 02-02 Agent
// interface. If any method drifts, the build breaks here.
var (
	_ agent.Agent = (*InfiniteToolCallAgent)(nil)
	_ agent.Agent = (*EmitNThenEscalate)(nil)
	_ agent.Agent = (*RecordingAgent)(nil)
	_ agent.Agent = (*CountingAgent)(nil)
)

// InfiniteToolCallAgent is the SC#2 budget-exhaustion fixture: its Run yields an
// Event carrying the SAME llm.ToolCall (fixed name + args) on every iteration,
// forever. It has no natural termination — only the LoopAgent's Budget stops it.
// The yield-after-false guard (D-22 footgun 2) is the ONLY exit path, so a
// consumer that breaks does not trip a "continued iteration" panic.
type InfiniteToolCallAgent struct {
	AgentName string // Author stamped on every emitted Event; defaults to "infinite" if empty
	ToolName  string // llm.ToolCall.Function.Name repeated forever; defaults to "noop"
	ToolArgs  string // llm.ToolCall.Function.Arguments repeated forever; defaults to "{}"
}

// Name is the Event Author / FindAgent key for this mock.
func (a *InfiniteToolCallAgent) Name() string { return orDefault(a.AgentName, "infinite") }

// Description is the human/LLM-facing one-liner.
func (*InfiniteToolCallAgent) Description() string {
	return "test mock: emits the same tool call forever (SC#2 budget-exhaustion fixture)"
}

// Run yields the same llm.ToolCall on every iteration, forever.
func (a *InfiniteToolCallAgent) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	call := llm.ToolCall{ID: "call-0", Type: "function"}
	call.Function.Name = orDefault(a.ToolName, "noop")
	call.Function.Arguments = orDefault(a.ToolArgs, "{}")
	author := a.Name()
	return func(yield func(*agent.Event, error) bool) {
		for { // never terminates on its own — the consumer's break/budget is the exit
			ev := &agent.Event{
				Author:      author,
				Branch:      ic.Branch,
				LLMResponse: &agent.LLMResponse{ToolCalls: []llm.ToolCall{call}},
			}
			if !yield(ev, nil) {
				return // D-22 footgun 2: yield returned false, stop without a bare yield
			}
		}
	}
}

// SubAgents returns nil — this is a leaf mock.
func (*InfiniteToolCallAgent) SubAgents() []agent.Agent { return nil }

// FindAgent returns self when name matches, else nil.
func (a *InfiniteToolCallAgent) FindAgent(name string) agent.Agent {
	return selfIfNamed(a, name)
}

// EmitNThenEscalate emits N normal Events, then one Event with
// Actions.Escalate=true, then stops. It drives escalate-propagation tests
// (the escalate Event is the canonical termination signal, D-04).
type EmitNThenEscalate struct {
	AgentName string // Author stamped on every Event; defaults to "escalator" if empty
	N         int    // number of normal Events before the escalate Event
}

// Name is the Event Author / FindAgent key for this mock.
func (a *EmitNThenEscalate) Name() string { return orDefault(a.AgentName, "escalator") }

// Description is the human/LLM-facing one-liner.
func (*EmitNThenEscalate) Description() string {
	return "test mock: emits N normal events then one escalate event (escalate-propagation fixture)"
}

// Run emits N normal Events then one Event with Actions.Escalate=true.
func (a *EmitNThenEscalate) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	author := a.Name()
	return func(yield func(*agent.Event, error) bool) {
		for range a.N {
			if !yield(&agent.Event{Author: author, Branch: ic.Branch}, nil) {
				return
			}
		}
		yield(&agent.Event{
			Author:  author,
			Branch:  ic.Branch,
			Actions: agent.Actions{Escalate: true},
		}, nil) // terminal event — return regardless of the yield result
	}
}

// SubAgents returns nil — this is a leaf mock.
func (*EmitNThenEscalate) SubAgents() []agent.Agent { return nil }

// FindAgent returns self when name matches, else nil.
func (a *EmitNThenEscalate) FindAgent(name string) agent.Agent { return selfIfNamed(a, name) }

// RecordingAgent records every Event it emits and the Branch/RequestID of each
// InvocationContext it sees, so workflow tests can assert emission order and the
// hierarchical branch labels (D-15) a parent stamped on it. It emits exactly the
// Events in Events (which may be empty for a pure observer).
type RecordingAgent struct {
	AgentName string         // Author stamped on emitted Events; defaults to "recorder" if empty
	Events    []*agent.Event // canned events to emit, in order (Author/Branch filled at Run time)

	SeenBranches []string // Branch of each InvocationContext Run was called with, in order
	Emitted      []*agent.Event
}

// Name is the Event Author / FindAgent key for this mock.
func (a *RecordingAgent) Name() string { return orDefault(a.AgentName, "recorder") }

// Description is the human/LLM-facing one-liner.
func (*RecordingAgent) Description() string {
	return "test mock: records seen branches and emitted events for order/label assertions"
}

// Run records the seen Branch and emits the canned Events in order.
func (a *RecordingAgent) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	author := a.Name()
	return func(yield func(*agent.Event, error) bool) {
		a.SeenBranches = append(a.SeenBranches, ic.Branch)
		for _, src := range a.Events {
			ev := *src // copy so the canned template is not mutated across runs
			ev.SetAuthorIfEmpty(author)
			if ev.Branch == "" {
				ev.Branch = ic.Branch
			}
			a.Emitted = append(a.Emitted, &ev)
			if !yield(&ev, nil) {
				return
			}
		}
	}
}

// SubAgents returns nil — this is a leaf mock.
func (*RecordingAgent) SubAgents() []agent.Agent { return nil }

// FindAgent returns self when name matches, else nil.
func (a *RecordingAgent) FindAgent(name string) agent.Agent { return selfIfNamed(a, name) }

// CountingAgent is the SC#3 shared-counter fixture. Each Run iteration calls
// ic.Budget.ConsumeStep() — the single shared *atomic.Int32 threaded down the
// whole tree (D-10) — and increments Calls in lockstep. It stops the instant the
// shared budget refuses a step, so a depth-N tree of CountingAgents consumes
// total ≤ max_steps, NOT max_steps^depth. It NEVER calls NewBudgetFromEnv:
// forking a fresh budget would reintroduce the depth³ trap (Pitfall 3 / T-02-09),
// so a future wrapper that exposes this mock as a tool MUST thread the same
// ic.Budget rather than build a new one.
type CountingAgent struct {
	AgentName string // Author stamped on emitted Events; defaults to "counter" if empty

	Calls int // successful ConsumeStep calls this mock made (test reads this)
}

// Name is the Event Author / FindAgent key for this mock.
func (a *CountingAgent) Name() string { return orDefault(a.AgentName, "counter") }

// Description is the human/LLM-facing one-liner.
func (*CountingAgent) Description() string {
	return "test mock: consumes the shared budget counter once per step (SC#3 shared-counter fixture)"
}

// Run consumes ic.Budget (the shared counter) once per step until it is refused.
func (a *CountingAgent) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	author := a.Name()
	return func(yield func(*agent.Event, error) bool) {
		for {
			ok, reason := ic.Budget.ConsumeStep()
			if !ok {
				yield(&agent.Event{
					Author: author,
					Branch: ic.Branch,
					Actions: agent.Actions{StateDelta: map[string]any{
						"termination_reason": "budget_exhausted",
						"limit_hit":          reason,
					}},
				}, nil) // terminal event — the shared budget refused this step
				return
			}
			a.Calls++
			if !yield(&agent.Event{Author: author, Branch: ic.Branch}, nil) {
				return
			}
		}
	}
}

// SubAgents returns nil — this is a leaf mock.
func (*CountingAgent) SubAgents() []agent.Agent { return nil }

// FindAgent returns self when name matches, else nil.
func (a *CountingAgent) FindAgent(name string) agent.Agent { return selfIfNamed(a, name) }

// orDefault returns v, or def when v is empty — lets each mock work zero-valued.
func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// selfIfNamed implements the leaf-agent FindAgent contract: self if the name
// matches, else nil (leaf mocks have no children to recurse into).
func selfIfNamed(a agent.Agent, name string) agent.Agent {
	if a.Name() == name {
		return a
	}
	return nil
}
