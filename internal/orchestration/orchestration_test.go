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
	for _, forbidden := range []string{"create_docx", "execute_code", "save_tool", "schedule_task"} {
		if slices.Contains(sw, forbidden) {
			t.Fatalf("swarm profile includes forbidden %q: %+v", forbidden, sw)
		}
	}
	for _, required := range []string{"run_aurabot_swarm", "read_swarm_result", "list_swarm_tasks"} {
		if !slices.Contains(sw, required) {
			t.Fatalf("swarm profile missing %q: %+v", required, sw)
		}
	}
	if slices.Contains(sw, "write_wiki") {
		t.Fatalf("swarm profile exposes write_wiki: %+v", sw)
	}
	for _, directRead := range []string{"search_memory", "search_wiki", "list_sources", "read_source"} {
		if slices.Contains(sw, directRead) {
			t.Fatalf("swarm profile exposes parent direct-read tool %q: %+v", directRead, sw)
		}
	}

	sb, err := ToolsForProfile(ProfileSandboxCompute, Availability{Sandbox: true})
	if err != nil {
		t.Fatalf("ToolsForProfile sandbox: %v", err)
	}
	for _, required := range []string{"execute_code", "list_tools", "read_tool", "list_skills", "read_skill", "list_sources", "read_source"} {
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

	def, err := ToolsForProfile(ProfileDefault, Availability{Proposals: true})
	if err != nil {
		t.Fatalf("ToolsForProfile default: %v", err)
	}
	for _, required := range []string{"write_wiki", "propose_wiki_change"} {
		if !slices.Contains(def, required) {
			t.Fatalf("default profile missing memory write tool %q: %+v", required, def)
		}
	}

	mem, err := ToolsForProfile(ProfileMemory, Availability{Proposals: true})
	if err != nil {
		t.Fatalf("ToolsForProfile memory: %v", err)
	}
	for _, required := range []string{"write_wiki", "propose_wiki_change"} {
		if !slices.Contains(mem, required) {
			t.Fatalf("memory profile missing memory write tool %q: %+v", required, mem)
		}
	}
}

func TestComposePromptOnlyMentionsSkillPreflightWhenSkillToolsExposed(t *testing.T) {
	base := PromptInput{
		Version:          VersionAuraAgentV1,
		Now:              time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		Location:         time.UTC,
		SkillsBlock:      "## Available Skills\n\n- **docx** - Create Word files",
		SwarmAvailable:   true,
		SandboxAvailable: true,
	}

	sw := ComposePrompt(withProfile(base, ProfileSwarmResearch))
	if strings.Contains(sw.Content, "## Available Skills") || strings.Contains(sw.Content, "Skill Preflight") {
		t.Fatalf("swarm prompt mentions skill tools that are hidden:\n%s", sw.Content)
	}

	sb := ComposePrompt(withProfile(base, ProfileSandboxCompute))
	if !strings.Contains(sb.Content, "## Available Skills") || !strings.Contains(sb.Content, "Skill Preflight") {
		t.Fatalf("sandbox prompt missing exposed skill guidance:\n%s", sb.Content)
	}
}

func withProfile(in PromptInput, profile Profile) PromptInput {
	in.Profile = profile
	return in
}

func TestExplicitProfileModeOverridesAuto(t *testing.T) {
	got := SelectProfile("crea un grafico", "memory", Availability{Swarm: true, Sandbox: true})
	if got != ProfileMemory {
		t.Fatalf("explicit profile = %q, want memory", got)
	}
}

func TestExplicitProfileModeFallsBackWhenRequiredRuntimeUnavailable(t *testing.T) {
	decision := SelectProfileDecision("calcola un CSV", string(ProfileSandboxCompute), Availability{Sandbox: false})
	if decision.Profile != ProfileDefault {
		t.Fatalf("Profile = %q, want default when explicit sandbox profile is unavailable", decision.Profile)
	}
	if !strings.Contains(decision.Reason, "unavailable") {
		t.Fatalf("Reason = %q, want unavailable explanation", decision.Reason)
	}

	if _, err := ToolsForProfile(ProfileSandboxCompute, Availability{Sandbox: false}); err == nil {
		t.Fatal("ToolsForProfile sandbox without sandbox returned nil error")
	}
}

func TestSelectProfileDecisionReportsReasonAndAvailabilityFallback(t *testing.T) {
	decision := SelectProfileDecision("facciamo il punto di tutta la pipeline", ProfileModeAuto, Availability{Swarm: true})
	if decision.Profile != ProfileSwarmResearch {
		t.Fatalf("Profile = %q, want swarm_research", decision.Profile)
	}
	if !strings.Contains(decision.Reason, "matched") || !strings.Contains(decision.Reason, string(ProfileSwarmResearch)) {
		t.Fatalf("Reason = %q, want matched swarm reason", decision.Reason)
	}

	fallback := SelectProfileDecision("facciamo il punto di tutta la pipeline", ProfileModeAuto, Availability{Swarm: false})
	if fallback.Profile != ProfileMemory {
		t.Fatalf("fallback Profile = %q, want memory when swarm unavailable", fallback.Profile)
	}
	if !strings.Contains(fallback.Reason, "unavailable") {
		t.Fatalf("fallback Reason = %q, want unavailable explanation", fallback.Reason)
	}
}

func TestProfileCardsDeclareCapabilityContracts(t *testing.T) {
	cards := ProfileCards()
	for _, profile := range []Profile{ProfileDefault, ProfileMemory, ProfileSwarmResearch, ProfileSandboxCompute, ProfileDocument, ProfileAdminReview} {
		card, ok := cards[profile]
		if !ok {
			t.Fatalf("ProfileCards missing %q", profile)
		}
		if card.Purpose == "" {
			t.Fatalf("profile %q missing purpose", profile)
		}
		if profile != ProfileSwarmResearch && len(card.AllowedTools) == 0 {
			t.Fatalf("profile %q missing allowed tools", profile)
		}
	}
	if cards[ProfileSwarmResearch].Access != AccessReadOnly {
		t.Fatalf("swarm access = %q, want read_only", cards[ProfileSwarmResearch].Access)
	}
	for _, denied := range []string{"write_wiki", "execute_code", "create_docx", "schedule_task"} {
		if !slices.Contains(cards[ProfileSwarmResearch].DeniedTools, denied) {
			t.Fatalf("swarm denied tools missing %q: %+v", denied, cards[ProfileSwarmResearch].DeniedTools)
		}
	}
	if cards[ProfileAdminReview].Access != AccessReviewOnly {
		t.Fatalf("admin_review access = %q, want review_only", cards[ProfileAdminReview].Access)
	}
	if slices.Contains(cards[ProfileAdminReview].AllowedTools, "run_task_now") {
		t.Fatalf("admin_review exposes run_task_now: %+v", cards[ProfileAdminReview].AllowedTools)
	}
}
