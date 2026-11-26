package main

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/runner"
)

const taskCancelHookDigest = "5555555555555555555555555555555555555555555555555555555555555555"

func taskCancelPending(t *testing.T, question, contextType string) askuser.Pending {
	t.Helper()
	return askuser.Pending{
		ConversationID: "conv-task",
		Kind:           tools.KindApproval,
		Question:       question,
		ResumeContext:  rawJSON(t, map[string]string{"type": contextType, "approval_sha256": taskCancelHookDigest}),
	}
}

// An accept of the exact question records one approval; a decline, a cancel or a rewritten
// question records nothing, and an expiry discards the challenge.
func TestTaskCancelResumeHookRecordsOnlyTheAcceptOfItsQuestion(t *testing.T) {
	task := &tools.TaskTool{CancelApprovals: &tools.TaskCancelApprovals{}}
	hook := newTaskCancelResumeHook(task)
	const question = "Approve cancelling a task not scheduled in this chat?"
	task.CancelApprovals.CreateChallenge("conv-task", taskCancelHookDigest, question, tools.TaskCancelAction{TaskID: "t-1"})
	pending := taskCancelPending(t, question, tools.TaskCancelApprovalContext)

	for _, action := range []string{askuser.ActionDecline, askuser.ActionCancel} {
		if err := hook(t.Context(), pending, runner.ResponseInput{Action: action}); err != nil {
			t.Fatalf("%s: %v", action, err)
		}
	}
	if task.CancelApprovals.Consume("conv-task", taskCancelHookDigest) {
		t.Fatal("a decline or a cancel became an approval")
	}
	rewritten := taskCancelPending(t, "Approve cancelling nothing important?", tools.TaskCancelApprovalContext)
	if err := hook(t.Context(), rewritten, runner.ResponseInput{Action: askuser.ActionAccept}); err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("accept of a rewritten question = %v, want a mismatch", err)
	}

	if err := hook(t.Context(), pending, runner.ResponseInput{Action: askuser.ActionAccept}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if !task.CancelApprovals.Consume("conv-task", taskCancelHookDigest) {
		t.Fatal("the accept was not recorded")
	}

	task.CancelApprovals.CreateChallenge("conv-task", taskCancelHookDigest, question, tools.TaskCancelAction{TaskID: "t-1"})
	if err := hook(t.Context(), pending, runner.ResponseInput{Action: askuser.ActionExpired}); err != nil {
		t.Fatalf("expiry: %v", err)
	}
	if _, ok := task.CancelApprovals.PendingChallenge("conv-task", taskCancelHookDigest); ok {
		t.Fatal("an expired challenge stayed pending")
	}
	if newTaskCancelResumeHook(nil) != nil || newTaskCancelResumeHook(&tools.TaskTool{}) != nil {
		t.Fatal("a hook without a store must be nil, as chainResumeHooks filters")
	}
}

// The stores are separate instances, so neither the plugin hook nor the shell hook can approve
// a cancel, and the cancel hook cannot approve a plugin challenge.
func TestTaskCancelAndPluginHooksCannotApproveEachOther(t *testing.T) {
	task := &tools.TaskTool{CancelApprovals: &tools.TaskCancelApprovals{}}
	pack := &tools.PackTool{Approvals: &tools.PluginApprovals{}, Backend: adminOnlyBackend{admin: ""}}
	const question = "Approve?"
	task.CancelApprovals.CreateChallenge("conv-task", taskCancelHookDigest, question, tools.TaskCancelAction{TaskID: "t-1"})
	pack.Approvals.CreateChallenge("conv-task", taskCancelHookDigest, question, tools.PluginAction{Action: "install"})
	accept := runner.ResponseInput{Action: askuser.ActionAccept}

	chain := chainResumeHooks(newShellResumeHook(tools.NewShellApprovals()), newPluginResumeHook(pack))
	if err := chain(t.Context(), taskCancelPending(t, question, tools.TaskCancelApprovalContext), accept); err != nil {
		t.Fatalf("the other hooks on a cancel context: %v", err)
	}
	if task.CancelApprovals.Consume("conv-task", taskCancelHookDigest) {
		t.Fatal("a cancel was approved by another hook")
	}
	if err := newTaskCancelResumeHook(task)(t.Context(), taskCancelPending(t, question, tools.PluginApprovalContext), accept); err != nil {
		t.Fatalf("the cancel hook on a plugin context: %v", err)
	}
	if pack.Approvals.Consume("conv-task", taskCancelHookDigest) {
		t.Fatal("the cancel hook approved a plugin challenge")
	}
}

// The registry keeps the task tool it registered, with its approvals store, for the resume hook.
func TestTaskToolHandleCarriesItsCancelApprovals(t *testing.T) {
	reg, handles := buildBaseRegistryWithHandles(config.LoadDB(), nil, nil)
	registered, ok := reg.Get("task")
	if !ok || handles.Task == nil || registered != handles.Task || handles.Task.CancelApprovals == nil {
		t.Fatalf("task handle = %+v, registered %v", handles.Task, registered)
	}
	if newTaskCancelResumeHook(handles.Task) == nil {
		t.Fatal("the wired task tool must yield a resume hook")
	}
}
