package agent

import (
	"context"
	"iter"
	"testing"
)

// stubAgent is a minimal in-test Agent implementation. The reusable mock set
// (InfiniteToolCallAgent, RecordingAgent, ...) lands in internal/agent/agenttest
// in Plan 02-04; here a leaf stub is enough to exercise the context contract and
// prove the open interface (no seal) is implementable outside this package's
// constructors.
type stubAgent struct {
	name string
	subs []Agent
}

func (s stubAgent) Name() string        { return s.name }
func (s stubAgent) Description() string { return "stub" }
func (s stubAgent) SubAgents() []Agent  { return s.subs }
func (s stubAgent) Run(InvocationContext) iter.Seq2[*Event, error] {
	return func(yield func(*Event, error) bool) {}
}

// FindAgent checks self then recurses into subs (the contract D-01 specifies).
func (s stubAgent) FindAgent(name string) Agent {
	if s.name == name {
		return s
	}
	for _, sub := range s.subs {
		if found := sub.FindAgent(name); found != nil {
			return found
		}
	}
	return nil
}

// compile-time assertion that a non-constructor type satisfies the OPEN interface.
var _ Agent = stubAgent{}

func TestInvocationContext_WithSubAgent_CopiesWithoutMutating(t *testing.T) {
	root := stubAgent{name: "root"}
	sub := stubAgent{name: "sub"}
	ic := InvocationContext{Ctx: context.Background(), Agent: root, Branch: "root"}

	child := ic.WithSubAgent(sub)

	if ic.Agent.Name() != "root" {
		t.Fatalf("receiver mutated: want Agent=root, got %q", ic.Agent.Name())
	}
	if child.Agent.Name() != "sub" {
		t.Fatalf("copy not re-pointed: want Agent=sub, got %q", child.Agent.Name())
	}
	if child.Branch != "root" {
		t.Fatalf("unrelated field lost in copy: want Branch=root, got %q", child.Branch)
	}
}

func TestInvocationContext_WithContext_CopiesWithoutMutating(t *testing.T) {
	type key struct{}
	base := context.Background()
	ic := InvocationContext{Ctx: base, Agent: stubAgent{name: "root"}}

	newCtx := context.WithValue(base, key{}, "v")
	child := ic.WithContext(newCtx)

	if ic.Ctx != base {
		t.Fatal("WithContext mutated the receiver's Ctx")
	}
	if child.Ctx.Value(key{}) != "v" {
		t.Fatal("copy did not carry the new context")
	}
	if child.Agent.Name() != "root" {
		t.Fatalf("unrelated field lost: want Agent=root, got %q", child.Agent.Name())
	}
}

func TestAgent_FindAgent_RecursesIntoSubs(t *testing.T) {
	tree := stubAgent{
		name: "root",
		subs: []Agent{stubAgent{name: "a"}, stubAgent{name: "b", subs: []Agent{stubAgent{name: "deep"}}}},
	}
	if got := tree.FindAgent("root"); got == nil || got.Name() != "root" {
		t.Fatalf("FindAgent(root) failed: %v", got)
	}
	if got := tree.FindAgent("deep"); got == nil || got.Name() != "deep" {
		t.Fatalf("FindAgent(deep) failed: %v", got)
	}
	if got := tree.FindAgent("missing"); got != nil {
		t.Fatalf("FindAgent(missing) must be nil, got %v", got)
	}
}
