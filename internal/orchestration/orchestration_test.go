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
		{name: "swarm english pipeline audit", text: "audit the whole Aura pipeline and tell me what is missing", want: ProfileSwarmResearch},
		{name: "swarm italian pipeline audit", text: "facciamo il punto di tutta la pipeline e cosa manca", want: ProfileSwarmResearch},
		{name: "swarm broad memory review", text: "analyze all memory and synthesize the gaps across the knowledge base", want: ProfileSwarmResearch},
		{name: "sandbox english csv chart", text: "compute a CSV table and chart for the E2E timings", want: ProfileSandboxCompute},
		{name: "sandbox italian csv chart", text: "calcola un CSV con grafico revenue e salva gli artifact", want: ProfileSandboxCompute},
		{name: "document english report", text: "create a report from the documents you have", want: ProfileDocument},
		{name: "document italian report", text: "crea un documento Word modificabile dal riepilogo della memoria", want: ProfileDocument},
		{name: "memory english remember", text: "remember this note in memory for the Aura project", want: ProfileMemory},
		{name: "memory capture review gate", text: "auto_low_risk memory capture should keep risky memory review-gated", want: ProfileMemory},
		{name: "memory italian save", text: "salva questa nota nella memoria di Aura", want: ProfileMemory},
		{name: "admin english dashboard settings", text: "review the dashboard settings and admin approval queue", want: ProfileAdminReview},
		{name: "admin italian settings", text: "apri le impostazioni dashboard e controlla la coda review admin", want: ProfileAdminReview},
		{name: "default english greeting", text: "hello, how are you?", want: ProfileDefault},
		{name: "default italian greeting", text: "ciao come va", want: ProfileDefault},
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
	if !slices.Equal(sw[:3], []string{"run_aurabot_swarm", "read_swarm_result", "list_swarm_tasks"}) {
		t.Fatalf("swarm profile should prepend terminal swarm tools, got %+v", sw)
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
	if slices.Index(sb, "list_skills") > slices.Index(sb, "execute_code") || slices.Index(sb, "read_skill") > slices.Index(sb, "execute_code") {
		t.Fatalf("sandbox profile should expose skill preflight tools before execute_code: %+v", sb)
	}
	if slices.Contains(sb, "save_tool") || slices.Contains(sb, "write_wiki") {
		t.Fatalf("sandbox profile exposes admin/write tools: %+v", sb)
	}

	doc, err := ToolsForProfile(ProfileDocument, Availability{Swarm: true})
	if err != nil {
		t.Fatalf("ToolsForProfile document: %v", err)
	}
	for _, required := range []string{"list_skills", "read_skill", "search_memory", "list_sources", "create_docx", "create_xlsx", "create_pdf"} {
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
	for _, forbidden := range []string{
		"execute_code", "run_aurabot_swarm", "install_skill", "delete_skill",
		"settings_update", "request_dashboard_token", "run_task_now",
		"install_plugin", "delete_plugin", "mcp_install", "mcp_register",
	} {
		if slices.Contains(def, forbidden) {
			t.Fatalf("default profile exposes forbidden admin/plugin/execute/swarm tool %q: %+v", forbidden, def)
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

func TestComposePromptDefersCurrentTurnMemoryToPostTurnCapture(t *testing.T) {
	plan := ComposePrompt(PromptInput{
		Version:           VersionAuraAgentV1,
		Now:               time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		Location:          time.UTC,
		ProposalAvailable: true,
		Profile:           ProfileAdminReview,
	})

	for _, want := range []string{
		"automatic post-turn memory capture",
		"Never call write_wiki in this profile",
		"answer normally",
	} {
		if !strings.Contains(plan.Content, want) {
			t.Fatalf("admin review prompt missing %q:\n%s", want, plan.Content)
		}
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

func TestToolsForProfileOnlyExposesCardDeclaredTools(t *testing.T) {
	availabilityCases := []Availability{
		{},
		{Swarm: true},
		{Sandbox: true},
		{Proposals: true},
		{Swarm: true, Sandbox: true, Proposals: true},
	}

	for _, profile := range []Profile{ProfileDefault, ProfileMemory, ProfileSwarmResearch, ProfileSandboxCompute, ProfileDocument, ProfileAdminReview} {
		for _, available := range availabilityCases {
			tools, err := ToolsForProfile(profile, available)
			if err != nil {
				continue
			}
			card, ok := ProfileCardFor(profile)
			if !ok {
				t.Fatalf("ProfileCardFor(%q) returned false", profile)
			}
			declared := declaredToolsForCard(card, available)
			for _, tool := range tools {
				if !slices.Contains(declared, tool) {
					t.Fatalf("%q with availability %+v exposes undeclared tool %q; tools=%+v declared=%+v", profile, available, tool, tools, declared)
				}
			}
		}
	}
}

func TestProfileCardForReturnsCopySafeCards(t *testing.T) {
	card, ok := ProfileCardFor(ProfileSwarmResearch)
	if !ok {
		t.Fatal("ProfileCardFor swarm_research returned false")
	}
	card.PositiveCues[0] = "mutated"
	card.NegativeCues[0] = "mutated"
	card.RequiredAvailability[0] = "mutated"
	card.ConditionalTools[0].Tools[0] = "mutated"
	card.DeniedTools[0] = "mutated"
	card.LoopPolicy.TerminalTools[0] = "mutated"

	reread, ok := ProfileCardFor(ProfileSwarmResearch)
	if !ok {
		t.Fatal("ProfileCardFor swarm_research reread returned false")
	}
	if reread.PositiveCues[0] == "mutated" {
		t.Fatalf("mutating PositiveCues changed profile card catalog: %+v", reread.PositiveCues)
	}
	if reread.NegativeCues[0] == "mutated" {
		t.Fatalf("mutating NegativeCues changed profile card catalog: %+v", reread.NegativeCues)
	}
	if reread.RequiredAvailability[0] == "mutated" {
		t.Fatalf("mutating RequiredAvailability changed profile card catalog: %+v", reread.RequiredAvailability)
	}
	if reread.ConditionalTools[0].Tools[0] == "mutated" {
		t.Fatalf("mutating ConditionalTools changed profile card catalog: %+v", reread.ConditionalTools)
	}
	if reread.DeniedTools[0] == "mutated" {
		t.Fatalf("mutating DeniedTools changed profile card catalog: %+v", reread.DeniedTools)
	}
	if reread.LoopPolicy.TerminalTools[0] == "mutated" {
		t.Fatalf("mutating loop terminal tools changed profile card catalog: %+v", reread.LoopPolicy.TerminalTools)
	}

	cards := ProfileCards()
	cards[ProfileSandboxCompute].AllowedTools[0] = "mutated"
	cards[ProfileSwarmResearch].ConditionalTools[0].Tools[0] = "mutated"
	rereadMap := ProfileCards()
	if rereadMap[ProfileSandboxCompute].AllowedTools[0] == "mutated" {
		t.Fatalf("mutating ProfileCards result changed profile card catalog: %+v", rereadMap[ProfileSandboxCompute].AllowedTools)
	}
	if rereadMap[ProfileSwarmResearch].ConditionalTools[0].Tools[0] == "mutated" {
		t.Fatalf("mutating ProfileCards conditional tools changed profile card catalog: %+v", rereadMap[ProfileSwarmResearch].ConditionalTools)
	}
}

func declaredToolsForCard(card ProfileCard, available Availability) []string {
	tools := append([]string(nil), card.AllowedTools...)
	for _, set := range card.ConditionalTools {
		if !testAvailabilityEnabled(set.Availability, available) {
			continue
		}
		if set.Prepend {
			tools = append(append([]string(nil), set.Tools...), tools...)
			continue
		}
		tools = append(tools, set.Tools...)
	}
	return tools
}

func testAvailabilityEnabled(name string, available Availability) bool {
	switch name {
	case "swarm":
		return available.Swarm
	case "sandbox":
		return available.Sandbox
	case "proposals":
		return available.Proposals
	default:
		return false
	}
}
