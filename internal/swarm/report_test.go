package swarm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/google/uuid"
)

// TestChildReport asserts the JSON contract (D-15): the always-present keys are
// goal_index/child_id/status, and the optional fields omit when empty.
func TestChildReport(t *testing.T) {
	full := ChildReport{
		GoalIndex:  2,
		ChildID:    "w3",
		Status:     StatusNeedsUserInput,
		Question:   "Which inbox?",
		Options:    []string{"work", "personal"},
		ToolCallID: "call-abc",
	}
	b, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"goal_index", "child_id", "status"} {
		if _, ok := m[k]; !ok {
			t.Errorf("ChildReport JSON missing required key %q: %s", k, b)
		}
	}
	if m["goal_index"].(float64) != 2 || m["child_id"] != "w3" || m["status"] != StatusNeedsUserInput {
		t.Errorf("ChildReport values not threaded: %s", b)
	}

	// Optional fields omit when empty.
	bare := ChildReport{GoalIndex: 0, ChildID: "w1", Status: StatusOK}
	bb, _ := json.Marshal(bare)
	for _, k := range []string{"summary", "error", "question", "options", "tool_call_id"} {
		if strings.Contains(string(bb), `"`+k+`"`) {
			t.Errorf("empty optional %q leaked onto the wire: %s", k, bb)
		}
	}
}

// TestDumpTranscriptPath asserts D-18 + Pitfall 4: the transcript lands at
// <runDir>/<convID>/swarm/<childID>.jsonl with a FLAT child id (no '/'), each line
// is a valid Event, and a forced write error is swallowed (returns nil) so a dump
// failure never fails the swarm.
func TestDumpTranscriptPath(t *testing.T) {
	tmp := t.TempDir()
	ev := agent.Event{
		RequestID: uuid.Must(uuid.NewV7()),
		Author:    "w1",
		Timestamp: time.Now(),
	}

	const childID = "w1"
	if strings.ContainsAny(childID, `/\`) {
		t.Fatalf("childID %q must be flat (no path separator)", childID)
	}

	if err := dumpTranscript(tmp, "conv1", childID, ev); err != nil {
		t.Fatalf("dumpTranscript returned error: %v", err)
	}
	// A second event must append (one JSON object per line).
	ev2 := ev
	ev2.RequestID = uuid.Must(uuid.NewV7())
	if err := dumpTranscript(tmp, "conv1", childID, ev2); err != nil {
		t.Fatalf("dumpTranscript (append) returned error: %v", err)
	}

	path := filepath.Join(tmp, "conv1", "swarm", childID+".jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("transcript not written at %s: %v", path, err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 transcript lines, got %d: %q", len(lines), raw)
	}
	for i, line := range lines {
		var decoded agent.Event
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			t.Errorf("line %d is not a valid Event: %v (%q)", i, err, line)
		}
	}

	// A forced write error (runDir is an existing FILE, so MkdirAll fails) is
	// swallowed: dumpTranscript returns nil and never fails the swarm.
	fileAsDir := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(fileAsDir, []byte("x"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if err := dumpTranscript(fileAsDir, "conv1", childID, ev); err != nil {
		t.Errorf("dumpTranscript over a file runDir must swallow the error, got %v", err)
	}
}
