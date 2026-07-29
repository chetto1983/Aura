package swarm

import (
	"context"
	"fmt"
	"testing"

	"go.uber.org/goleak"
	"pgregory.net/rapid"
)

// TestSwarmProperties (D-25) holds the rapid invariants over goals arrays length
// 1..8 with a randomized per-child outcome mix:
//   - report length == len(goals) and report[i].GoalIndex == i (order preserved);
//   - per-child isolation: a failed/paused child never flips a sibling's status;
//   - tree steps ≤ parent remaining at spawn (the shared *atomic.Int32 bound);
//   - goleak-clean after every return.
func TestSwarmProperties(t *testing.T) {
	defer goleak.VerifyNone(t)
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 8).Draw(rt, "goals")
		kinds := []string{"ok", "fail", "pause"}

		r := newRouter()
		goals := make([]string, n)
		want := make([]string, n)
		for i := range n {
			sub := fmt.Sprintf("task-%d", i)
			goals[i] = sub + " body"
			kind := rapid.SampledFrom(kinds).Draw(rt, fmt.Sprintf("kind-%d", i))
			want[i] = expectedStatus(kind)
			switch kind {
			case "fail":
				r.route(sub, outcome{kind: "fail"})
			case "pause":
				r.route(sub, outcome{kind: "pause", question: "q " + sub, options: []string{"a", "b"}})
			default:
				r.route(sub, outcome{kind: "ok", text: "done " + sub})
			}
		}

		// Generous budget so the preflight always admits the spawn; the property is
		// about the SHARED bound, not exhaustion.
		const parentSteps = 100
		rc := testRunConfig(rt, r, parentSteps)
		remAtSpawn := rc.ParentBudget.Remaining()

		out, err := Run(context.Background(), rc, goals)
		if err != nil {
			rt.Fatalf("Run: %v", err)
		}
		reports := parseReports(rt, out)

		if len(reports) != n {
			rt.Fatalf("len(reports)=%d, want %d", len(reports), n)
		}
		for i, rep := range reports {
			if rep.GoalIndex != i {
				rt.Errorf("report[%d].GoalIndex=%d (order broken)", i, rep.GoalIndex)
			}
			if rep.ChildID != fmt.Sprintf("w%d", i+1) {
				rt.Errorf("report[%d].ChildID=%q, want w%d", i, rep.ChildID, i+1)
			}
			if rep.Status != want[i] {
				rt.Errorf("report[%d].Status=%q, want %q (per-child isolation)", i, rep.Status, want[i])
			}
		}

		// Tree steps consumed ≤ parent remaining at spawn (shared *atomic.Int32).
		consumed := remAtSpawn - rc.ParentBudget.Remaining()
		if consumed < 0 || consumed > remAtSpawn {
			rt.Errorf("tree consumed %d steps, want within [0,%d]", consumed, remAtSpawn)
		}
	})
}

// expectedStatus maps a scripted outcome kind to the ChildReport status it must
// produce, so the property asserts exact per-child isolation.
func expectedStatus(kind string) string {
	switch kind {
	case "fail":
		return StatusFailed
	case "pause":
		return StatusNeedsUserInput
	default:
		return StatusOK
	}
}
