package tools

// task_pause.go holds the task tool's pause and resume actions (prd.md §15), split from
// task.go to keep it under the 600-LOC cap.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/cron"
)

func (t *TaskTool) actionPause(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	id, err := requireTaskID(raw, "pause")
	if err != nil {
		return ToolResult{}, err
	}
	if err := t.Store.PauseScheduledTask(ctx, id); err != nil {
		return ToolResult{}, fmt.Errorf("task pause: %w", err)
	}
	s := "paused task " + id + "; it will not fire until resumed"
	return ToolResult{Preview: s, Bytes: len(s)}, nil
}

func (t *TaskTool) actionResume(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	id, err := requireTaskID(raw, "resume")
	if err != nil {
		return ToolResult{}, err
	}
	next, err := t.Store.ResumeScheduledTask(ctx, id)
	if err != nil {
		return ToolResult{}, fmt.Errorf("task resume: %w", err)
	}
	s := "resumed task " + id + "; next run " + next.UTC().Format(time.RFC3339)
	return ToolResult{Preview: s, Bytes: len(s)}, nil
}

// pausedFlag tells the reader why a task stopped firing; a task the scheduler paused says
// how many failed runs did it, so the cause is looked at before it is resumed.
func pausedFlag(r ScheduledTask) string {
	if r.PausedReason == cron.PausedByFailures {
		return fmt.Sprintf(" [paused after %d failed runs]", r.ConsecutiveFailures)
	}
	return " [paused]"
}
