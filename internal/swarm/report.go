// Package swarm is the ephemeral per-call coordinator for CAP-03: it fans a set
// of goals out as LlmAgent workers, runs them in budget-bounded leak-safe waves,
// isolates per-child failures (a failed child becomes a report entry, siblings
// keep running — D-02), and collects an ordered []ChildReport. The engine is
// decoupled from the swarm_spawn tool (09-05 wires them) so it stays independently
// testable with agenttest.FakeClient.
package swarm

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/chetto1983/aura/internal/agent"
)

// Status values a ChildReport can carry (D-15). They are model-readable: the
// parent LLM reads these off the collected report to decide aggregation / proxying.
const (
	StatusOK             = "ok"
	StatusFailed         = "failed"
	StatusNeedsUserInput = "needs_user_input"
)

// ChildReport is one worker's slot in the swarm result, ordered by goal index
// (D-15). ChildID is the deterministic flat worker id ("w1".."wN", D-16) — it
// contains no path separator so it is safe as both the worker SessionID suffix
// and the transcript filename (Pitfall 4). The needs_user_input fields
// (Question/Options/ToolCallID) carry the worker's ask_user pause payload (D-04)
// so the parent can proxy it via D-05.
type ChildReport struct {
	GoalIndex  int      `json:"goal_index"`
	ChildID    string   `json:"child_id"`
	Status     string   `json:"status"`
	Summary    string   `json:"summary,omitempty"`
	Error      string   `json:"error,omitempty"`
	Question   string   `json:"question,omitempty"`
	Options    []string `json:"options,omitempty"`
	ToolCallID string   `json:"tool_call_id,omitempty"`
}

// dumpTranscript appends ev (via Event.MarshalJSON, one JSON object per line) to
// <runDir>/<convID>/swarm/<childID>.jsonl. This is a SEPARATE direct write, NOT
// the tool-spillover sidecar (D-18, Pitfall 4): childID is the flat worker id (no
// slash), so the path is controlled and never model-traversable. It is best-effort
// — a write error is logged and swallowed (return nil) so a dump failure never
// fails the swarm.
func dumpTranscript(runDir, convID, childID string, ev agent.Event) error {
	dir := filepath.Join(runDir, convID, "swarm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		slog.Warn("swarm transcript mkdir failed", "child", childID, "err", err)
		return nil
	}
	line, err := ev.MarshalJSON()
	if err != nil {
		slog.Warn("swarm transcript marshal failed", "child", childID, "err", err)
		return nil
	}
	path := filepath.Join(dir, childID+".jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		slog.Warn("swarm transcript open failed", "child", childID, "err", err)
		return nil
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(line, '\n')); err != nil {
		slog.Warn("swarm transcript write failed", "child", childID, "err", err)
		return nil
	}
	return nil
}

// marshalReports serializes the ordered reports to compact JSON for the swarm
// ToolResult body (the ONLY spillover path is the caller's tools.NewResult — D-15).
func marshalReports(reports []ChildReport) (string, error) {
	b, err := json.Marshal(reports)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
