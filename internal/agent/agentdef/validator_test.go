package agentdef

import (
	"strings"
	"testing"
)

func TestValidator_RejectsSubagentsOnWorker(t *testing.T) {
	err := (&Validator{}).Validate([]AgentDefinition{
		delegatingDefinition("worker", TierWorker, "helper"),
		validDefinition("helper", TierWorker),
	})
	if err == nil || !strings.Contains(err.Error(), "worker worker cannot declare subagents") {
		t.Fatalf("err = %v, want worker subagent rejection", err)
	}
}

func TestValidator_RejectsSameTierDelegation(t *testing.T) {
	err := (&Validator{}).Validate([]AgentDefinition{
		delegatingDefinition("planner", TierChat, "critic"),
		validDefinition("critic", TierChat),
	})
	if err == nil || !strings.Contains(err.Error(), "same-tier") {
		t.Fatalf("err = %v, want same-tier delegation rejection", err)
	}
}

func delegatingDefinition(id string, tier AgentTier, childID string) AgentDefinition {
	def := validDefinition(id, tier)
	def.WhenToUse = "delegate"
	def.Prompt = "delegate"
	def.Subagents = []SubagentRef{{ID: childID}}
	return def
}

func validDefinition(id string, tier AgentTier) AgentDefinition {
	return AgentDefinition{
		ID:        id,
		WhenToUse: "when " + id,
		Prompt:    "prompt " + id,
		Tier:      tier,
	}
}
