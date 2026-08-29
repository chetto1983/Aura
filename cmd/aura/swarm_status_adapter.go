// swarm_status_adapter.go wires SWARM-10's missing leg (51-11 Task 4,
// 51-UX-ENVELOPE-RESEARCH.md §G3) into the composition root: the ONE place that
// joins the durable job row (internal/documents) with the transcript tail
// (internal/swarm.ReadTranscript) into tools.SwarmWorkerStatus, since
// internal/agent/tools must import neither. Follows swarmTranscriptAdapter's own
// pattern (serve_agui.go): a thin struct closing over the daemon's already-
// resolved store/RunDir.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/swarm"
)

const defaultSwarmStatusJobListLimit = 8

// swarmStatusAdapter satisfies the tools package's unexported swarmStatusReader
// seam structurally: that interface cannot be named from this package (it is
// unexported), so there is no separate `var _ tools.swarmStatusReader = ...`
// assertion to write -- the struct literal field assignment at this file's own
// registration call site (cmd/aura/main.go) IS the compile-time check, the same
// implicit shape swarmRunner/RunnerAdapter (swarm_spawn.go / runner_adapter.go)
// already relies on.
type swarmStatusAdapter struct {
	store   *documents.PostgresIngestionJobStore
	runDir  string
	maxJobs int
}

// WorkerStatus uses the ambient identity as both the SQL predicate and RLS carrier.
func (a swarmStatusAdapter) WorkerStatus(ctx context.Context, conversationID, childID string, tailEvents int) ([]tools.SwarmWorkerStatus, error) {
	identityID := identityctx.IdentityID(ctx)
	if identityID == "" {
		return nil, fmt.Errorf("swarm status identity context is required")
	}
	if a.store == nil {
		return nil, fmt.Errorf("swarm status store is not configured")
	}

	var rows []documents.DelegationJobRow
	if childID != "" {
		row, found, err := a.store.FindDelegationJob(ctx, identityID, conversationID, childID)
		if err != nil {
			return nil, err
		}
		if !found {
			return []tools.SwarmWorkerStatus{}, nil
		}
		rows = []documents.DelegationJobRow{row}
	} else {
		limit := a.maxJobs
		if limit <= 0 {
			limit = defaultSwarmStatusJobListLimit
		}
		var err error
		rows, err = a.store.ListDelegationJobs(ctx, identityID, conversationID, limit)
		if err != nil {
			return nil, err
		}
	}

	out := make([]tools.SwarmWorkerStatus, 0, len(rows))
	for _, row := range rows {
		status, err := a.projectRow(ctx, conversationID, row, tailEvents)
		if err != nil {
			return nil, err
		}
		out = append(out, status)
	}
	return out, nil
}

// swarmStatusTerminalJobStatuses mirrors UpdateIngestionJobStatus's own
// terminal-status CASE list (internal/db/queries/ingestion_jobs.sql):
// queued/running/awaiting_input are NOT terminal, so elapsed keeps ticking
// against "now" for those; the other four freeze it at completed_at.
var swarmStatusTerminalJobStatuses = map[string]bool{
	"succeeded": true, "failed": true, "dead_letter": true, "canceled": true,
}

