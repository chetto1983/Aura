package orchestration

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestComposePromptReportsVersionModulesAndHash(t *testing.T) {
	plan := ComposePrompt(PromptInput{
		Version:           VersionAuraAgentV1,
		Now:               time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC),
		Location:          time.UTC,
		Overlay:           "Operator guidance.",
		SkillsBlock:       "## Available Skills\n\n- **docx** - Create Word files",
		SwarmAvailable:    true,
		SandboxAvailable:  true,
		ProposalAvailable: true,
		Profile:           ProfileDocument,
	})

	if plan.Version != VersionAuraAgentV1 {
		t.Fatalf("Version = %q", plan.Version)
	}
	for _, want := range []string{ModuleBase, ModuleRuntime, ModuleOverlay, ModuleSkills, ModuleSwarm, ModuleSandbox, ModuleFileGeneration, ModuleSecurity, ModuleWikiProposals} {
		if !slices.Contains(plan.Modules, want) {
			t.Fatalf("modules missing %q: %+v", want, plan.Modules)
		}
	}
	if len(plan.Hash) != 16 {
		t.Fatalf("hash = %q, want 16-char short hash", plan.Hash)
	}
	if !strings.Contains(plan.Content, "Prompt Version: aura-agent-v1") {
		t.Fatalf("prompt missing version marker:\n%s", plan.Content)
	}
	if !strings.Contains(plan.Content, "read_skill") || !strings.Contains(plan.Content, "before using typed document/file tools") {
		t.Fatalf("prompt missing document skill preflight:\n%s", plan.Content)
	}
}

func TestSelectProfileAutoRoutesSwarmSandboxDocumentAndMemory(t *testing.T) {
	tests := []struct {
		name string
		text string
		want Profile
	}{
		{name: "swarm", text: "facciamo il punto di tutta la pipeline e cosa manca", want: ProfileSwarmResearch},
		{name: "sandbox", text: "calcola un CSV con grafico revenue e salva gli artifact", want: ProfileSandboxCompute},
		{name: "document", text: "crea un documento Word modificabile dal riepilogo della memoria", want: ProfileDocument},
		{name: "memory", text: "cosa ricordi del progetto Aura?", want: ProfileMemory},
		{name: "default", text: "ciao come va", want: ProfileDefault},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectProfile(tt.text, ProfileModeAuto, Availability{Swarm: true, Sandbox: true})
			if got != tt.want {
				t.Fatalf("SelectProfile(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestToolProfileAllowlistsKeepRiskBoundaries(t *testing.T) {
	sw, err := ToolsForProfile(ProfileSwarmResearch, Availability{Swarm: true})
	if err != nil {
		t.Fatalf("ToolsForProfile swarm: %v", err)
	}
	for _, forbidden := range []string{"write_wiki", "create_docx", "execute_code", "save_tool", "schedule_task"} {
		if slices.Contains(sw, forbidden) {
			t.Fatalf("swarm profile includes forbidden %q: %+v", forbidden, sw)
		}
	}
	for _, required := range []string{"run_aurabot_swarm", "read_swarm_result", "search_memory", "list_sources"} {
		if !slices.Contains(sw, required) {
			t.Fatalf("swarm profile missing %q: %+v", required, sw)
		}
	}

	sb, err := ToolsForProfile(ProfileSandboxCompute, Availability{Sandbox: true})
	if err != nil {
		t.Fatalf("ToolsForProfile sandbox: %v", err)
	}
	for _, required := range []string{"execute_code", "list_tools", "read_tool", "list_sources", "read_source"} {
		if !slices.Contains(sb, required) {
			t.Fatalf("sandbox profile missing %q: %+v", required, sb)
		}
	}
	if slices.Contains(sb, "save_tool") || slices.Contains(sb, "write_wiki") {
		t.Fatalf("sandbox profile exposes admin/write tools: %+v", sb)
	}

	doc, err := ToolsForProfile(ProfileDocument, Availability{Swarm: true})
	if err != nil {
		t.Fatalf("ToolsForProfile document: %v", err)
	}
	for _, required := range []string{"list_skills", "read_skill", "search_memory", "run_aurabot_swarm", "read_swarm_result", "create_docx", "create_xlsx", "create_pdf"} {
		if !slices.Contains(doc, required) {
			t.Fatalf("document profile missing %q: %+v", required, doc)
		}
	}
}

func TestExplicitProfileModeOverridesAuto(t *testing.T) {
	got := SelectProfile("crea un grafico", "memory", Availability{Swarm: true, Sandbox: true})
	if got != ProfileMemory {
		t.Fatalf("explicit profile = %q, want memory", got)
	}
}
