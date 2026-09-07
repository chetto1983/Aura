package agent

import (
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// llm_agent_events_scheduler_test.go pins the Meta→SchedulerDelta lift: the task tool says on
// its result that it CHANGED the schedule, and toolResultEvent turns that into the delta the
// AG-UI translator fans out as aura.scheduler.
//
// It is the same named-lift seam the artifact and mcp_view descriptors ride, and it exists for
// a measured reason: without it a reminder created in chat reached Postgres, fired, delivered
// on Telegram, and never appeared on the governance board sitting beside the conversation that
// created it (2026-09-07, the operator's own deployment).

// schedulerRun builds a toolRunResult whose Meta carries the given scheduler descriptor (or
// none when delta is nil), mirroring what the task tool returns through runTool.
func schedulerRun(t *testing.T, delta map[string]any) toolRunResult {
	t.Helper()
	now := time.Now().UTC()
	run := toolRunResult{
		ToolCallID: "call-task",
		ToolName:   "task",
		Arguments:  `{"action":"create","when":"in 5 minutes"}`,
		StartedAt:  now,
		EndedAt:    now.Add(2 * time.Millisecond),
		Preview:    "1 task(s): 01a07ac7 kind=reminder",
	}
	res := tools.ToolResult{Preview: run.Preview, Bytes: len(run.Preview)}
	if delta != nil {
		meta := tools.ToolResultMeta{"scheduler": delta}
		res.Meta = &meta
	}
	run.Result = res
	return run
}

// TestToolResultEvent_LiftsSchedulerMeta is the lift itself.
func TestToolResultEvent_LiftsSchedulerMeta(t *testing.T) {
	a := newBareAgent(t, tools.NewRegistry())
	var span [8]byte

	ev := a.toolResultEvent(internalPauseIC(t), span, nil, schedulerRun(t, map[string]any{"action": "create"}))

	if ev.Actions.SchedulerDelta == nil {
		t.Fatal("toolResultEvent must lift the Meta scheduler key onto Actions.SchedulerDelta")
	}
	if got := ev.Actions.SchedulerDelta["action"]; got != "create" {
		t.Fatalf("SchedulerDelta.action = %v, want create", got)
	}
	if ev.Actions.ToolInvocation == nil {
		t.Fatal("the existing ToolInvocation stamp must remain — the scheduler lift is additive")
	}
}

// TestToolResultEvent_NoSchedulerLeavesNil keeps the branch additive: a read-only task action,
// or any other tool, must produce exactly the event it produced before.
func TestToolResultEvent_NoSchedulerLeavesNil(t *testing.T) {
	a := newBareAgent(t, tools.NewRegistry())
	var span [8]byte

	ev := a.toolResultEvent(internalPauseIC(t), span, nil, schedulerRun(t, nil))
	if ev.Actions.SchedulerDelta != nil {
		t.Fatalf("a run without the scheduler key must leave SchedulerDelta nil, got %v", ev.Actions.SchedulerDelta)
	}

	run := schedulerRun(t, nil)
	meta := tools.ToolResultMeta{"artifact": map[string]any{"path": "/x", "filename": "x"}}
	run.Result.Meta = &meta
	other := a.toolResultEvent(internalPauseIC(t), span, nil, run)
	if other.Actions.SchedulerDelta != nil {
		t.Fatalf("a different Meta key must leave SchedulerDelta nil, got %v", other.Actions.SchedulerDelta)
	}
}