// projectRow treats an absent transcript as an empty tail, while surfacing read
// or decode failures so corrupted worker evidence is never reported as silence.
func (a swarmStatusAdapter) projectRow(ctx context.Context, conversationID string, row documents.DelegationJobRow, tailEvents int) (tools.SwarmWorkerStatus, error) {
	status := tools.SwarmWorkerStatus{
		ChildID: row.ChildID, Goal: row.Goal, Status: row.Status,
		Attempt: row.AttemptCount, MaxAttempts: row.MaxAttempts,
	}
	end := time.Now().UTC()
	if swarmStatusTerminalJobStatuses[row.Status] && !row.CompletedAt.IsZero() {
		end = row.CompletedAt
	}
	status.ElapsedSec = tools.SwarmWorkerElapsedSeconds(row.CreatedAt, end)

	raw, _, err := swarm.ReadTranscript(ctx, a.runDir, conversationID, row.ChildID, 0)
	if err != nil {
		return tools.SwarmWorkerStatus{}, fmt.Errorf("read transcript for child %q: %w", row.ChildID, err)
	}
	if len(raw) == 0 {
		return status, nil
	}
	lines := swarmStatusCompleteLines(raw)
	if len(lines) > tailEvents {
		lines = lines[len(lines)-tailEvents:]
	}
	events := make([]tools.SwarmWorkerEvent, 0, len(lines))
	for _, line := range lines {
		var ev agent.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			return tools.SwarmWorkerStatus{}, fmt.Errorf("decode transcript event for child %q: %w", row.ChildID, err)
		}
		kind, detail := swarmStatusEventKindDetail(ev)
		events = append(events, tools.SwarmWorkerEvent{
			At:     ev.Timestamp.UTC().Format(time.RFC3339Nano),
			Kind:   kind,
			Detail: tools.CapSwarmStatusDetail(detail),
		})
	}
	status.Tail = events
	if len(events) > 0 {
		status.LastEventAt = events[len(events)-1].At
	}
	return status, nil
}

// swarmStatusCompleteLines splits ReadTranscript's own complete-line bytes
// (already newline-terminated, T-51-28/ReadTranscript's own contract) on '\n'
// into individual JSONL lines, dropping the trailing empty element
// bytes.Split leaves after the final separator.
func swarmStatusCompleteLines(raw []byte) [][]byte {
	trimmed := bytes.TrimRight(raw, "\n")
	if len(trimmed) == 0 {
		return nil
	}
	return bytes.Split(trimmed, []byte("\n"))
}

// swarmStatusEventKindDetail derives Kind from which of the event's
// Actions/LLMResponse fields is populated (this task's own behavior spec: a
// tool invocation, a tool result, a pause, an error, or assistant text) and
// picks the most informative string for Detail. A bare marker event carrying
// only StateDelta (runChild's own terminal marker, swarm.go) falls through to
// the "event" default, summarized from its own swarm_child_status key when
// present.
func swarmStatusEventKindDetail(ev agent.Event) (kind, detail string) {
	switch {
	case ev.Actions.AwaitingInput != nil:
		return "pause", ev.Actions.AwaitingInput.Question
	case ev.Actions.ToolInvocation != nil && ev.Actions.ToolInvocation.Event == agent.ToolInvocationStart:
		return "tool_call", ev.Actions.ToolInvocation.ToolName
	case ev.Actions.ToolInvocation != nil && ev.Actions.ToolInvocation.Status == "error":
		return "error", firstNonEmptySwarmStatus(ev.Actions.ToolInvocation.Error, ev.Actions.ToolInvocation.ToolName)
	case ev.Actions.ToolInvocation != nil:
		return "tool_result", firstNonEmptySwarmStatus(ev.Actions.ToolInvocation.ResultPreview, ev.Actions.ToolInvocation.ToolName)
	case ev.LLMResponse != nil && ev.LLMResponse.Content != "":
		return "assistant", ev.LLMResponse.Content
	default:
		return "event", swarmStatusStateDeltaSummary(ev.Actions.StateDelta)
	}
}

func firstNonEmptySwarmStatus(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// swarmStatusStateDeltaSummary names runChild's own terminal marker
// (swarm_child_status/swarm_child_id/swarm_child_duration_sec, swarm.go) by its
// status when present; any other marker-only event falls back to its raw JSON,
// bounded by CapSwarmStatusDetail at the call site.
func swarmStatusStateDeltaSummary(delta map[string]any) string {
	if len(delta) == 0 {
		return ""
	}
	if status, ok := delta["swarm_child_status"].(string); ok {
		return "worker finished: " + status
	}
	b, err := json.Marshal(delta)
	if err != nil {
		return ""
	}
	return string(b)
}
