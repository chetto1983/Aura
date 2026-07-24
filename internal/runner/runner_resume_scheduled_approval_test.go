package runner

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/askuser"
)

// schedApprovalPending builds a scheduled_task_approval ask_user pause like the model
// relay (or the sweep mint) creates — kind=approval + resume_context carrying the task id.
func schedApprovalPending(taskID string) askuser.Pending {
	rc, _ := json.Marshal(map[string]string{"type": "scheduled_task_approval", "task_id": taskID})
	return askuser.Pending{
		Token:          "tok-1",
		ConversationID: "conv-1",
		Kind:           "approval",
		ToolCallID:     "call-1",
		ResumeContext:  rc,
	}
}

// On accept, the resumed model MUST be told the scheduled task is now active (the
// resume-hook activation is a silent side-effect) or it re-runs task schedule in a loop
// (the E2E BUG-1B loop). The RoleTool answer must confirm activation + forbid re-scheduling
// and stay keyed to the original tool_call_id (wire-validity).
func TestAnswerTurn_ScheduledApprovalAccept_TellsModelTaskActive(t *testing.T) {
	r := &Runner{}
	turn := r.answerTurn(schedApprovalPending("019f9349-e0e5-77cc-9639-804bcf88b968"),
		ResponseInput{Action: askuser.ActionAccept, Content: "yes"})

	if turn.ToolCallID != "call-1" {
		t.Fatalf("answer must key the original tool_call_id, got %q", turn.ToolCallID)
	}
	c := turn.Content
	if c == "yes" {
		t.Fatalf("accept must not inject the raw button value — the model learns nothing from %q", c)
	}
	if !strings.Contains(strings.ToLower(c), "active") {
		t.Fatalf("accept content must confirm the task is active, got %q", c)
	}
	if !strings.Contains(c, "019f9349") {
		t.Fatalf("accept content should reference the short task id, got %q", c)
	}
	if !strings.Contains(strings.ToLower(c), "not") { // "do NOT schedule again"
		t.Fatalf("accept content must forbid re-scheduling, got %q", c)
	}
}

// On decline the resume hook cancels the task; the model must be told it is cancelled (and
// not re-schedule), overriding the generic "user declined to answer" marker.
func TestAnswerTurn_ScheduledApprovalDecline_TellsModelCancelled(t *testing.T) {
	r := &Runner{}
	turn := r.answerTurn(schedApprovalPending("019f934b-b0dd-7ef3-88f8-6a92a608d7ed"),
		ResponseInput{Action: askuser.ActionDecline})

	if turn.Content == declinedContent {
		t.Fatalf("scheduled decline must override the generic declined marker")
	}
	c := strings.ToLower(turn.Content)
	if !strings.Contains(c, "cancel") && !strings.Contains(c, "reject") {
		t.Fatalf("decline content must confirm cancellation, got %q", turn.Content)
	}
}

// A plain approval pause (no scheduled_task_approval resume_context) is untouched — the raw
// accept content still flows through so ordinary HITL is unaffected.
func TestAnswerTurn_NonScheduledApproval_Unchanged(t *testing.T) {
	r := &Runner{}
	p := askuser.Pending{Token: "t", ConversationID: "c", Kind: "approval", ToolCallID: "call-9"}
	turn := r.answerTurn(p, ResponseInput{Action: askuser.ActionAccept, Content: "yes"})
	if turn.Content != "yes" {
		t.Fatalf("non-scheduled approval accept must keep the raw content, got %q", turn.Content)
	}
}
