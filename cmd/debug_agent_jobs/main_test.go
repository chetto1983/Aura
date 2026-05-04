package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/aura/aura/internal/scheduler"
)

func TestRunProvesRunSkipMutateRerun(t *testing.T) {
	rep, cleanup, err := run(t.Context(), options{Timeout: 30 * time.Second})
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rep.OK {
		t.Fatalf("report did not pass: %+v", rep)
	}
	if len(rep.Runs) != 3 {
		t.Fatalf("runs = %d, want 3", len(rep.Runs))
	}
	first, second, third := rep.Runs[0], rep.Runs[1], rep.Runs[2]
	if first.Skipped || first.LLMCalls == 0 || first.ToolCalls == 0 {
		t.Fatalf("first run should execute through LLM/tool loop: %+v", first)
	}
	if !second.Skipped || second.LLMCalls != 0 || second.ToolCalls != 0 {
		t.Fatalf("second run should skip before LLM/tool calls: %+v", second)
	}
	if third.Skipped || !third.WakeSignatureChanged || third.WakeSignature == first.WakeSignature {
		t.Fatalf("third run should rerun after mutation with changed signature: first=%+v third=%+v", first, third)
	}
}

func TestRunPersistsMetricsAfterFinalRun(t *testing.T) {
	rep, cleanup, err := run(t.Context(), options{Keep: true, Timeout: 30 * time.Second})
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !rep.OK {
		t.Fatalf("report did not pass: %+v", rep)
	}
	if rep.Runs[2].TokensTotal == 0 {
		t.Fatalf("final run tokens not reported: %+v", rep.Runs[2])
	}
	store, err := scheduler.OpenStore(rep.DBPath)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()
	task, err := store.GetByName(t.Context(), "phase19-agent-job-e2e")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if task.LastOutput == "" || task.LastMetricsJSON == "" || task.WakeSignature == "" {
		t.Fatalf("persisted task missing result fields: %+v", task)
	}
	var metrics map[string]any
	if err := json.Unmarshal([]byte(task.LastMetricsJSON), &metrics); err != nil {
		t.Fatalf("metrics JSON: %v", err)
	}
	if metrics["skipped"] != false || metrics["llm_calls"] == float64(0) {
		t.Fatalf("final persisted metrics = %+v", metrics)
	}
	var roundTrip report
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if !roundTrip.OK || len(roundTrip.Runs) != 3 {
		t.Fatalf("round-trip report = %+v", roundTrip)
	}
}
