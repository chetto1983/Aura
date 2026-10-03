package agui

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

func TestRecoveredToolResultIsNotProjectedAsSuccess(t *testing.T) {
	var call llm.ToolCall
	call.ID, call.Type, call.Function.Name = "recovered-call", "function", "shell_exec"
	unknown := `error: previous result unknown after crash recovery for tool "shell_exec"; verify before re-running this tool call.`
	for _, content := range []string{unknown, "9137", "error: ordinary command output"} {
		history := []llm.Message{
			{Role: llm.RoleUser, Content: unknown},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
			{Role: llm.RoleTool, ToolCallID: call.ID, Content: content},
		}
		encoded, err := json.Marshal(projectDisplaySnapshot(history))
		if err != nil {
			t.Fatal(err)
		}
		var decoded struct {
			Messages []struct {
				Content string `json:"content"`
				IsError bool   `json:"isError"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Messages[0].IsError || decoded.Messages[2].IsError != (content == unknown) {
			t.Fatalf("incorrect recovery outcome: %s", encoded)
		}
		if decoded.Messages[2].Content != content {
			t.Fatalf("result changed: %s", encoded)
		}
	}
}

// TestPendingPauseIsAwaitingNotFailed pins that a call whose turn waits on the person's
// answer replays as waiting: the recovery placeholder LoadHistory pairs with it is dropped
// and the call carries awaitingInput. A call with no pending pause keeps the error flag.
func TestPendingPauseIsAwaitingNotFailed(t *testing.T) {
	var call llm.ToolCall
	call.ID, call.Type, call.Function.Name = "pause-call", "function", "ask_user"
	cases := []struct {
		name         string
		pending      map[string]struct{}
		wantMessages int
		wantAwaiting bool
	}{
		{"pending pause", map[string]struct{}{call.ID: {}}, 2, true},
		{"no pending pause", nil, 3, false},
		{"another call's pause", map[string]struct{}{"other-call": {}}, 3, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := projectDisplaySnapshot([]llm.Message{
				{Role: llm.RoleUser, Content: "remind me a week before"},
				{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
				{Role: llm.RoleTool, ToolCallID: call.ID, Content: conversations.RecoveryToolResultContent(call)},
			})
			markAwaitingInput(&snap, tc.pending)
			if len(snap.Messages) != tc.wantMessages {
				t.Fatalf("messages = %d, want %d", len(snap.Messages), tc.wantMessages)
			}
			if got := snap.Messages[1].ToolCalls[0].AwaitingInput; got != tc.wantAwaiting {
				t.Fatalf("awaitingInput = %v, want %v", got, tc.wantAwaiting)
			}
			if !tc.wantAwaiting && !snap.Messages[2].IsError {
				t.Fatal("a call with no pending pause lost its recovery error flag")
			}
		})
	}
}

func TestPendingToolCallsKeepsThisThreadsPauses(t *testing.T) {
	s := &Server{approvals: &fakeApprovalStore{pendings: []askuser.Pending{
		{ConversationID: "thread-1", ToolCallID: "call-a"},
		{ConversationID: "thread-1", ToolCallID: ""},
		{ConversationID: "thread-2", ToolCallID: "call-b"},
	}}}
	got := s.pendingToolCalls(context.Background(), "thread-1")
	if _, ok := got["call-a"]; !ok || len(got) != 1 {
		t.Fatalf("pending = %v, want only call-a", got)
	}
	if (&Server{}).pendingToolCalls(context.Background(), "thread-1") != nil {
		t.Fatal("no approval store must yield no pending calls")
	}
	failing := &Server{approvals: &fakeApprovalStore{err: errors.New("db down")}}
	if failing.pendingToolCalls(context.Background(), "thread-1") != nil {
		t.Fatal("a failed read must yield no pending calls")
	}
}
