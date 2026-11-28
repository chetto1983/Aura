package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// The history page: a few runs by default, never more than the model can read at once, and each
// summary bounded so one verbose job cannot crowd out the others.
const (
	defaultHistoryRuns = 5
	maxHistoryRuns     = 20
	historySummaryRune = 1500
)

// TaskRun is one past run of a scheduled task, as the history action shows it.
type TaskRun struct {
	StartedAt time.Time
	Status    string
	Summary   string
	LastError string
}

// actionHistory reads what a task's runs produced. Measured on the lab VM on 2026-11-28: after a
// run_now the model could see only that the task had gone back to its cadence, so it slept and
// polled for a report that was already on the run ledger. It mutates nothing, so it skips the
// mutation guard requireTaskID applies.
func (t *TaskTool) actionHistory(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a taskArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("task history args: %w", err)
	}
	id := strings.TrimSpace(a.TaskID)
	if id == "" {
		return ToolResult{}, fmt.Errorf("task history: task_id is required")
	}
	runs, err := t.Store.ListTaskRuns(ctx, id, historyLimit(a.Limit))
	if err != nil {
		return ToolResult{}, fmt.Errorf("task history: %w", err)
	}
	s := renderTaskRuns(id, runs)
	return ToolResult{Preview: s, Bytes: len(s)}, nil
}

func historyLimit(asked int) int {
	switch {
	case asked <= 0:
		return defaultHistoryRuns
	case asked > maxHistoryRuns:
		return maxHistoryRuns
	default:
		return asked
	}
}

// renderTaskRuns keeps the store's newest-first order.
func renderTaskRuns(id string, runs []TaskRun) string {
	if len(runs) == 0 {
		return "no runs for task " + id
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d run(s) of task %s, newest first:\n", len(runs), id)
	for _, r := range runs {
		fmt.Fprintf(&b, "\n- %s %s\n", r.StartedAt.UTC().Format(time.RFC3339), r.Status)
		if r.Summary != "" {
			fmt.Fprintf(&b, "  summary: %s\n", clipRunes(r.Summary, historySummaryRune))
		}
		if r.LastError != "" {
			fmt.Fprintf(&b, "  error: %s\n", r.LastError)
		}
	}
	return b.String()
}
