package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/aura/aura/internal/orchestration"
)

func TestBuildReportRouteEvals(t *testing.T) {
	tests := []struct {
		name           string
		prompt         string
		wantProfile    orchestration.Profile
		wantTools      []string
		forbiddenTools []string
		wantSkills     []string
		wantTerminal   []string
	}{
		{
			name:           "pipeline audit routes to swarm research",
			prompt:         "facciamo il punto di tutta la pipeline Aura e dimmi cosa manca",
			wantProfile:    orchestration.ProfileSwarmResearch,
			wantTools:      []string{"run_aurabot_swarm", "read_swarm_result", "list_swarm_tasks"},
			forbiddenTools: []string{"search_memory", "search_wiki", "read_source", "execute_code", "write_wiki"},
			wantTerminal:   []string{"run_aurabot_swarm"},
		},
		{
			name:           "memory answer routes to memory",
			prompt:         "cosa sai dalla memoria sul progetto Aura?",
			wantProfile:    orchestration.ProfileMemory,
			wantTools:      []string{"search_memory", "search_wiki", "read_wiki", "read_source"},
			forbiddenTools: []string{"execute_code", "create_docx", "create_xlsx", "create_pdf", "schedule_task"},
			wantSkills:     []string{"aura-memory-audit"},
		},
		{
			name:           "document summary routes to document",
			prompt:         "crea un documento con il riepilogo dei documenti che hai",
			wantProfile:    orchestration.ProfileDocument,
			wantTools:      []string{"list_skills", "read_skill", "search_memory", "read_source", "create_docx", "create_xlsx", "create_pdf"},
			forbiddenTools: []string{"install_skill", "delete_skill", "settings_update"},
			wantSkills:     []string{"aura-source-extraction", "documents:documents", "docx", "document-pdf", "xlsx"},
		},
		{
			name:           "computed csv chart routes to sandbox compute",
			prompt:         "calcola una tabella CSV e un grafico sui tempi E2E",
			wantProfile:    orchestration.ProfileSandboxCompute,
			wantTools:      []string{"execute_code", "list_skills", "read_skill", "read_source", "store_source"},
			forbiddenTools: []string{"write_wiki", "schedule_task", "install_skill", "delete_skill", "settings_update"},
			wantSkills:     []string{"aura-source-extraction", "systematic-debugging", "test-driven-development"},
			wantTerminal:   []string{"execute_code"},
		},
		{
			name:           "admin review routes to admin review",
			prompt:         "apri le settings e verifica SEARCH_BACKEND nella coda review",
			wantProfile:    orchestration.ProfileAdminReview,
			wantTools:      []string{"daily_briefing", "list_tasks", "propose_wiki_change", "propose_skill_change", "list_skills", "read_skill"},
			forbiddenTools: []string{"write_wiki", "execute_code", "run_task_now", "install_skill", "delete_skill", "settings_update"},
			wantSkills:     []string{"aura-memory-audit", "codex-security:security-scan"},
		},
		{
			name:           "docker release check routes to admin review",
			prompt:         "prepara la docker release e controlla la review queue prima di pubblicare",
			wantProfile:    orchestration.ProfileAdminReview,
			wantTools:      []string{"daily_briefing", "list_tasks", "propose_wiki_change", "propose_skill_change", "list_skills", "read_skill"},
			forbiddenTools: []string{"write_wiki", "execute_code", "run_task_now", "install_skill", "delete_skill", "settings_update"},
			wantSkills:     []string{"aura-memory-audit", "codex-security:security-scan"},
		},
		{
			name:           "mcp plugin install proposal routes to admin review",
			prompt:         "proponi installazione plugin MCP dal marketplace e mettila in approval queue",
			wantProfile:    orchestration.ProfileAdminReview,
			wantTools:      []string{"daily_briefing", "list_tasks", "propose_wiki_change", "propose_skill_change", "list_skills", "read_skill"},
			forbiddenTools: []string{"write_wiki", "execute_code", "run_task_now", "install_skill", "delete_skill", "settings_update"},
			wantSkills:     []string{"aura-memory-audit", "codex-security:security-scan"},
		},
		{
			name:           "ordinary answer routes to default",
			prompt:         "spiegami in due righe cosa significa retrieval augmented generation",
			wantProfile:    orchestration.ProfileDefault,
			wantTools:      []string{"search_memory", "web_search", "daily_briefing", "write_wiki"},
			forbiddenTools: []string{"execute_code", "run_aurabot_swarm", "install_skill", "delete_skill", "request_dashboard_token"},
			wantSkills:     []string{"aura-memory-audit"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := buildReport(reportInput{
				Prompt:        tt.prompt,
				Mode:          orchestration.ProfileModeAuto,
				Swarm:         true,
				Sandbox:       true,
				Proposals:     true,
				PromptVersion: orchestration.VersionAuraAgentV1,
			})

			if report.Profile != string(tt.wantProfile) {
				t.Fatalf("Profile = %q, want %q; reason=%s", report.Profile, tt.wantProfile, report.ProfileSelectReason)
			}
			if report.ProfileSelectReason == "" {
				t.Fatal("ProfileSelectReason is empty")
			}
			if len(report.Capabilities) == 0 {
				t.Fatal("Capabilities is empty")
			}
			for _, want := range tt.wantTools {
				if !slices.Contains(report.ToolsExposed, want) {
					t.Fatalf("ToolsExposed missing %q: %+v", want, report.ToolsExposed)
				}
			}
			for _, forbidden := range tt.forbiddenTools {
				if slices.Contains(report.ToolsExposed, forbidden) {
					t.Fatalf("ToolsExposed includes forbidden tool %q: %+v", forbidden, report.ToolsExposed)
				}
			}
			for _, want := range tt.wantSkills {
				if !reportMentionsSkillGuidance(report, want) {
					t.Fatalf("skill guidance missing %q: %+v", want, report.SkillPreflightGuidance)
				}
			}
			for _, want := range tt.wantTerminal {
				if !slices.Contains(report.TerminalTools, want) {
					t.Fatalf("TerminalTools missing %q: %+v", want, report.TerminalTools)
				}
			}
			if report.LoopPolicy.MaxSteps <= 0 {
				t.Fatalf("LoopPolicy.MaxSteps = %d, want positive", report.LoopPolicy.MaxSteps)
			}
			if report.LoopPolicy.MaxElapsed == "" || report.LoopPolicy.MaxElapsed == "0s" {
				t.Fatalf("LoopPolicy.MaxElapsed = %q, want positive duration", report.LoopPolicy.MaxElapsed)
			}
		})
	}
}

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

