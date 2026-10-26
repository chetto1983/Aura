package workflow_test

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
)

// newTestIC builds a minimal single-Run-scoped InvocationContext for workflow
// tests: a real Budget from the builtin defaults, a background context, and the
// given root branch label.
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
