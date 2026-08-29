package swarm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"go.uber.org/goleak"
)

// swarm_childid_test.go is runChild's 51-11 coverage -- the stable ChildID
// fallback rule and the terminal transcript marker -- split out of
// swarm_test.go (CLAUDE.md's 600-LOC ceiling) once this plan added it. Shares
// swarm_test.go's own helpers (newRouter, outcome, testRunConfig) unchanged.

// TestRunChildChildID (51-11) pins rc.ChildID's fallback rule: a non-empty
// value wins (the background delegation path's stable, deterministic id),
// and an empty value falls back to runChild's own "w<idx+1>" -- byte-for-byte
// the SYNCHRONOUS swarm_spawn path's existing behaviour, unaffected by this
// plan.
func TestRunChildChildID(t *testing.T) {
	defer goleak.VerifyNone(t)

	t.Run("honours a configured ChildID", func(t *testing.T) {
		r := newRouter().route("alpha", outcome{kind: "ok", text: "A"})
		rc := testRunConfig(t, r, 25)
		rc.ChildID = "w1-custom9"
		budget := rc.ParentBudget.Child(1)

		report, _ := runChild(context.Background(), rc, budget, 0, "alpha task")
		if report.ChildID != "w1-custom9" {
			t.Fatalf("report.ChildID = %q, want the configured rc.ChildID", report.ChildID)
		}
		path := filepath.Join(rc.Cfg.RunDir, rc.ConvID, "swarm", "w1-custom9.jsonl")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("transcript for the configured child id missing at %s: %v", path, err)
		}
	})

	t.Run("falls back to the flat wN id when unset", func(t *testing.T) {
		r := newRouter().route("alpha", outcome{kind: "ok", text: "A"})
		rc := testRunConfig(t, r, 25) // rc.ChildID left at its zero value
		budget := rc.ParentBudget.Child(1)

		report, _ := runChild(context.Background(), rc, budget, 0, "alpha task")
		if report.ChildID != "w1" {
			t.Fatalf("report.ChildID = %q, want the flat w1 fallback", report.ChildID)
		}
	})
}

// TestRunChildWritesTerminalMarker (51-11) reads the transcript back through
// ReadTranscript -- the artifact 51-12a's worker-events route will actually
// consume -- and asserts its LAST line carries the cross-plan contract's
// three state-delta keys and the report's own final status. Asserted as a
// PROPERTY on the artifact, not as a call-count on dumpTranscript (an
// occurrence count would pass even if the marker landed in the wrong file).
func TestRunChildWritesTerminalMarker(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRouter().route("alpha", outcome{kind: "ok", text: "A"})
	rc := testRunConfig(t, r, 25)
	rc.ChildID = "w1-marker"
	budget := rc.ParentBudget.Child(1)

	report, _ := runChild(context.Background(), rc, budget, 0, "alpha task")

	last := lastTranscriptEvent(t, rc, "w1-marker")
	status, _ := last.Actions.StateDelta["swarm_child_status"].(string)
	childID, _ := last.Actions.StateDelta["swarm_child_id"].(string)
	if status != report.Status {
		t.Fatalf("marker swarm_child_status = %q, want the report's own status %q", status, report.Status)
	}
	if childID != "w1-marker" {
		t.Fatalf("marker swarm_child_id = %q, want %q", childID, "w1-marker")
	}
	if _, ok := last.Actions.StateDelta["swarm_child_duration_sec"]; !ok {
		t.Fatal("marker missing swarm_child_duration_sec")
	}
}

// TestRunChildWritesTerminalMarkerOnCancel (51-11): a lease loss or daemon
// shutdown cancels the ctx PASSED TO runChild (never the internal staleness
// timer's own derived cancel, which TestWorkerStalledReport already covers).
// The "block" outcome holds on <-ctx.Done() with NO event ever streamed, so
// the marker is the transcript's ONLY line -- proving the marker fires even
// when the loop's `for ev := range worker.Run(ic)` never yields a single event.
func TestRunChildWritesTerminalMarkerOnCancel(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRouter().route("alpha", outcome{kind: "block"})
	rc := testRunConfig(t, r, 25)
	rc.ChildID = "w1-cancelled"
	budget := rc.ParentBudget.Child(1)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan ChildReport, 1)
	go func() {
		report, _ := runChild(ctx, rc, budget, 0, "alpha task")
		done <- report
	}()
	// Give runChild a moment to reach the blocking Stream call before cancelling
	// its ctx out from under it.
	time.Sleep(20 * time.Millisecond)
	cancel()

	var report ChildReport
	select {
	case report = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runChild did not return after its context was cancelled")
	}
	if report.Status == "" {
		t.Fatal("cancelled runChild returned an empty status")
	}

	last := lastTranscriptEvent(t, rc, "w1-cancelled")
	status, _ := last.Actions.StateDelta["swarm_child_status"].(string)
	if status == "" {
		t.Fatal("marker on a cancelled worker carries an empty status -- the transcript ends in silence")
	}
}

// lastTranscriptEvent reads childID's transcript back through ReadTranscript
// and unmarshals its LAST complete line into an agent.Event.
func lastTranscriptEvent(t *testing.T, rc RunConfig, childID string) agent.Event {
	t.Helper()
	raw, _, err := ReadTranscript(context.Background(), rc.Cfg.RunDir, rc.ConvID, childID, 0)
	if err != nil {
		t.Fatalf("ReadTranscript: %v", err)
	}
	trimmed := strings.TrimRight(string(raw), "\n")
	if trimmed == "" {
		t.Fatal("transcript is empty, want at least the terminal marker line")
	}
	lines := strings.Split(trimmed, "\n")
	var last agent.Event
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatalf("last transcript line is not a valid Event: %v", err)
	}
	return last
}
