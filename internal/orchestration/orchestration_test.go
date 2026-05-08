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
		Toolset:           ToolsetDocument,
	})

	if plan.Version != VersionAuraAgentV1 {
		t.Fatalf("Version = %q", plan.Version)
	}
	for _, want := range []string{ModuleBase, ModuleRuntime, ModuleOverlay, ModuleSkills, ModuleSandbox, ModuleFileGeneration, ModuleSecurity} {
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
	if !strings.Contains(plan.Content, "Tool call examples are attached to each tool definition") {
		t.Fatalf("document prompt missing tool example placement guidance:\n%s", plan.Content)
	}
	if !strings.Contains(plan.Content, "Read a skill when the user names it") || !strings.Contains(plan.Content, "Do not read skills just to satisfy a ritual") {
		t.Fatalf("prompt missing advisory skill-use guidance:\n%s", plan.Content)
	}
}

func TestSelectToolsetAutoRoutesToFourToolsets(t *testing.T) {
	tests := []struct {
		name string
		text string
		want Toolset
	}{
		{name: "broad pipeline audit stays default", text: "audit the whole Aura pipeline and tell me what is missing", want: ToolsetDefault},
		{name: "italian conversation log debug stays default", text: "guarda i log delle conversazioni e dimmi dove ti blocchi", want: ToolsetDefault},
		{name: "memory remember stays default", text: "remember this note in memory for the Aura project", want: ToolsetDefault},
		{name: "sandbox english csv chart", text: "compute a CSV table and chart for the E2E timings", want: ToolsetCompute},
		{name: "sandbox italian csv chart", text: "calcola un CSV con grafico revenue e salva gli artifact", want: ToolsetCompute},
		{name: "document english report", text: "create a report from the documents you have", want: ToolsetDocument},
		{name: "document italian report", text: "crea un documento Word modificabile dal riepilogo della memoria", want: ToolsetDocument},
		{name: "admin english dashboard settings", text: "review the dashboard settings and admin approval queue", want: ToolsetAdmin},
		{name: "admin italian settings", text: "apri le impostazioni dashboard e controlla la coda review admin", want: ToolsetAdmin},
		{name: "default english greeting", text: "hello, how are you?", want: ToolsetDefault},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SelectToolset(tt.text, ToolsetModeAuto, Availability{Swarm: true, Sandbox: true, Proposals: true})
			if got != tt.want {
				t.Fatalf("SelectToolset(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestToolsetsExposeBroadSafeDefaultsAndSpecializedAdditions(t *testing.T) {
	def, err := ToolsForToolset(ToolsetDefault, Availability{Swarm: true, WorkspaceFiles: true})
	if err != nil {
		t.Fatalf("ToolsForToolset default: %v", err)
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
	compute, err := ToolsForToolset(ToolsetCompute, Availability{Sandbox: true, WorkspaceFiles: true})
	if err != nil {
		t.Fatalf("ToolsForToolset compute: %v", err)
	}
	for _, required := range []string{"execute_code", "list_tools", "read_tool", "search_memory", "read_source", "web_fetch"} {
		if !slices.Contains(compute, required) {
			t.Fatalf("compute toolset missing %q: %+v", required, compute)
		}
	}

	doc, err := ToolsForToolset(ToolsetDocument, Availability{Swarm: true, WorkspaceFiles: true})
	if err != nil {
		t.Fatalf("ToolsForToolset document: %v", err)
	}
	for _, required := range []string{"create_docx", "create_xlsx", "create_pdf", "search_memory", "run_aurabot_swarm"} {
		if !slices.Contains(doc, required) {
			t.Fatalf("document toolset missing %q: %+v", required, doc)
		}
	}
	for _, forbidden := range []string{"list_files", "read_file", "search_files", "write_file", "apply_patch", "list_sources", "read_source", "web_search", "web_fetch"} {
		if slices.Contains(doc, forbidden) {
			t.Fatalf("document toolset exposes broad exploration tool %q: %+v", forbidden, doc)
		}
	}

	admin, err := ToolsForToolset(ToolsetAdmin, Availability{Swarm: true})
	if err != nil {
		t.Fatalf("ToolsForToolset admin: %v", err)
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
	without, err := ToolsForToolset(ToolsetDefault, Availability{})
	if err != nil {
		t.Fatalf("ToolsForToolset default: %v", err)
	}
	if slices.Contains(without, "read_file") {
		t.Fatalf("workspace tools exposed while unavailable: %+v", without)
	}

	with, err := ToolsForToolset(ToolsetDefault, Availability{WorkspaceFiles: true})
	if err != nil {
		t.Fatalf("ToolsForToolset default workspace: %v", err)
	}
	for _, required := range []string{"list_files", "read_file", "search_files", "write_file", "apply_patch"} {
		if !slices.Contains(with, required) {
			t.Fatalf("workspace-enabled toolset missing %q: %+v", required, with)
		}
	}
}

func TestExplicitComputeFallsBackWhenSandboxUnavailable(t *testing.T) {
	decision := SelectToolsetDecision("calcola un CSV", "compute", Availability{Sandbox: false})
	if decision.Toolset != ToolsetDefault {
		t.Fatalf("Toolset = %q, want default when explicit compute toolset is unavailable", decision.Toolset)
	}
	if !strings.Contains(decision.Reason, "unavailable") {
		t.Fatalf("Reason = %q, want unavailable explanation", decision.Reason)
	}

	if _, err := ToolsForToolset(ToolsetCompute, Availability{Sandbox: false}); err == nil {
		t.Fatal("ToolsForToolset compute without sandbox returned nil error")
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

	def := ComposePrompt(withToolset(base, ToolsetDefault))
	if !strings.Contains(def.Content, "## Available Skills") || !strings.Contains(def.Content, "Skill Use") {
		t.Fatalf("default prompt missing exposed skill guidance:\n%s", def.Content)
	}

	compute := ComposePrompt(withToolset(base, ToolsetCompute))
	if !strings.Contains(compute.Content, "## Available Skills") || !strings.Contains(compute.Content, "Skill Use") {
		t.Fatalf("compute prompt missing exposed skill guidance:\n%s", compute.Content)
	}
}

func withToolset(in PromptInput, toolset Toolset) PromptInput {
	in.Toolset = toolset
	return in
}
