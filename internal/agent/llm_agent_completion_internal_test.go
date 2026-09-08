package agent

import (
	"github.com/chetto1983/aura/internal/llm"
	"testing"
)

func TestLastUserRequest_SkipsCurrentAndRetainedNudges(t *testing.T) {
	history := []llm.Message{{Role: llm.RoleUser, Content: "build me the spreadsheet"}}
	for _, nudge := range []string{
		recoveryNudgeGeneric, recoveryNudgeEmpty, recoveryNudgeToolPrefix + "echo",
		completionVetoPrefix + "drafting notes", "Completion check FAILED again: legacy critic nudge",
		verifyOnStopNudgePrefix + "unverified", deliverOnStopNudgePrefix + "not delivered",
	} {
		history = append(history, llm.Message{Role: llm.RoleUser, Content: nudge})
	}
	history = append(history, llm.Message{Role: llm.RoleAssistant, Content: "answer"}, llm.Message{Role: llm.RoleUser, Content: " "})
	if got := lastUserRequest(history); got != "build me the spreadsheet" {
		t.Fatalf("request=%q", got)
	}
	if got := lastUserRequest(nil); got != "" {
		t.Fatalf("empty history request=%q", got)
	}
}
