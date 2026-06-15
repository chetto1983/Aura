package workflow_test

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/workflow"
)

func TestLoopAgent_MaxIterZeroUsesDefaultCeiling(t *testing.T) {
	const defaultCeiling = 1000
	maxSteps := defaultCeiling + 5
	b, err := agent.NewBudget(agent.BudgetOptions{MaxSteps: &maxSteps})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	ic := agent.InvocationContext{Ctx: context.Background(), Branch: "root", Budget: b}
	sub := &budgetOwningToolAgent{name: "owned"}
	lp := workflow.NewLoop("loop", 0, sub)

	evs, err := drain(lp.Run(ic))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(evs) != defaultCeiling+1 {
		t.Fatalf("event count = %d, want %d child events + 1 terminal", len(evs), defaultCeiling)
	}
	final := evs[len(evs)-1]
	if !final.Actions.Escalate {
		t.Fatal("default ceiling terminal must escalate")
	}
	if got := final.Actions.StateDelta["termination_reason"]; got != "iteration_limit" {
		t.Fatalf("termination_reason = %v, want iteration_limit", got)
	}
	if got := final.Actions.StateDelta["limit_hit"]; got != "max_iterations" {
		t.Fatalf("limit_hit = %v, want max_iterations", got)
	}
	if got := final.Actions.StateDelta["steps_consumed"]; got != defaultCeiling {
		t.Fatalf("steps_consumed = %v, want %d", got, defaultCeiling)
	}
	if sub.runs != defaultCeiling {
		t.Fatalf("sub runs = %d, want %d", sub.runs, defaultCeiling)
	}
	if got := b.Remaining(); got != maxSteps-defaultCeiling {
		t.Fatalf("remaining budget = %d, want %d", got, maxSteps-defaultCeiling)
	}
}
