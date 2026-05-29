package workflow_test

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/workflow"
)

// newTestIC builds a minimal single-Run-scoped InvocationContext for workflow
// tests: a real Budget from the builtin defaults (Sequential never consumes it),
// a background context, and the given root branch label.
func newTestIC(t *testing.T, branch string) agent.InvocationContext {
	t.Helper()
	b, err := agent.NewBudgetFromEnv()
	if err != nil {
		t.Fatalf("NewBudgetFromEnv: %v", err)
	}
	return agent.InvocationContext{
		Ctx:    context.Background(),
		Branch: branch,
		Budget: b,
	}
}

// drain collects every Event the iterator yields and the first non-nil error.
func drain(seq func(yield func(*agent.Event, error) bool)) ([]*agent.Event, error) {
	var evs []*agent.Event
	var firstErr error
	for ev, err := range seq {
		if err != nil && firstErr == nil {
			firstErr = err
		}
		evs = append(evs, ev)
	}
	return evs, firstErr
}

func TestSequentialAgent_RunsAllSubsInOrder(t *testing.T) {
	a := &agenttest.RecordingAgent{AgentName: "A", Events: []*agent.Event{{}}}
	b := &agenttest.RecordingAgent{AgentName: "B", Events: []*agent.Event{{}}}
	c := &agenttest.RecordingAgent{AgentName: "C", Events: []*agent.Event{{}}}

	seq := workflow.NewSequential("root", a, b, c)
	ic := newTestIC(t, "root")

	evs, err := drain(seq.Run(ic))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"A", "B", "C"}
	if len(evs) != len(want) {
		t.Fatalf("event count: want %d, got %d", len(want), len(evs))
	}
	for i, ev := range evs {
		if ev.Author != want[i] {
			t.Errorf("event %d author: want %q, got %q", i, want[i], ev.Author)
		}
	}

	// Each sub must have seen its dot-joined branch label (D-15).
	for _, rec := range []*agenttest.RecordingAgent{a, b, c} {
		if len(rec.SeenBranches) != 1 {
			t.Fatalf("%s: want 1 Run invocation, got %d", rec.Name(), len(rec.SeenBranches))
		}
		want := "root." + rec.Name()
		if rec.SeenBranches[0] != want {
			t.Errorf("%s branch: want %q, got %q", rec.Name(), want, rec.SeenBranches[0])
		}
	}
}

func TestSequentialAgent_PropagatesEscalate(t *testing.T) {
	a := &agenttest.RecordingAgent{AgentName: "A", Events: []*agent.Event{{}}}
	b := &agenttest.EmitNThenEscalate{AgentName: "B", N: 0} // escalates immediately
	c := &agenttest.RecordingAgent{AgentName: "C", Events: []*agent.Event{{}}}

	seq := workflow.NewSequential("root", a, b, c)
	ic := newTestIC(t, "root")

	evs, err := drain(seq.Run(ic))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A's event, then B's escalate event; C must NOT be invoked.
	if len(evs) != 2 {
		t.Fatalf("event count: want 2 (A + B-escalate), got %d", len(evs))
	}
	if evs[0].Author != "A" {
		t.Errorf("event 0 author: want %q, got %q", "A", evs[0].Author)
	}
	if evs[1].Author != "B" || !evs[1].Actions.Escalate {
		t.Errorf("event 1: want B with escalate, got author=%q escalate=%v", evs[1].Author, evs[1].Actions.Escalate)
	}
	if len(c.SeenBranches) != 0 {
		t.Errorf("sub C must not be invoked after B escalates; saw %d invocations", len(c.SeenBranches))
	}
}
