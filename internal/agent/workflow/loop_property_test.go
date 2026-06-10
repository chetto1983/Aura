package workflow_test

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent/workflow"
	"pgregory.net/rapid"
)

func TestLoopAgent_Property_EscalateYieldedBeforeReturn(t *testing.T) {
	// D-21: whenever a sub escalates, the LoopAgent MUST yield that escalate Event
	// before the iterator returns; the escalate is never swallowed.
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
