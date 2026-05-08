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
	for _, want := range []string{ModuleBase, ModuleRuntime, ModuleOverlay, ModuleSkills, ModuleSwarm, ModuleSandbox, ModuleFileGeneration, ModuleSecurity} {
		if !slices.Contains(plan.Modules, want) {
			t.Fatalf("modules missing %q: %+v", want, plan.Modules)
		}
	}
	if len(plan.Hash) != 16 {
		t.Fatalf("hash = %q, want 16-char short hash", plan.Hash)
	}
	if !strings.Contains(plan.Content, "Prompt Version: aura-agent-v1") || !strings.Contains(plan.Content, "Toolset: document") {
		t.Fatalf("prompt missing version/toolset marker:\n%s", plan.Content)
	}
	if !strings.Contains(plan.Content, "Read a skill when the user names it") || !strings.Contains(plan.Content, "Do not read skills just to satisfy a ritual") {
		t.Fatalf("prompt missing advisory skill-use guidance:\n%s", plan.Content)
	}
}

func TestSelectProfileAutoRoutesToFourToolsets(t *testing.T) {
	tests := []struct {
		name string
		text string
		want Profile
	}{
		{name: "broad pipeline audit stays default", text: "audit the whole Aura pipeline and tell me what is missing", want: ProfileDefault},
		{name: "italian conversation log debug stays default", text: "guarda i log delle conversazioni e dimmi dove ti blocchi", want: ProfileDefault},
		{name: "memory remember stays default", text: "remember this note in memory for the Aura project", want: ProfileDefault},
		{name: "sandbox english csv chart", text: "compute a CSV table and chart for the E2E timings", want: ProfileCompute},
		{name: "sandbox italian csv chart", text: "calcola un CSV con grafico revenue e salva gli artifact", want: ProfileCompute},
		{name: "document english report", text: "create a report from the documents you have", want: ProfileDocument},
		{name: "document italian report", text: "crea un documento Word modificabile dal riepilogo della memoria", want: ProfileDocument},
		{name: "admin english dashboard settings", text: "review the dashboard settings and admin approval queue", want: ProfileAdmin},
		{name: "admin italian settings", text: "apri le impostazioni dashboard e controlla la coda review admin", want: ProfileAdmin},
		{name: "default english greeting", text: "hello, how are you?", want: ProfileDefault},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectProfile(tt.text, ProfileModeAuto, Availability{Swarm: true, Sandbox: true, Proposals: true})
			if got != tt.want {
				t.Fatalf("SelectProfile(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestToolsetsExposeBroadSafeDefaultsAndSpecializedAdditions(t *testing.T) {
	def, err := ToolsForProfile(ProfileDefault, Availability{Swarm: true, WorkspaceFiles: true})
	if err != nil {
		t.Fatalf("ToolsForProfile default: %v", err)
	}
	for _, required := range []string{"list_files", "read_file", "search_files", "write_file", "apply_patch", "search_memory", "list_sources", "read_source", "web_search", "web_fetch", "run_aurabot_swarm"} {
		if !slices.Contains(def, required) {
			t.Fatalf("default toolset missing %q: %+v", required, def)
		}
	}
	for _, forbidden := range []string{"execute_code", "create_docx", "create_xlsx", "create_pdf", "install_skill", "delete_skill", "request_dashboard_token"} {
		if slices.Contains(def, forbidden) {
			t.Fatalf("default toolset exposes specialized/admin tool %q: %+v", forbidden, def)
		}
	}

	compute, err := ToolsForProfile(ProfileCompute, Availability{Sandbox: true, WorkspaceFiles: true})
	if err != nil {
		t.Fatalf("ToolsForProfile compute: %v", err)
	}
	for _, required := range []string{"execute_code", "list_tools", "read_tool", "search_memory", "read_source", "web_fetch"} {
		if !slices.Contains(compute, required) {
			t.Fatalf("compute toolset missing %q: %+v", required, compute)
		}
	}

	doc, err := ToolsForProfile(ProfileDocument, Availability{Swarm: true, WorkspaceFiles: true})
	if err != nil {
		t.Fatalf("ToolsForProfile document: %v", err)
	}
	for _, required := range []string{"create_docx", "create_xlsx", "create_pdf", "search_memory", "read_source", "run_aurabot_swarm"} {
		if !slices.Contains(doc, required) {
			t.Fatalf("document toolset missing %q: %+v", required, doc)
		}
	}

	admin, err := ToolsForProfile(ProfileAdmin, Availability{Swarm: true})
	if err != nil {
		t.Fatalf("ToolsForProfile admin: %v", err)
	}
	for _, required := range []string{"request_dashboard_token", "install_skill", "delete_skill", "settings_update", "run_task_now", "search_memory"} {
		if !slices.Contains(admin, required) {
			t.Fatalf("admin toolset missing %q: %+v", required, admin)
		}
	}
	if slices.Contains(admin, "execute_code") {
		t.Fatalf("admin toolset should not expose execute_code: %+v", admin)
	}
}

func TestWorkspaceToolsAreConditional(t *testing.T) {
	without, err := ToolsForProfile(ProfileDefault, Availability{})
	if err != nil {
		t.Fatalf("ToolsForProfile default: %v", err)
	}
	if slices.Contains(without, "read_file") {
		t.Fatalf("workspace tools exposed while unavailable: %+v", without)
	}

	with, err := ToolsForProfile(ProfileDefault, Availability{WorkspaceFiles: true})
	if err != nil {
		t.Fatalf("ToolsForProfile default workspace: %v", err)
	}
	for _, required := range []string{"list_files", "read_file", "search_files", "write_file", "apply_patch"} {
		if !slices.Contains(with, required) {
			t.Fatalf("workspace-enabled toolset missing %q: %+v", required, with)
		}
	}
}

func TestLegacyProfileModesNormalizeToToolsets(t *testing.T) {
	tests := []struct {
		mode string
		want Profile
	}{
		{mode: "memory", want: ProfileDefault},
		{mode: "swarm_research", want: ProfileDefault},
		{mode: "sandbox_compute", want: ProfileCompute},
		{mode: "admin_review", want: ProfileAdmin},
		{mode: "compute", want: ProfileCompute},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			got := SelectProfile("hello", tt.mode, Availability{Sandbox: true, Swarm: true})
			if got != tt.want {
				t.Fatalf("SelectProfile legacy mode %q = %q, want %q", tt.mode, got, tt.want)
			}
			if _, err := ToolsForProfile(Profile(tt.mode), Availability{Sandbox: true, Swarm: true}); err != nil {
				t.Fatalf("ToolsForProfile legacy mode %q: %v", tt.mode, err)
			}
		})
	}
}

func TestExplicitComputeFallsBackWhenSandboxUnavailable(t *testing.T) {
	decision := SelectProfileDecision("calcola un CSV", "compute", Availability{Sandbox: false})
	if decision.Profile != ProfileDefault {
		t.Fatalf("Profile = %q, want default when explicit compute toolset is unavailable", decision.Profile)
	}
	if !strings.Contains(decision.Reason, "unavailable") {
		t.Fatalf("Reason = %q, want unavailable explanation", decision.Reason)
	}

	if _, err := ToolsForProfile(ProfileCompute, Availability{Sandbox: false}); err == nil {
		t.Fatal("ToolsForProfile compute without sandbox returned nil error")
	}
}

func TestComposePromptIncludesSkillsForDefaultAndCompute(t *testing.T) {
	base := PromptInput{
		Version:          VersionAuraAgentV1,
		Now:              time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		Location:         time.UTC,
		SkillsBlock:      "## Available Skills\n\n- **docx** - Create Word files",
		SwarmAvailable:   true,
		SandboxAvailable: true,
	}

	def := ComposePrompt(withProfile(base, ProfileDefault))
	if !strings.Contains(def.Content, "## Available Skills") || !strings.Contains(def.Content, "Skill Use") {
		t.Fatalf("default prompt missing exposed skill guidance:\n%s", def.Content)
	}

	compute := ComposePrompt(withProfile(base, ProfileCompute))
	if !strings.Contains(compute.Content, "## Available Skills") || !strings.Contains(compute.Content, "Skill Use") {
		t.Fatalf("compute prompt missing exposed skill guidance:\n%s", compute.Content)
	}
}

func withProfile(in PromptInput, profile Profile) PromptInput {
	in.Profile = profile
	return in
}

func TestProfileCardsDeclareFourToolsets(t *testing.T) {
	cards := ProfileCards()
	for _, profile := range []Profile{ProfileDefault, ProfileCompute, ProfileDocument, ProfileAdmin} {
		card, ok := cards[profile]
		if !ok {
			t.Fatalf("ProfileCards missing %q", profile)
		}
		if card.Purpose == "" || len(card.AllowedTools) == 0 {
			t.Fatalf("toolset %q missing purpose or tools: %+v", profile, card)
		}
	}
	if len(cards) != 4 {
		t.Fatalf("ProfileCards len = %d, want 4: %+v", len(cards), cards)
	}
	if cards[ProfileAdmin].Access != AccessReviewOnly {
		t.Fatalf("admin access = %q, want review_only", cards[ProfileAdmin].Access)
	}
}

func TestToolsForProfileOnlyExposesCardDeclaredTools(t *testing.T) {
	availabilityCases := []Availability{
		{},
		{Swarm: true},
		{Sandbox: true},
		{Proposals: true},
		{WorkspaceFiles: true},
		{Swarm: true, Sandbox: true, Proposals: true},
	}

	for _, profile := range []Profile{ProfileDefault, ProfileCompute, ProfileDocument, ProfileAdmin} {
		for _, available := range availabilityCases {
			tools, err := ToolsForProfile(profile, available)
			if err != nil {
				continue
			}
			card, ok := ProfileCardFor(profile)
			if !ok {
				t.Fatalf("ProfileCardFor(%q) returned false", profile)
			}
			declared := append([]string(nil), card.AllowedTools...)
			for _, set := range card.ConditionalTools {
				declared = append(declared, set.Tools...)
			}
			for _, tool := range tools {
				if !slices.Contains(declared, tool) {
					t.Fatalf("%q with availability %+v exposes undeclared tool %q; tools=%+v declared=%+v", profile, available, tool, tools, declared)
				}
			}
		}
	}
}

func TestProfileCardResultsAreCopySafe(t *testing.T) {
	card, ok := ProfileCardFor(ProfileDefault)
	if !ok {
		t.Fatal("ProfileCardFor default returned false")
	}
	card.PositiveCues = append(card.PositiveCues, "mutated")
	card.NegativeCues = append(card.NegativeCues, "mutated")
	card.ConditionalTools[0].Tools[0] = "mutated"
	card.DeniedTools[0] = "mutated"
	card.LoopPolicy.MaxSteps = 99

	reread, ok := ProfileCardFor(ProfileDefault)
	if !ok {
		t.Fatal("ProfileCardFor default reread returned false")
	}
	if slices.Contains(reread.PositiveCues, "mutated") {
		t.Fatalf("mutating PositiveCues changed profile card catalog: %+v", reread.PositiveCues)
	}
	if slices.Contains(reread.NegativeCues, "mutated") {
		t.Fatalf("mutating NegativeCues changed profile card catalog: %+v", reread.NegativeCues)
	}
	if reread.ConditionalTools[0].Tools[0] == "mutated" {
		t.Fatalf("mutating ConditionalTools changed profile card catalog: %+v", reread.ConditionalTools)
	}
	if reread.DeniedTools[0] == "mutated" {
		t.Fatalf("mutating DeniedTools changed profile card catalog: %+v", reread.DeniedTools)
	}
	if reread.LoopPolicy.MaxSteps == 99 {
		t.Fatalf("mutating loop policy changed profile card catalog: %+v", reread.LoopPolicy)
	}
}
