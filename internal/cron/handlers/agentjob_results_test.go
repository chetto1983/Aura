package handlers

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
)

// The briefing's acceptance needs what the model read, whole: the ledger keeps 2 KiB
// (docs/superpowers/specs/2026-10-09-daily-briefing-design.md, "Keeping a run's reads").
func TestAJobAskedToKeepsItsToolResults(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	h := AgentJobHandler{Deps: AgentDeps{Client: agenttest.NewFakeClient(loopTurns(2)...), Registry: jobRegistry(), RunDir: runDir}}
	payload := []byte(`{"goal":"Write my daily briefing","keep_results":true}`)
	if _, err := h.Run(t.Context(), Job{Payload: payload, StepBudget: 6, RunID: "run-kept"}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(runDir, "tmp", "agent_job-run-kept", "results.jsonl")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("no kept results: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("results mode = %v, want 0600: they hold what the model read", info.Mode().Perm())
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var kept []keptResult
	for scan := bufio.NewScanner(f); scan.Scan(); {
		var r keptResult
		if err := json.Unmarshal(scan.Bytes(), &r); err != nil {
			t.Fatalf("line %q: %v", scan.Text(), err)
		}
		kept = append(kept, r)
	}
	if len(kept) < 2 {
		t.Fatalf("kept %d results, want the two loop calls at least: %+v", len(kept), kept)
	}
	first := kept[0]
	if first.Tool != "loop" || first.Arguments != `{"n":0}` || first.Status != "ok" || first.Result != "ok" || first.ToolCallID != "c0" {
		t.Errorf("first kept result = %+v, want loop c0 with its arguments and result", first)
	}
}

func TestAJobNotAskedKeepsNothing(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	h := AgentJobHandler{Deps: AgentDeps{Client: agenttest.NewFakeClient(loopTurns(1)...), Registry: jobRegistry(), RunDir: runDir}}
	if _, err := h.Run(t.Context(), Job{Payload: []byte(`{"goal":"x"}`), StepBudget: 4, RunID: "run-plain"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "tmp")); !os.IsNotExist(err) {
		t.Errorf("a job without keep_results wrote under tmp/: %v", err)
	}
}

// An acceptance day without its capture would be silently unscored, so a capture the job
// cannot open fails the run before the model is called.
func TestAJobThatCannotKeepItsResultsFails(t *testing.T) {
	t.Parallel()
	client := agenttest.NewFakeClient(loopTurns(1)...)
	h := AgentJobHandler{Deps: AgentDeps{Client: client, Registry: jobRegistry()}}
	if _, err := h.Run(t.Context(), Job{Payload: []byte(`{"goal":"x","keep_results":true}`), StepBudget: 4, RunID: "run-nodir"}); err == nil {
		t.Fatal("keep_results with no run dir ran anyway")
	}
	if n := len(client.RecordedRequests()); n != 0 {
		t.Errorf("the model was called %d times before the capture failed", n)
	}
}

func endEvent(tool string) *agent.Event {
	return &agent.Event{Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{
		Event: agent.ToolInvocationEnd, ToolCallID: "c", ToolName: tool, Status: "ok", ResultPreview: "whole",
	}}}
}

func TestAResultLogThatCannotWriteSaysSo(t *testing.T) {
	t.Parallel()
	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openResultLog(notADir, "r"); err == nil {
		t.Error("a run dir that is a file opened a result log")
	}

	occupied := t.TempDir()
	if err := os.MkdirAll(filepath.Join(occupied, "tmp", "agent_job-r", "results.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := openResultLog(occupied, "r"); err == nil {
		t.Error("a results path that is a directory opened a result log")
	}

	closed, err := openResultLog(t.TempDir(), "r")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := closed.record(endEvent("board")); err == nil {
		t.Error("a write to a closed result log reported no error")
	}
}

func TestAResultLogKeepsOnlyFinishedCalls(t *testing.T) {
	t.Parallel()
	var none *resultLog
	if err := none.record(endEvent("board")); err != nil || none.Close() != nil {
		t.Error("a job with no result log must keep nothing and fail nothing")
	}

	dir := t.TempDir()
	l, err := openResultLog(dir, "r")
	if err != nil {
		t.Fatal(err)
	}
	start := &agent.Event{Actions: agent.Actions{ToolInvocation: &agent.ToolInvocation{Event: agent.ToolInvocationStart, ToolName: "board"}}}
	for _, ev := range []*agent.Event{start, {}, endEvent("board")} {
		if err := l.record(ev); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "tmp", "agent_job-r", "results.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var kept keptResult
	if err := json.Unmarshal(raw, &kept); err != nil || kept.Tool != "board" || kept.Result != "whole" {
		t.Errorf("kept %q, want exactly the one finished call", raw)
	}
}
