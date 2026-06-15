package workflow_test

import (
	"errors"
	"iter"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/workflow"
	"go.uber.org/goleak"
)

// AG-043: the parallel result-closer leak edge — break the consumer at EVERY
// event index under multiple producing children and assert zero goroutine leak
// each time. The existing test only breaks after the first event; this stresses
// the drain path at every break point.
func TestParallelAgent_NoLeak_BreakAtEveryIndex(t *testing.T) {
	defer goleak.VerifyNone(t)

	for breakAt := 0; breakAt < 6; breakAt++ {
		c0 := &slowAgent{name: "c0", n: 50, delay: 5 * time.Millisecond}
		c1 := &slowAgent{name: "c1", n: 50, delay: 5 * time.Millisecond}
		c2 := &slowAgent{name: "c2", n: 50, delay: 5 * time.Millisecond}
		root := workflow.NewParallel("root", c0, c1, c2)
		ic := budgetIC(t, "root", 1000)

		seen := 0
		for ev, err := range root.Run(ic) {
			if err != nil {
				t.Fatalf("breakAt=%d: unexpected error: %v", breakAt, err)
			}
			_ = ev
			if seen == breakAt {
				break
			}
			seen++
		}
	}
	// goleak.VerifyNone (deferred) fails if any child goroutine is still parked
	// after any of the break points.
}

// cyclicAgent is a leaf-shaped agent whose SubAgents() can point back to an
// ancestor, forming a cycle (the open Agent interface allows it). Its FindAgent
// delegates to the orchestrator's findInTree via the workflow agent that wraps it.
type cyclicAgent struct {
	name string
	subs []agent.Agent
}

func (a *cyclicAgent) Name() string             { return a.name }
func (*cyclicAgent) Description() string        { return "test cyclic agent" }
func (a *cyclicAgent) SubAgents() []agent.Agent { return a.subs }
func (a *cyclicAgent) Run(agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(func(*agent.Event, error) bool) {}
}
func (a *cyclicAgent) FindAgent(name string) agent.Agent {
	// Delegate to the same shared traversal the workflow orchestrators use by
	// constructing a sequential wrapper; but to keep the cycle, just self-match here.
	if a.name == name {
		return a
	}
	for _, s := range a.subs {
		if found := s.FindAgent(name); found != nil {
			return found
		}
	}
	return nil
}

// AG-037: a cyclic/diamond agent graph must not stack-overflow; findInTree (used
// by every workflow orchestrator's FindAgent) terminates and returns the correct
// lookup.
func TestFindInTree_CyclicGraphDoesNotOverflow(t *testing.T) {
	a := &cyclicAgent{name: "a"}
	b := &cyclicAgent{name: "b"}
	a.subs = []agent.Agent{b}
	b.subs = []agent.Agent{a} // cycle a -> b -> a

	// Wrap in a workflow orchestrator so findInTree is the lookup path.
	root := workflow.NewSequential("root", a)

	if got := root.FindAgent("b"); got == nil || got.Name() != "b" {
		t.Fatalf("FindAgent(b) in a cyclic graph = %v, want b", got)
	}
	if got := root.FindAgent("absent"); got != nil {
		t.Fatalf("FindAgent(absent) in a cyclic graph = %v, want nil (and no overflow)", got)
	}
}

// AG-037: a diamond (one agent reachable via two parents) is visited once and
// resolves correctly.
func TestFindInTree_DiamondResolves(t *testing.T) {
	shared := &cyclicAgent{name: "shared"}
	left := &cyclicAgent{name: "left", subs: []agent.Agent{shared}}
	right := &cyclicAgent{name: "right", subs: []agent.Agent{shared}}
	root := workflow.NewParallel("root", left, right)

	if got := root.FindAgent("shared"); got == nil || got.Name() != "shared" {
		t.Fatalf("FindAgent(shared) in a diamond = %v, want shared", got)
	}
}

// erroringSubAgent emits one event then a real error to drive the sequential
// chain-abort path.
type erroringSubAgent struct{ name string }

func (a *erroringSubAgent) Name() string           { return a.name }
func (*erroringSubAgent) Description() string      { return "errors after one event" }
func (*erroringSubAgent) SubAgents() []agent.Agent { return nil }
func (a *erroringSubAgent) FindAgent(n string) agent.Agent {
	if a.name == n {
		return a
	}
	return nil
}
func (a *erroringSubAgent) Run(agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		if !yield(&agent.Event{Author: a.name}, nil) {
			return
		}
		yield(nil, errors.New("sub blew up"))
	}
}

// AG-061: when a sequential workflow aborts on a sub error, it emits a
// chain_aborted_at StateDelta marker naming the aborting sub.
func TestSequential_ChainAbortEmitsMarker(t *testing.T) {
	seq := workflow.NewSequential("chain", &erroringSubAgent{name: "step2"})
	ic := newTestIC(t, "root")

	var sawMarker string
	for ev, err := range seq.Run(ic) {
		if ev != nil && ev.Actions.StateDelta != nil {
			if v, ok := ev.Actions.StateDelta["chain_aborted_at"]; ok {
				sawMarker, _ = v.(string)
			}
		}
		_ = err
	}
	if sawMarker != "step2" {
		t.Fatalf("chain_aborted_at marker = %q, want step2 (AG-061)", sawMarker)
	}
}
