package runner

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
)

// Measured 2026-11-12: the model confirmed a destination the route never reaches. The approved
// answer repeats where the result goes, from the host-written resume context.
func TestTheApprovedAnswerNamesTheDestination(t *testing.T) {
	t.Parallel()
	pending := askuser.Pending{Kind: tools.KindApproval, ResumeContext: json.RawMessage(
		`{"type":"scheduled_task_approval","task_id":"01a1cec3-0000-0000-0000-000000000000","delivers":"Delivers via email to operator@example.com."}`)}
	answer := scheduledApprovalAnswer(pending, ResponseInput{Action: askuser.ActionAccept})
	for _, want := range []string{"Operator APPROVED scheduled task 01a1cec3.", "Delivers via email to operator@example.com.", "nobody else receives it"} {
		if !strings.Contains(answer, want) {
			t.Errorf("answer lacks %q:\n%s", want, answer)
		}
	}
	pending.ResumeContext = json.RawMessage(`{"type":"scheduled_task_approval","task_id":"01a1cec3"}`)
	if answer := scheduledApprovalAnswer(pending, ResponseInput{Action: askuser.ActionAccept}); strings.Contains(answer, "nobody else") {
		t.Fatalf("a minted approval with no delivery note claims one:\n%s", answer)
	}
}
