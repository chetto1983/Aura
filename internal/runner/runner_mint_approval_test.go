package runner

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestMintApprovalPause_WritesWireValidPauseAndReturnsToken proves the mint writes a pause
// keyed by the returned token, keyed to the conversation, Kind=approval, with the
// scheduled_task_approval resume_context preserved — AND a matching assistant ask_user
// tool_call turn (wire-validity: the pause's ToolCallID references a real tool_call).
func TestMintApprovalPause_WritesWireValidPauseAndReturnsToken(t *testing.T) {
	conv := newFakeConvStore()
	pause := newFakePauseStore()
	r := &Runner{resumeCommitter: newSplitResumeCommitter(conv, pause), pause: pause}

	const convID = "11111111-1111-1111-1111-111111111111"
	rc := json.RawMessage(`{"type":"scheduled_task_approval","task_id":"abc123"}`)
	token, err := r.MintApprovalPause(context.Background(), convID, "Approve scheduled task?", rc)
	if err != nil {
		t.Fatalf("MintApprovalPause: %v", err)
	}
	if token == "" {
		t.Fatal("want a non-empty token")
	}

	// The pause is durable and consumable.
	got, err := pause.GetByToken(context.Background(), token)
	if err != nil {
		t.Fatalf("GetByToken(%s): %v", token, err)
	}
	if got.Kind != "approval" || got.ConversationID != convID {
		t.Fatalf("pause fields wrong: kind=%q conv=%q", got.Kind, got.ConversationID)
	}
	if got.ToolCallID == "" {
		t.Fatal("pause must carry a ToolCallID referencing the synthetic assistant tool_call")
	}
	var payload struct {
		Type   string `json:"type"`
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(got.ResumeContext, &payload); err != nil {
		t.Fatalf("resume_context not valid json: %v", err)
	}
	if payload.Type != "scheduled_task_approval" || payload.TaskID != "abc123" {
		t.Fatalf("resume_context not preserved: %s", got.ResumeContext)
	}

	// Wire-validity: history carries an assistant message whose tool_call id == pause.ToolCallID.
	history, err := conv.LoadHistory(context.Background(), convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if !hasAssistantToolCall(history, got.ToolCallID) {
		t.Fatalf("history missing the assistant ask_user tool_call %s (orphan RoleTool risk): %+v", got.ToolCallID, history)
	}
}

// TestMintApprovalPause_TokenIsUnique proves two mints on the same conversation produce
// distinct tokens (no dedup here — dedup is the ensurer adapter's job).
func TestMintApprovalPause_TokenIsUnique(t *testing.T) {
	conv := newFakeConvStore()
	pause := newFakePauseStore()
	r := &Runner{resumeCommitter: newSplitResumeCommitter(conv, pause), pause: pause}
	const convID = "22222222-2222-2222-2222-222222222222"
	rc := json.RawMessage(`{"type":"scheduled_task_approval","task_id":"t1"}`)

	t1, err := r.MintApprovalPause(context.Background(), convID, "q1", rc)
	if err != nil {
		t.Fatalf("mint 1: %v", err)
	}
	t2, err := r.MintApprovalPause(context.Background(), convID, "q2", rc)
	if err != nil {
		t.Fatalf("mint 2: %v", err)
	}
	if t1 == t2 {
		t.Fatalf("mints must yield distinct tokens, got %s twice", t1)
	}
}

func hasAssistantToolCall(history []llm.Message, toolCallID string) bool {
	for _, m := range history {
		if m.Role != llm.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID == toolCallID {
				return true
			}
		}
	}
	return false
}