func TestBuildReportDoesNotExposeStaleAliasesOrYamlRefs(t *testing.T) {
	input := reportInput{
		Prompt:        "crea un documento con il riepilogo dei documenti che hai",
		Mode:          orchestration.ProfileModeAuto,
		Swarm:         true,
		Sandbox:       true,
		Proposals:     true,
		PromptVersion: orchestration.VersionAuraAgentV1,
		Overlay:       "Overlay without stale refs.",
		SkillsBlock:   "Installed skills: documents:documents, xlsx.",
	}
	report := buildReport(input)

	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	combined := strings.ToLower(string(raw))
	staleRefs := []string{"golem", ".yaml"}
	for _, stale := range staleRefs {
		if strings.Contains(combined, stale) {
			t.Fatalf("report contains stale ref %q:\n%s", stale, combined)
		}
	}

	promptPlan := orchestration.ComposePrompt(orchestration.PromptInput{
		Version:           input.PromptVersion,
		Overlay:           input.Overlay,
		SkillsBlock:       input.SkillsBlock,
		SwarmAvailable:    input.Swarm,
		SandboxAvailable:  input.Sandbox,
		ProposalAvailable: input.Proposals,
		Profile:           orchestration.ProfileDocument,
	})
	promptContent := strings.ToLower(promptPlan.Content)
	for _, stale := range staleRefs {
		if strings.Contains(promptContent, stale) {
			t.Fatalf("prompt contains stale ref %q", stale)
		}
	}

	for _, capability := range orchestration.AllCapabilities() {
		for _, hint := range orchestration.CapabilitySkillHints(capability) {
			hint = strings.ToLower(hint)
			for _, stale := range staleRefs {
				if strings.Contains(hint, stale) {
					t.Fatalf("capability %q hint %q contains stale ref %q", capability, hint, stale)
				}
			}
		}
	}
}

func reportMentionsSkillGuidance(report report, skill string) bool {
	for _, item := range report.SkillPreflightGuidance {
		if slices.Contains(item.AllowedSkillNames, skill) || slices.Contains(item.SkillHints, skill) {
			return true
		}
	}
	return false
}
