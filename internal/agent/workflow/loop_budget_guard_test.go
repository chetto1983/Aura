package workflow_test

import (
	"context"
	"iter"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/workflow"
)

// budgetOwningNoOpAgent owns the budget but neither consumes it nor escalates —
// the degenerate case that makes a maxIter=0 parent hot-spin without a no-progress
// guard (B-07).
type budgetOwningNoOpAgent struct{ name string }

func (a *budgetOwningNoOpAgent) Name() string           { return a.name }
func (*budgetOwningNoOpAgent) Description() string      { return "test: owns budget, makes no progress" }
func (*budgetOwningNoOpAgent) SubAgents() []agent.Agent { return nil }
func (*budgetOwningNoOpAgent) OwnsBudget() bool         { return true }
func (a *budgetOwningNoOpAgent) FindAgent(name string) agent.Agent {
	if a.name == name {
		return a
	}
	return nil
}
func (a *budgetOwningNoOpAgent) Run(ic agent.InvocationContext) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		yield(&agent.Event{Author: a.name, Branch: ic.Branch}, nil)
	}
}

// TestLoopAgent_MaxIterZero_BudgetOwningNoProgress_Terminates covers B-07: a
// maxIter=0 loop whose sole budget-owning sub neither consumes the shared budget
// nor escalates must still terminate (no-progress guard) instead of hot-spinning.
func TestLoopAgent_MaxIterZero_BudgetOwningNoProgress_Terminates(t *testing.T) {
	maxSteps := 5
	b, err := agent.NewBudget(agent.BudgetOptions{MaxSteps: &maxSteps})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	ic := agent.InvocationContext{Ctx: context.Background(), Branch: "root", Budget: b}
	lp := workflow.NewLoop("loop", 0, &budgetOwningNoOpAgent{name: "noop"})

	count := 0
	var final *agent.Event
	for ev, err := range lp.Run(ic) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		count++
		if count > 100 {
			t.Fatal("B-07: maxIter=0 with a no-progress budget-owning sub hot-spins — no termination")
		}
		final = ev
	}
	if final == nil || !final.Actions.Escalate {
		t.Fatalf("loop must emit a terminal escalate event on no progress, got %+v", final)
	}
}
