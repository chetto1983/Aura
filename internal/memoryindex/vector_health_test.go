package memoryindex

import (
	"errors"
	"testing"
)

func TestVectorHealthTrackerTransitions(t *testing.T) {
	tracker := NewVectorHealthTracker(false, "")
	initial := tracker.Snapshot()
	if initial.Enabled || initial.Running {
		t.Fatalf("initial snapshot = %+v, want disabled and not running", initial)
	}

	tracker.SetEnabled(true, "aura_memory_v1_compact")
	tracker.Started()
	running := tracker.Snapshot()
	if !running.Enabled || !running.Running || running.Collection != "aura_memory_v1_compact" {
		t.Fatalf("running snapshot = %+v, want enabled running collection", running)
	}
	if running.LastStartedAt == nil {
		t.Fatalf("running snapshot = %+v, want LastStartedAt", running)
	}

	tracker.Succeeded(VectorReport{Collection: "aura_memory_v1_compact", DocsIndexed: 12, VectorSize: 1024})
	success := tracker.Snapshot()
	if !success.Enabled || success.Running || success.LastError != "" {
		t.Fatalf("success snapshot = %+v, want enabled not running no error", success)
	}
	if success.LastFinishedAt == nil || success.LastDocsIndexed != 12 || success.VectorSize != 1024 {
		t.Fatalf("success snapshot = %+v, want finished docs/vector size", success)
	}

	tracker.Started()
	tracker.Failed(errors.New("qdrant unavailable"))
	failed := tracker.Snapshot()
	if failed.Running || failed.LastError != "qdrant unavailable" || failed.LastFinishedAt == nil {
		t.Fatalf("failed snapshot = %+v, want stopped with error", failed)
	}
}
