package agentdef

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestValidator_WarnsSubagentsOnWorker(t *testing.T) {
	logs, err := validateWithLogs(
		delegatingDefinition("worker", TierWorker, "helper"),
		validDefinition("helper", TierWorker),
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	assertLogContains(t, logs, "worker_has_subagents")
}

func TestValidator_WarnsSameTierDelegation(t *testing.T) {
	logs, err := validateWithLogs(
		delegatingDefinition("planner", TierChat, "critic"),
		validDefinition("critic", TierChat),
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	assertLogContains(t, logs, "same_tier_delegation")
}

func TestValidator_WarnsMissingTier(t *testing.T) {
	def := validDefinition("worker", "")
	logs, err := validateWithLogs(def)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	assertLogContains(t, logs, "missing_tier_default_worker")
}

func TestValidator_WarnsCycle(t *testing.T) {
	logs, err := validateWithLogs(
		delegatingDefinition("planner", TierChat, "worker"),
		delegatingDefinition("worker", TierWorker, "planner"),
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	assertLogContains(t, logs, "cycle")
}

func TestValidator_ValidChainQuiet(t *testing.T) {
	var logs bytes.Buffer
	err := (&Validator{Logger: slog.New(slog.NewTextHandler(&logs, nil))}).Validate([]AgentDefinition{
		delegatingDefinition("planner", TierChat, "worker"),
		validDefinition("worker", TierWorker),
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if logs.Len() != 0 {
		t.Fatalf("logs = %q, want quiet validator", logs.String())
	}
}

func TestValidator_EnforceTierRejectsPolicyViolations(t *testing.T) {
	tests := []struct {
		name string
		defs []AgentDefinition
		want string
	}{
		{
			name: "worker subagents",
			defs: []AgentDefinition{
				delegatingDefinition("worker", TierWorker, "helper"),
				validDefinition("helper", TierWorker),
			},
			want: "worker_has_subagents",
		},
		{
			name: "same tier",
			defs: []AgentDefinition{
				delegatingDefinition("planner", TierChat, "critic"),
				validDefinition("critic", TierChat),
			},
			want: "same_tier_delegation",
		},
		{
			name: "missing tier",
			defs: []AgentDefinition{validDefinition("worker", "")},
			want: "missing_tier_default_worker",
		},
		{
			name: "cycle",
			defs: []AgentDefinition{
				delegatingDefinition("planner", TierChat, "worker"),
				delegatingDefinition("worker", TierWorker, "planner"),
			},
			want: "cycle",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := (&Validator{EnforceTier: true}).Validate(tt.defs)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidator_EnforceTierAllowsSummarizer(t *testing.T) {
	def, err := BuiltinDefinition("summarizer")
	if err != nil {
		t.Fatalf("BuiltinDefinition: %v", err)
	}
	if err := (&Validator{EnforceTier: true}).Validate([]AgentDefinition{def}); err != nil {
		t.Fatalf("Validate summarizer: %v", err)
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

func assertLogContains(t *testing.T, logs, want string) {
	t.Helper()
	if !strings.Contains(logs, want) {
		t.Fatalf("logs = %q, want %q", logs, want)
	}
}

func validateWithLogs(defs ...AgentDefinition) (string, error) {
	var logs bytes.Buffer
	err := (&Validator{Logger: slog.New(slog.NewTextHandler(&logs, nil))}).Validate(defs)
	return logs.String(), err
}
