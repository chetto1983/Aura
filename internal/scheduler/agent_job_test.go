package scheduler

import (
	"reflect"
	"testing"

	"github.com/aura/aura/internal/toolsets"
)

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

func TestNormalizeAgentJobPayload_ToolsetsSkillsAndContext(t *testing.T) {
	got, err := NormalizeAgentJobPayload(`{
		"goal":"Check markets",
		"enabled_toolsets":["memory_read","web_research","memory_read"],
		"tool_allowlist":["search_memory","web_fetch","write_wiki","search_memory"],
		"skills":[" aura-implementation ","aura-implementation"],
		"context_from":[" [[markets]] ","[[markets]]","source:src_123"],
		"wake_if_changed":["wiki:markets","wiki:markets"]
	}`)
	if err != nil {
		t.Fatalf("NormalizeAgentJobPayload: %v", err)
	}
	if got.Goal != "Check markets" {
		t.Fatalf("Goal = %q", got.Goal)
	}
	wantToolsets := []string{toolsets.ProfileMemoryRead, toolsets.ProfileWebResearch, toolsets.ProfileSkillsRead}
	if !reflect.DeepEqual(got.EnabledToolsets, wantToolsets) {
		t.Fatalf("EnabledToolsets = %+v, want %+v", got.EnabledToolsets, wantToolsets)
	}
	wantTools := []string{"search_memory", "web_fetch", "list_skills", "read_skill", "search_skill_catalog"}
	if !reflect.DeepEqual(got.ToolAllowlist, wantTools) {
		t.Fatalf("ToolAllowlist = %+v, want %+v", got.ToolAllowlist, wantTools)
	}
	if !reflect.DeepEqual(got.Skills, []string{"aura-implementation"}) {
		t.Fatalf("Skills = %+v", got.Skills)
	}
	if !reflect.DeepEqual(got.ContextFrom, []string{"[[markets]]", "source:src_123"}) {
		t.Fatalf("ContextFrom = %+v", got.ContextFrom)
	}
	if !reflect.DeepEqual(got.WakeIfChanged, []string{"wiki:markets"}) {
		t.Fatalf("WakeIfChanged = %+v", got.WakeIfChanged)
	}
}

func TestNormalizeAgentJobPayload_RejectsUnknownToolset(t *testing.T) {
	if _, err := NormalizeAgentJobPayload(`{"goal":"x","enabled_toolsets":["memory_write"]}`); err == nil {
		t.Fatal("expected unknown toolset error")
	}
}

func TestNormalizeAgentJobPayload_RejectsSandboxCodeToolset(t *testing.T) {
	_, err := NormalizeAgentJobPayload(`{"goal":"x","enabled_toolsets":["sandbox_code"]}`)
	if err == nil {
		t.Fatal("expected sandbox_code to be outside scheduled agent job perimeter")
	}
}

func TestResolveAgentJobTools_RejectsRequestedToolOutsideEnabledToolsets(t *testing.T) {
	_, err := ResolveAgentJobTools([]string{toolsets.ProfileMemoryRead}, []string{"web_search"}, false)
	if err == nil {
		t.Fatal("expected tool allowlist/toolset mismatch error")
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
