package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
)

type fakePauseLister struct {
	pending []askuser.Pending
	err     error
}

func (f fakePauseLister) ListPending(_ context.Context, _ string) ([]askuser.Pending, error) {
	return f.pending, f.err
}

type recordingMinter struct {
	token   string
	convID  string
	q       string
	rc      json.RawMessage
	calls   int
	mintErr error
}

func (m *recordingMinter) MintApprovalPause(_ context.Context, convID, question string, resumeContext json.RawMessage) (string, error) {
	m.calls++
	m.convID = convID
	m.q = question
	m.rc = resumeContext
	if m.mintErr != nil {
		return "", m.mintErr
	}
	return m.token, nil
}

// TestEnsureApprovalPause_ReusesMatchingPending proves an existing pending
// scheduled_task_approval pause for the same task is REUSED (no mint) — the sweep must never
// mint a duplicate across cadences or when the model already relayed.
func TestEnsureApprovalPause_ReusesMatchingPending(t *testing.T) {
	lister := fakePauseLister{pending: []askuser.Pending{
		{Token: "existing-tok", Kind: tools.KindApproval,
			ResumeContext: rawJSON(t, map[string]string{"type": "scheduled_task_approval", "task_id": "task-7"})},
	}}
	minter := &recordingMinter{token: "fresh-tok"}
	e := approvalPauseEnsurer{pauses: lister, minter: minter}

	tok, err := e.EnsureApprovalPause(context.Background(), "task-7", "conv-A", "agent_job")
	if err != nil {
		t.Fatalf("EnsureApprovalPause: %v", err)
	}
	if tok != "existing-tok" {
		t.Errorf("token = %q, want the reused existing-tok", tok)
	}
	if minter.calls != 0 {
		t.Errorf("mint calls = %d, want 0 (reuse)", minter.calls)
	}
}

// TestEnsureApprovalPause_MintsWhenAbsent proves that with no matching pending pause the ensurer
// mints one via the minter, keyed to the conversation, carrying the scheduled_task_approval
// resume_context. A non-matching pending pause (different task, different type) does not block.
func TestEnsureApprovalPause_MintsWhenAbsent(t *testing.T) {
	lister := fakePauseLister{pending: []askuser.Pending{
		{Token: "other-task", Kind: tools.KindApproval,
			ResumeContext: rawJSON(t, map[string]string{"type": "scheduled_task_approval", "task_id": "task-OTHER"})},
		{Token: "skill", Kind: tools.KindApproval,
			ResumeContext: rawJSON(t, map[string]string{"type": "skill_approval"})},
	}}
	minter := &recordingMinter{token: "fresh-tok"}
	e := approvalPauseEnsurer{pauses: lister, minter: minter}

	tok, err := e.EnsureApprovalPause(context.Background(), "task-7", "conv-A", "agent_job")
	if err != nil {
		t.Fatalf("EnsureApprovalPause: %v", err)
	}
	if tok != "fresh-tok" || minter.calls != 1 {
		t.Fatalf("token=%q mintCalls=%d, want fresh-tok/1", tok, minter.calls)
	}
	if minter.convID != "conv-A" {
		t.Errorf("mint conv = %q, want conv-A", minter.convID)
	}
	var rc struct {
		Type   string `json:"type"`
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(minter.rc, &rc); err != nil {
		t.Fatalf("minted resume_context invalid: %v", err)
	}
	if rc.Type != "scheduled_task_approval" || rc.TaskID != "task-7" {
		t.Errorf("minted resume_context = %s, want scheduled_task_approval/task-7", minter.rc)
	}
}

// TestEnsureApprovalPause_ListError surfaces a pending-read failure (never silently mints).
func TestEnsureApprovalPause_ListError(t *testing.T) {
	e := approvalPauseEnsurer{pauses: fakePauseLister{err: errors.New("db down")}, minter: &recordingMinter{}}
	if _, err := e.EnsureApprovalPause(context.Background(), "task-7", "conv-A", "agent_job"); err == nil {
		t.Fatal("want the list-pending error to surface")
	}
}
