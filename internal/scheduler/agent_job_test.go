package scheduler

import "testing"

func TestNormalizeAgentJobPayload_TextGoal(t *testing.T) {
	got, err := NormalizeAgentJobPayload("Check sources and propose wiki updates.")
	if err != nil {
		t.Fatalf("NormalizeAgentJobPayload: %v", err)
	}
	if got.Goal != "Check sources and propose wiki updates." {
		t.Errorf("Goal = %q", got.Goal)
	}
	if got.WritePolicy != AgentJobWritePolicyProposeOnly {
		t.Errorf("WritePolicy = %q", got.WritePolicy)
	}
	if got.Notify == nil || !*got.Notify {
		t.Fatal("Notify should default true")
	}
	if len(got.ToolAllowlist) == 0 {
		t.Fatal("ToolAllowlist should default")
	}
}

func TestNormalizeAgentJobPayload_JSON(t *testing.T) {
	got, err := NormalizeAgentJobPayload(`{
		"goal":"Check markets",
		"tool_allowlist":["web_search","web_search","propose_wiki_change"],
		"notify":false
	}`)
	if err != nil {
		t.Fatalf("NormalizeAgentJobPayload: %v", err)
	}
	if got.Goal != "Check markets" {
		t.Errorf("Goal = %q", got.Goal)
	}
	if len(got.ToolAllowlist) != 2 || got.ToolAllowlist[0] != "web_search" || got.ToolAllowlist[1] != "propose_wiki_change" {
		t.Errorf("ToolAllowlist = %#v", got.ToolAllowlist)
	}
	if got.Notify == nil || *got.Notify {
		t.Fatal("Notify should preserve false")
	}
}

func TestNormalizeAgentJobPayload_RejectsBadInput(t *testing.T) {
	for _, raw := range []string{
		"",
		`{"goal":""}`,
		`{"goal":"x","write_policy":"direct_write"}`,
		`{bad json`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := NormalizeAgentJobPayload(raw); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
