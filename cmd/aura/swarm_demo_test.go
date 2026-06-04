package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/swarm"
)

// TestSwarmDemoDeterministic asserts `aura swarm-demo` drives the engine over the
// mock FakeClient and prints an ordered report array of length == goals count, all
// ok, with no live LLM or network call (D-16). The order is goal index (the engine
// orders by index, not completion), so the assertion is stable across the concurrent
// worker fan-out.
func TestSwarmDemoDeterministic(t *testing.T) {
	goals := []string{"first goal", "second goal", "third goal"}
	var buf bytes.Buffer
	if err := swarmDemo(&buf, goals, 50); err != nil {
		t.Fatalf("swarmDemo: %v", err)
	}

	var reports []swarm.ChildReport
	if err := json.Unmarshal(buf.Bytes(), &reports); err != nil {
		t.Fatalf("demo output is not a JSON report array: %v\n%s", err, buf.String())
	}
	if len(reports) != len(goals) {
		t.Fatalf("want %d reports, got %d", len(goals), len(reports))
	}
	for i, rep := range reports {
		if rep.GoalIndex != i {
			t.Errorf("report[%d].GoalIndex = %d, want %d (ordered by goal index)", i, rep.GoalIndex, i)
		}
		if rep.Status != swarm.StatusOK {
			t.Errorf("report[%d].Status = %q, want ok (mock workers always succeed)", i, rep.Status)
		}
		if rep.Summary != demoWorkerAnswer {
			t.Errorf("report[%d].Summary = %q, want %q", i, rep.Summary, demoWorkerAnswer)
		}
	}
}
