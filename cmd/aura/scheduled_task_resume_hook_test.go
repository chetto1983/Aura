package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/runner"
)

// fakeScheduledApprover records the owner-scoped approve/cancel calls so the hook tests assert
// routing without a DB (mirrors the shell/gateway hook fakes).
type fakeScheduledApprover struct {
	taskID       string
	convID       string
	calls        int
	cancelTaskID string
	cancelConvID string
	cancelCalls  int
	err          error
}

func (f *fakeScheduledApprover) ApproveScheduledTaskInConversation(_ context.Context, taskID, conversationID string) error {
	f.calls++
	f.taskID = taskID
	f.convID = conversationID
	return f.err
}

func (f *fakeScheduledApprover) CancelScheduledTaskInConversation(_ context.Context, taskID, conversationID string) error {
	f.cancelCalls++
	f.cancelTaskID = taskID
	f.cancelConvID = conversationID
	return f.err
}

// TestScheduledTaskResumeHookApprovesAccepted proves an accept on a
// scheduled_task_approval pause activates the task, keyed on the AUTHENTICATED
// pending.ConversationID (never the model-relayed resume_context) — the WR-02 binding.
func TestScheduledTaskResumeHookApprovesAccepted(t *testing.T) {
	store := &fakeScheduledApprover{}
	hook := newScheduledTaskResumeHook(store)

	err := hook(context.Background(), askuser.Pending{
		ConversationID: "conv-A",
		Kind:           tools.KindApproval,
		ResumeContext:  rawJSON(t, map[string]string{"type": "scheduled_task_approval", "task_id": "task-99"}),
	}, runner.ResponseInput{Action: askuser.ActionAccept})
	if err != nil {
		t.Fatalf("hook: %v", err)
	}
	if store.calls != 1 {
		t.Fatalf("approve calls = %d, want 1", store.calls)
	}
	if store.taskID != "task-99" {
		t.Errorf("approved task_id = %q, want task-99", store.taskID)
	}
	if store.convID != "conv-A" {
		t.Errorf("approve keyed on conversation %q, want the authenticated conv-A", store.convID)
	}
}

// TestScheduledTaskResumeHookCancelsDeclined proves a decline on a scheduled_task_approval
// pause CANCELS the task (owner-scoped on the authenticated conversation) so a declined task is
// dropped and stops re-nudging. Decline is the on-channel "No" (ActionCancel never reaches a
// ResumeHook — SubmitAnswer short-circuits it before applyResumeHook).
func TestScheduledTaskResumeHookCancelsDeclined(t *testing.T) {
	store := &fakeScheduledApprover{}
	hook := newScheduledTaskResumeHook(store)

	err := hook(context.Background(), askuser.Pending{
		ConversationID: "conv-A",
		Kind:           tools.KindApproval,
		ResumeContext:  rawJSON(t, map[string]string{"type": "scheduled_task_approval", "task_id": "task-99"}),
	}, runner.ResponseInput{Action: askuser.ActionDecline})
	if err != nil {
		t.Fatalf("hook: %v", err)
	}
	if store.cancelCalls != 1 || store.calls != 0 {
		t.Fatalf("decline => cancel=%d approve=%d, want cancel=1 approve=0", store.cancelCalls, store.calls)
	}
	if store.cancelTaskID != "task-99" || store.cancelConvID != "conv-A" {
		t.Errorf("cancel keyed on (%q,%q), want (task-99, conv-A)", store.cancelTaskID, store.cancelConvID)
	}
}

// TestScheduledTaskResumeHookIgnoresNonMatching proves the hook resolves NOTHING (fail-closed)
// on a raw ActionCancel, a non-approval kind, or an unrelated resume_context type — the task
// stays pending_approval.
func TestScheduledTaskResumeHookIgnoresNonMatching(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action string
		kind   string
		rcType string
	}{
		{"cancel", askuser.ActionCancel, tools.KindApproval, "scheduled_task_approval"},
		{"not approval kind", askuser.ActionAccept, tools.KindClarification, "scheduled_task_approval"},
		{"unrelated type", askuser.ActionAccept, tools.KindApproval, "skill_approval"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeScheduledApprover{}
			hook := newScheduledTaskResumeHook(store)
			err := hook(context.Background(), askuser.Pending{
				ConversationID: "conv-A",
				Kind:           tc.kind,
				ResumeContext:  rawJSON(t, map[string]string{"type": tc.rcType, "task_id": "task-99"}),
			}, runner.ResponseInput{Action: tc.action})
			if err != nil {
				t.Fatalf("hook: %v", err)
			}
			if store.calls != 0 || store.cancelCalls != 0 {
				t.Fatalf("%s must resolve nothing, got approve=%d cancel=%d", tc.name, store.calls, store.cancelCalls)
			}
		})
	}
}

// TestScheduledTaskResumeHookMissingTaskID proves an accept with the right type but no
// task_id is a hard error (the hook cannot activate an unkeyed task) and approves nothing.
func TestScheduledTaskResumeHookMissingTaskID(t *testing.T) {
	store := &fakeScheduledApprover{}
	hook := newScheduledTaskResumeHook(store)

	err := hook(context.Background(), askuser.Pending{
		ConversationID: "conv-A",
		Kind:           tools.KindApproval,
		ResumeContext:  rawJSON(t, map[string]string{"type": "scheduled_task_approval"}),
	}, runner.ResponseInput{Action: askuser.ActionAccept})
	if err == nil {
		t.Fatal("hook accepted a scheduled_task_approval with no task_id")
	}
	if store.calls != 0 {
		t.Fatalf("missing task_id must not approve, got %d calls", store.calls)
	}
}

// TestScheduledTaskResumeHookPropagatesApproveError proves an approver failure (e.g. a
// foreign task_id that matches no pending_approval row in this conversation) surfaces.
func TestScheduledTaskResumeHookPropagatesApproveError(t *testing.T) {
	store := &fakeScheduledApprover{err: errors.New("not awaiting approval")}
	hook := newScheduledTaskResumeHook(store)

	err := hook(context.Background(), askuser.Pending{
		ConversationID: "conv-A",
		Kind:           tools.KindApproval,
		ResumeContext:  rawJSON(t, map[string]string{"type": "scheduled_task_approval", "task_id": "foreign"}),
	}, runner.ResponseInput{Action: askuser.ActionAccept})
	if err == nil {
		t.Fatal("hook must propagate the approver error")
	}
	if store.calls != 1 {
		t.Fatalf("approve calls = %d, want 1", store.calls)
	}
}

// TestScheduledTaskResumeHookNilStore proves a nil store yields a nil hook so
// chainResumeHooks filters it out (mirrors newShellResumeHook/newGatewayResumeHook).
func TestScheduledTaskResumeHookNilStore(t *testing.T) {
	if newScheduledTaskResumeHook(nil) != nil {
		t.Fatal("newScheduledTaskResumeHook(nil) must return a nil hook")
	}
}
