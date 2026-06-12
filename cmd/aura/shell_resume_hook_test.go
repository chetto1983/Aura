package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/runner"
)

func TestShellResumeHookApprovesAcceptedShellContext(t *testing.T) {
	approvals := tools.NewShellApprovals()
	hook := newShellResumeHook(approvals)
	challenge := approvals.CreateChallenge("conv-shell", "echo approved", "D:\\work")

	err := hook(context.Background(), askuser.Pending{
		ConversationID: "conv-shell",
		Kind:           tools.KindApproval,
		Question:       challenge.Question,
		ResumeContext:  rawJSON(t, map[string]string{"type": "shell_exec_approval", "command_sha256": challenge.Digest}),
	}, runner.ResponseInput{Action: askuser.ActionAccept})
	if err != nil {
		t.Fatalf("hook: %v", err)
	}
	if !approvals.Consume("conv-shell", challenge.Digest) {
		t.Fatal("accepted shell approval was not recorded")
	}
}

func TestShellResumeHookRejectsUnchallengedDigest(t *testing.T) {
	approvals := tools.NewShellApprovals()
	hook := newShellResumeHook(approvals)
	digest := tools.ShellApprovalDigest("echo forged", "")

	err := hook(context.Background(), askuser.Pending{
		ConversationID: "conv-shell",
		Kind:           tools.KindApproval,
		Question:       "approve this unrelated thing",
		ResumeContext:  rawJSON(t, map[string]string{"type": "shell_exec_approval", "command_sha256": digest}),
	}, runner.ResponseInput{Action: askuser.ActionAccept})
	if err == nil {
		t.Fatal("hook accepted an unchallenged shell digest")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v, want not found", err)
	}
	if approvals.Consume("conv-shell", digest) {
		t.Fatal("unchallenged digest was approved")
	}
}

func TestShellResumeHookRejectsMismatchedQuestion(t *testing.T) {
	approvals := tools.NewShellApprovals()
	hook := newShellResumeHook(approvals)
	challenge := approvals.CreateChallenge("conv-shell", "echo sensitive", "D:\\work")

	err := hook(context.Background(), askuser.Pending{
		ConversationID: "conv-shell",
		Kind:           tools.KindApproval,
		Question:       "Approve a harmless status check?",
		ResumeContext:  rawJSON(t, map[string]string{"type": "shell_exec_approval", "command_sha256": challenge.Digest}),
	}, runner.ResponseInput{Action: askuser.ActionAccept})
	if err == nil {
		t.Fatal("hook accepted a mismatched approval question")
	}
	if !strings.Contains(err.Error(), "question mismatch") {
		t.Fatalf("error = %v, want question mismatch", err)
	}
	if approvals.Consume("conv-shell", challenge.Digest) {
		t.Fatal("mismatched question was approved")
	}
}

func TestShellResumeHookIgnoresDeclineCancelAndUnrelatedContext(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action string
		kind   string
		rc     json.RawMessage
	}{
		{
			name:   "decline",
			action: askuser.ActionDecline,
			kind:   tools.KindApproval,
			rc:     rawJSON(t, map[string]string{"type": "shell_exec_approval", "command_sha256": "declined"}),
		},
		{
			name:   "cancel",
			action: askuser.ActionCancel,
			kind:   tools.KindApproval,
			rc:     rawJSON(t, map[string]string{"type": "shell_exec_approval", "command_sha256": "cancelled"}),
		},
		{
			name:   "unrelated resume context",
			action: askuser.ActionAccept,
			kind:   tools.KindApproval,
			rc:     rawJSON(t, map[string]string{"type": "skill_approval", "command_sha256": "unrelated"}),
		},
		{
			name:   "not approval kind",
			action: askuser.ActionAccept,
			kind:   tools.KindClarification,
			rc:     rawJSON(t, map[string]string{"type": "shell_exec_approval", "command_sha256": "wrong-kind"}),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			approvals := tools.NewShellApprovals()
			hook := newShellResumeHook(approvals)
			err := hook(context.Background(), askuser.Pending{
				ConversationID: "conv-shell",
				Kind:           tc.kind,
				ResumeContext:  tc.rc,
			}, runner.ResponseInput{Action: tc.action})
			if err != nil {
				t.Fatalf("hook: %v", err)
			}
			if approvals.Consume("conv-shell", digestFromContext(t, tc.rc)) {
				t.Fatal("ignored resume context should not approve digest")
			}
		})
	}
}

func TestChainResumeHooksRunsAllHooks(t *testing.T) {
	var calls []string
	hook := chainResumeHooks(
		func(context.Context, askuser.Pending, runner.ResponseInput) error {
			calls = append(calls, "skill")
			return nil
		},
		nil,
		func(context.Context, askuser.Pending, runner.ResponseInput) error {
			calls = append(calls, "shell")
			return nil
		},
	)

	if err := hook(context.Background(), askuser.Pending{}, runner.ResponseInput{}); err != nil {
		t.Fatalf("hook: %v", err)
	}
	if len(calls) != 2 || calls[0] != "skill" || calls[1] != "shell" {
		t.Fatalf("hooks called in order = %#v, want skill then shell", calls)
	}
}

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return raw
}

func digestFromContext(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var rc struct {
		CommandSHA256 string `json:"command_sha256"`
	}
	if err := json.Unmarshal(raw, &rc); err != nil {
		t.Fatalf("unmarshal resume context: %v", err)
	}
	return rc.CommandSHA256
}
