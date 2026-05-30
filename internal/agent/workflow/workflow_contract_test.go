package workflow_test

import (
	"context"
	"iter"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/workflow"
	"github.com/chetto1983/aura/internal/llm"
)

// TestWorkflowAgents_InterfaceContract exercises the Agent accessor surface
// (Name/Description/SubAgents) and the FindAgent recursion both orchestrators
// share via findInTree — self-match, child-match, nested-match, and absent.
func TestWorkflowAgents_InterfaceContract(t *testing.T) {
	leaf := &agenttest.RecordingAgent{AgentName: "leaf"}
	inner := workflow.NewSequential("inner", leaf)
	root := workflow.NewLoop("root", 1, inner)

	for _, a := range []agent.Agent{inner, root} {
		if a.Name() == "" {
			t.Errorf("Name must be non-empty")
		}
		if a.Description() == "" {
			t.Errorf("Description must be non-empty")
		}
		if len(a.SubAgents()) != 1 {
			t.Errorf("%s: want 1 sub-agent, got %d", a.Name(), len(a.SubAgents()))
		}
	}

	if got := root.FindAgent("root"); got != root {
		t.Errorf("FindAgent(self): want root, got %v", got)
	}
	if got := root.FindAgent("inner"); got != inner {
		t.Errorf("FindAgent(child): want inner, got %v", got)
	}
	if got := root.FindAgent("leaf"); got != agent.Agent(leaf) {
		t.Errorf("FindAgent(nested leaf): want leaf, got %v", got)
	}
	if got := root.FindAgent("absent"); got != nil {
		t.Errorf("FindAgent(absent): want nil, got %v", got)
	}
}

// TestSequentialAgent_RootBranchHasNoParentDot covers joinBranch's empty-parent
// path: a sub of a root-branch-less Sequential sees just its own name.
func TestSequentialAgent_RootBranchHasNoParentDot(t *testing.T) {
	rec := &agenttest.RecordingAgent{AgentName: "only", Events: []*agent.Event{{}}}
	seq := workflow.NewSequential("root", rec)
	ic := agent.InvocationContext{Ctx: context.Background(), Branch: ""}
	b, err := agent.NewBudgetFromEnv()
	if err != nil {
		t.Fatalf("NewBudgetFromEnv: %v", err)
	}
	ic.Budget = b

	if _, err := drain(seq.Run(ic)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rec.SeenBranches) != 1 || rec.SeenBranches[0] != "only" {
		t.Errorf("root branch label: want %q, got %v", "only", rec.SeenBranches)
	}
}

// malformedArgsAgent emits one tool call whose Arguments are NOT valid JSON, to
// cover canonArgs's raw-bytes fallback (a malformed-args tool still fingerprints
// stably rather than erroring out of the loop).
type malformedArgsAgent struct{ name string }

func (a *malformedArgsAgent) Name() string                 { return a.name }
func (*malformedArgsAgent) Description() string            { return "test: malformed tool args" }
func (*malformedArgsAgent) SubAgents() []agent.Agent       { return nil }
func (a *malformedArgsAgent) FindAgent(string) agent.Agent { return nil }
func (a *malformedArgsAgent) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	call := llm.ToolCall{ID: "c", Type: "function"}
	call.Function.Name = "broken"
	call.Function.Arguments = "{not-json"
	return func(yield func(*agent.Event, error) bool) {
		for {
			ev := &agent.Event{Author: a.name, LLMResponse: &agent.LLMResponse{ToolCalls: []llm.ToolCall{call}}}
			if !yield(ev, nil) {
				return
			}
		}
	}
}

// TestLoopAgent_MalformedToolArgs_FallsBackToRawFingerprint ensures the loop still
// terminates cleanly (here: via dedup on the stable raw-bytes fingerprint) when a
// tool's args are not valid JSON — canonArgs falls back to the raw bytes.
func TestLoopAgent_MalformedToolArgs_FallsBackToRawFingerprint(t *testing.T) {
	lp := workflow.NewLoop("loop", 0, &malformedArgsAgent{name: "broken"})
	ic := newTestIC(t, "root")
	evs, err := drain(lp.Run(ic))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	final := evs[len(evs)-1]
	if !final.Actions.Escalate {
		t.Errorf("loop must terminate with an explicit escalate Event, got %+v", final.Actions)
	}
}
