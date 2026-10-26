package workflow_test

import (
	"iter"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/workflow"
)

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
	// Self-match only, so the cycle stays: the orchestrator's findInTree is what
	// must terminate.
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
	root := workflow.NewLoop("root", 1, a)

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
	root := workflow.NewLoop("root", 1, left, right)

	if got := root.FindAgent("shared"); got == nil || got.Name() != "shared" {
		t.Fatalf("FindAgent(shared) in a diamond = %v, want shared", got)
	}
}
