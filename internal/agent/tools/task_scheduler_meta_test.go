package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// task_scheduler_meta_test.go pins the half of the schedule-changed signal that lives in the
// tool: an action that MUTATED the schedule says so on its result Meta, and a read-only one
// says nothing.
//
// The agent lifts that key onto Actions.SchedulerDelta and the AG-UI translator fans it out as
// the aura.scheduler CUSTOM frame a governance board keys on. Without it the board is a
// snapshot taken at mount: measured 2026-09-07, a reminder created in chat was in
// scheduler_tasks at 07:31:16, fired, delivered on Telegram, and never appeared on the board
// sitting beside the conversation that created it.

// schedulerMeta reads the scheduler descriptor off a tool result, or nil when absent.
func schedulerMeta(res ToolResult) map[string]any {
	if res.Meta == nil {
		return nil
	}
	value, ok := (*res.Meta)["scheduler"]
	if !ok {
		return nil
	}
	delta, _ := value.(map[string]any)
	return delta
}

// TestTaskMutatingActionsStampTheSchedulerMeta covers all three verbs that change what the
// board shows. Listing them one by one is deliberate: a fourth mutating action added later
// without its stamp is a board that silently goes stale again.
func TestTaskMutatingActionsStampTheSchedulerMeta(t *testing.T) {
	cases := []struct {
		action string
		args   string
	}{
		{"schedule", `{"action":"schedule","schedule_kind":"every","every_minutes":10,"kind":"reminder","payload":{"text":"standup"},"notify":"stdout"}`},
		{"cancel", `{"action":"cancel","task_id":"abc"}`},
		{"run_now", `{"action":"run_now","task_id":"abc"}`},
	}
	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			tool := &TaskTool{Store: &fakeTaskStore{createID: "abc"}}

			res, err := tool.Execute(context.Background(), json.RawMessage(tc.args))
			if err != nil {
				t.Fatalf("%s: %v", tc.action, err)
			}
			delta := schedulerMeta(res)
			if delta == nil {
				t.Fatalf("%s changed the schedule and left no scheduler Meta: a board cannot learn it happened", tc.action)
			}
			if got := delta["action"]; got != tc.action {
				t.Fatalf("scheduler Meta action = %v, want %q", got, tc.action)
			}
		})
	}
}

// TestTaskListLeavesNoSchedulerMeta keeps the signal meaningful: a read must not tell every
// open board to refetch, or the frame stops meaning "something changed".
func TestTaskListLeavesNoSchedulerMeta(t *testing.T) {
	tool := &TaskTool{Store: &fakeTaskStore{}}

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if delta := schedulerMeta(res); delta != nil {
		t.Fatalf("a read-only action stamped %v, want no scheduler Meta", delta)
	}
}

// TestTaskFailedMutationLeavesNoSchedulerMeta: a verb that errored changed nothing, so telling
// the board to reread would be a refetch that finds exactly what it already had.
func TestTaskFailedMutationLeavesNoSchedulerMeta(t *testing.T) {
	tool := &TaskTool{Store: &fakeTaskStore{cancelErr: errors.New("cancel refused")}}

	res, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"cancel","task_id":"abc"}`))
	if err == nil {
		t.Fatal("the fake store was told to fail the cancel")
	}
	if delta := schedulerMeta(res); delta != nil {
		t.Fatalf("a failed mutation stamped %v, want no scheduler Meta", delta)
	}
}
