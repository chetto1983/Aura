package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/chetto1983/aura/internal/agent"
)

// keptResult is one finished tool call of a job asked to keep its results, whole: the ledger
// keeps 2 KiB of a result, and the daily briefing's baseline must read what the model read
// (docs/superpowers/specs/2026-10-09-daily-briefing-design.md, "Keeping a run's reads").
type keptResult struct {
	ToolCallID        string    `json:"tool_call_id"`
	Tool              string    `json:"tool"`
	Arguments         string    `json:"arguments"`
	Status            string    `json:"status"`
	Error             string    `json:"error,omitempty"`
	Result            string    `json:"result"`
	ResultSidecarPath string    `json:"result_sidecar_path,omitempty"`
	EndedAt           time.Time `json:"ended_at"`
}

// resultLog appends a job's kept results as JSON lines under the run dir's tmp/, the class the
// daily retention_sweep deletes after AURA_RETENTION_TEMP_HOURS. A nil log keeps nothing.
type resultLog struct {
	file *os.File
	enc  *json.Encoder
}

func openResultLog(runDir, runID string) (*resultLog, error) {
	if runDir == "" {
		return nil, errors.New("agent_job: keep_results needs a run dir")
	}
	dir := filepath.Join(runDir, "tmp", "agent_job-"+runID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("agent_job: keep_results: %w", err)
	}
	// #nosec G304 -- dir is <operator-configured run dir>/tmp/agent_job-<host-generated run id>.
	f, err := os.OpenFile(filepath.Join(dir, "results.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("agent_job: keep_results: %w", err)
	}
	return &resultLog{file: f, enc: json.NewEncoder(f)}, nil
}

// record keeps ev when it ends a tool call.
func (l *resultLog) record(ev *agent.Event) error {
	if l == nil {
		return nil
	}
	inv := ev.Actions.ToolInvocation
	if inv == nil || inv.Event != agent.ToolInvocationEnd {
		return nil
	}
	r := keptResult{
		ToolCallID: inv.ToolCallID, Tool: inv.ToolName, Arguments: inv.Arguments, Status: inv.Status,
		Error: inv.Error, Result: inv.ResultPreview, ResultSidecarPath: inv.ResultSidecarPath,
	}
	if inv.EndedAt != nil {
		r.EndedAt = *inv.EndedAt
	}
	if err := l.enc.Encode(r); err != nil {
		return fmt.Errorf("agent_job: keep_results: %w", err)
	}
	return nil
}

func (l *resultLog) Close() error {
	if l == nil {
		return nil
	}
	return l.file.Close()
}
