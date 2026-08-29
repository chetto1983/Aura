package display

import (
	"reflect"
	"testing"
)

// TestNormalizeSwarm (SWARM-01): a []ChildReport becomes a swarm_report payload
// carrying every ChildReport field for in-place row expansion (D-08).
func TestNormalizeSwarm(t *testing.T) {
	reports := []ChildReport{
		{GoalIndex: 0, ChildID: "w1", Status: StatusOK, Summary: "found 3 sources"},
		{GoalIndex: 1, ChildID: "w2", Status: StatusFailed, Error: "timeout"},
		{GoalIndex: 2, ChildID: "w3", Status: StatusNeedsUserInput, Question: "which region?", Options: []string{"EU", "US"}, ToolCallID: "call-ask-1"},
	}
	p, ok := normalizeSwarm("call-swarm-1", reports)
	if !ok || p.Type != KindSwarmReport {
		t.Fatalf("normalizeSwarm = (%v, %v), want swarm_report/true", p.Type, ok)
	}
	if len(p.Swarm) != 3 {
		t.Fatalf("Swarm = %d, want 3", len(p.Swarm))
	}
	// Every field survives.
	if p.Swarm[0].Summary != "found 3 sources" || p.Swarm[1].Error != "timeout" {
		t.Fatalf("summary/error lost: %+v", p.Swarm)
	}
	nui := p.Swarm[2]
	if nui.Question != "which region?" || len(nui.Options) != 2 || nui.ToolCallID != "call-ask-1" {
		t.Fatalf("needs_user_input fields lost: %+v", nui)
	}
}

// TestNormalizeSwarmCopiesInput: the normalizer returns a copy, so mutating the
// returned slice cannot corrupt the caller's reports (and vice versa).
func TestNormalizeSwarmCopiesInput(t *testing.T) {
	reports := []ChildReport{{GoalIndex: 0, ChildID: "w1", Status: StatusOK}}
	p, _ := normalizeSwarm("c", reports)
	p.Swarm[0].Status = StatusFailed
	if reports[0].Status != StatusOK {
		t.Fatalf("normalizeSwarm aliased the caller's slice")
	}
}

func TestChildReportBackgroundFieldsKeepWireTags(t *testing.T) {
	typ := reflect.TypeFor[ChildReport]()
	for name, wantTag := range map[string]string{"Goal": "goal,omitempty", "Attempts": "attempts,omitempty"} {
		field, ok := typ.FieldByName(name)
		if !ok {
			t.Fatalf("ChildReport missing %s", name)
		}
		if got := field.Tag.Get("json"); got != wantTag {
			t.Fatalf("ChildReport.%s json tag = %q, want %q", name, got, wantTag)
		}
	}
}
