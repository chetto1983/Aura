package agent

// White-box test for the one coupling the black-box suite cannot see: the verify-on-stop
// nudge is injected as a USER-role message, so lastUserRequest must skip it. Without
// this, the completion critic on a later round grades the turn against Aura's own
// injection instead of the user's request.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestVerifyNudgeIsRecognisedAsAnAgentNudge(t *testing.T) {
	cwd := filepath.Join("workspace", "src")
	ledger := &fakeLedger{facts: map[string]ProjectFacts{cwd: nodeProject("go test ./...")}}

	nudge, ok := BuildVerifyOnStopNudge(VerifyOnStopRequest{
		Ledger: ledger, SessionID: "s1",
		ChangedPaths: []string{filepath.Join(cwd, "app.go")},
		MaxAttempts:  verificationMaxAttempts,
	})
	if !ok {
		t.Fatal("expected a nudge")
	}
	if !strings.HasPrefix(nudge, verifyOnStopNudgePrefix) {
		t.Fatalf("nudge no longer starts with the constant isAgentNudge matches on: %.60s", nudge)
	}
	if !isAgentNudge(nudge) {
		t.Fatal("isAgentNudge must recognise the verify-on-stop nudge")
	}

	history := []llm.Message{
		{Role: llm.RoleSystem, Content: "sys"},
		{Role: llm.RoleUser, Content: "fix the parser"},
		{Role: llm.RoleAssistant, Content: "patched it"},
		{Role: llm.RoleUser, Content: nudge},
	}
	if got := lastUserRequest(history); got != "fix the parser" {
		t.Errorf("lastUserRequest = %.60q, want the genuine request", got)
	}
}
