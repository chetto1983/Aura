package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Measured 2026-11-12 on the lab VM: asked to email a job's summary to someone else, the model
// scheduled notify=email and confirmed that address, while the email route reaches only the
// operator's own. The approval carries where the result really goes, so the APPROVED answer can
// say it.
func TestTheApprovalCarriesWhereTheResultGoes(t *testing.T) {
	t.Parallel()
	store := &messageTaskStore{}
	tool := &TaskTool{Store: store, Destinations: &fakeDestinations{recipient: "operator@example.com"}}
	raw := `{"action":"schedule","kind":"agent_job",` + inTwoHours() + `,"payload":{"goal":"Riassumi la posta"},"notify":"email"}`
	pause, ok := tool.HostReview(context.Background(), "conv-1", "call-1", json.RawMessage(raw))
	if !ok {
		t.Fatal("the agent_job was not parked")
	}
	var resume map[string]string
	if err := json.Unmarshal(pause.ResumeContext, &resume); err != nil {
		t.Fatalf("resume context: %v", err)
	}
	if got, want := resume["delivers"], "Delivers via email to operator@example.com."; got != want {
		t.Fatalf("delivers = %q, want %q", got, want)
	}
	if resume["type"] != "scheduled_task_approval" || resume["task_id"] != "task-1" {
		t.Fatalf("resume context = %v", resume)
	}
}

func TestTheDescriptionSaysNotifyReachesOnlyTheOperator(t *testing.T) {
	t.Parallel()
	desc := (&TaskTool{}).Spec().Description
	for _, want := range []string{
		"notify reaches only the operator",
		"cannot be sent to someone else",
	} {
		if !strings.Contains(desc, want) {
			t.Errorf("description lacks %q", want)
		}
	}
}
