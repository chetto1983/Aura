package main

import (
	"slices"
	"testing"

	"github.com/aura/aura/internal/orchestration"
)

func TestBuildReportRoutesPipelinePromptToSwarm(t *testing.T) {
	report := buildReport(reportInput{
		Prompt:        "facciamo il punto di tutta la pipeline",
		Mode:          orchestration.ProfileModeAuto,
		Swarm:         true,
		Sandbox:       true,
		Proposals:     true,
		PromptVersion: orchestration.VersionAuraAgentV1,
	})

	if report.Profile != string(orchestration.ProfileSwarmResearch) {
		t.Fatalf("Profile = %q, want swarm_research", report.Profile)
	}
	if report.ProfileSelectReason == "" {
		t.Fatal("ProfileSelectReason is empty")
	}
	for _, want := range []string{"run_aurabot_swarm", "read_swarm_result", "list_swarm_tasks"} {
		if !slices.Contains(report.ToolsExposed, want) {
			t.Fatalf("ToolsExposed missing %q: %+v", want, report.ToolsExposed)
		}
	}
	for _, hidden := range []string{"search_memory", "search_wiki", "read_source"} {
		if slices.Contains(report.ToolsExposed, hidden) {
			t.Fatalf("ToolsExposed includes parent direct-read tool %q: %+v", hidden, report.ToolsExposed)
		}
	}
}

func TestBuildReportReportsPromptMetadata(t *testing.T) {
	report := buildReport(reportInput{
		Prompt:        "calcola un csv con grafico",
		Mode:          orchestration.ProfileModeAuto,
		Sandbox:       true,
		PromptVersion: orchestration.VersionAuraAgentV1,
	})

	if report.Profile != string(orchestration.ProfileSandboxCompute) {
		t.Fatalf("Profile = %q, want sandbox_compute", report.Profile)
	}
	if report.PromptVersion != orchestration.VersionAuraAgentV1 {
		t.Fatalf("PromptVersion = %q", report.PromptVersion)
	}
	if report.PromptHash == "" {
		t.Fatal("PromptHash is empty")
	}
	if !slices.Contains(report.PromptModules, orchestration.ModuleSandbox) {
		t.Fatalf("PromptModules missing sandbox: %+v", report.PromptModules)
	}
	if !slices.Contains(report.ToolsExposed, "execute_code") {
		t.Fatalf("ToolsExposed missing execute_code: %+v", report.ToolsExposed)
	}
}
