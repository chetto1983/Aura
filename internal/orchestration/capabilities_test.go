package orchestration

import (
	"slices"
	"strings"
	"testing"
)

func TestAllPlannedCapabilitiesArePresent(t *testing.T) {
	want := []Capability{
		CapabilityMemoryRead,
		CapabilityMemoryWriteReviewed,
		CapabilitySourceExtraction,
		CapabilityDocumentGeneration,
		CapabilitySandboxCompute,
		CapabilitySwarmResearch,
		CapabilityBrowserE2E,
		CapabilityDockerRuntime,
		CapabilitySecurityReview,
		CapabilityReleaseGit,
		CapabilityMCPPlugin,
	}

	got := AllCapabilities()
	if len(got) != len(want) {
		t.Fatalf("AllCapabilities len = %d, want %d: %+v", len(got), len(want), got)
	}
	for _, capability := range want {
		if !slices.Contains(got, capability) {
			t.Fatalf("AllCapabilities missing %q: %+v", capability, got)
		}
	}
}

func TestCapabilityDefinitionsDeclareProfilesToolsAndSkillHints(t *testing.T) {
	for _, capability := range AllCapabilities() {
		def, ok := CapabilityDefinitionFor(capability)
		if !ok {
			t.Fatalf("missing definition for %q", capability)
		}
		if def.Capability != capability {
			t.Fatalf("definition key %q has Capability %q", capability, def.Capability)
		}
		if len(def.Profiles) == 0 {
			t.Fatalf("%q has no profiles", capability)
		}
		if len(def.Tools) == 0 && !def.FutureOnly {
			t.Fatalf("%q has no tools and is not marked future-only", capability)
		}
		if len(def.SkillHints) == 0 {
			t.Fatalf("%q has no skill hints", capability)
		}
	}
}

func TestDocumentGenerationCapabilityMapsToDocumentProfileAndTools(t *testing.T) {
	if got := CapabilityProfileNames(CapabilityDocumentGeneration); !slices.Contains(got, string(ProfileDocument)) {
		t.Fatalf("document_generation profiles = %+v, want document", got)
	}
	for _, tool := range []string{"create_docx", "create_xlsx", "create_pdf"} {
		if got := CapabilityToolNames(CapabilityDocumentGeneration); !slices.Contains(got, tool) {
			t.Fatalf("document_generation tools missing %q: %+v", tool, got)
		}
		capability, ok := CapabilityForTool(tool)
		if !ok || capability != CapabilityDocumentGeneration {
			t.Fatalf("CapabilityForTool(%q) = %q, %v; want document_generation, true", tool, capability, ok)
		}
	}
}

func TestSandboxComputeCapabilityMapsToSandboxProfileAndExecuteCode(t *testing.T) {
	if got := CapabilityProfileNames(CapabilitySandboxCompute); !slices.Contains(got, string(ProfileSandboxCompute)) {
		t.Fatalf("sandbox_compute profiles = %+v, want sandbox_compute", got)
	}
	if got := CapabilityToolNames(CapabilitySandboxCompute); !slices.Contains(got, "execute_code") {
		t.Fatalf("sandbox_compute tools missing execute_code: %+v", got)
	}
	capability, ok := CapabilityForTool("execute_code")
	if !ok || capability != CapabilitySandboxCompute {
		t.Fatalf("CapabilityForTool(execute_code) = %q, %v; want sandbox_compute, true", capability, ok)
	}
}

func TestSwarmResearchCapabilityMapsToSwarmProfileAndTools(t *testing.T) {
	if got := CapabilityProfileNames(CapabilitySwarmResearch); !slices.Contains(got, string(ProfileSwarmResearch)) {
		t.Fatalf("swarm_research profiles = %+v, want swarm_research", got)
	}
	for _, tool := range []string{"run_aurabot_swarm", "read_swarm_result", "list_swarm_tasks"} {
		if got := CapabilityToolNames(CapabilitySwarmResearch); !slices.Contains(got, tool) {
			t.Fatalf("swarm_research tools missing %q: %+v", tool, got)
		}
		capability, ok := CapabilityForTool(tool)
		if !ok || capability != CapabilitySwarmResearch {
			t.Fatalf("CapabilityForTool(%q) = %q, %v; want swarm_research, true", tool, capability, ok)
		}
	}
}

func TestCapabilitiesForToolReturnsAllOverlappingCapabilitiesInPlannedOrder(t *testing.T) {
	for _, tool := range []string{"list_sources", "read_source"} {
		got := CapabilitiesForTool(tool)
		want := []Capability{CapabilityMemoryRead, CapabilitySourceExtraction}
		if !slices.Equal(got, want) {
			t.Fatalf("CapabilitiesForTool(%q) = %+v, want %+v", tool, got, want)
		}

		primary, ok := CapabilityForTool(tool)
		if !ok || primary != want[0] {
			t.Fatalf("CapabilityForTool(%q) = %q, %v; want primary %q, true", tool, primary, ok, want[0])
		}
	}

	for _, tool := range []string{"propose_wiki_change", "propose_skill_change"} {
		got := CapabilitiesForTool(tool)
		want := []Capability{CapabilityMemoryWriteReviewed, CapabilitySecurityReview}
		if !slices.Equal(got, want) {
			t.Fatalf("CapabilitiesForTool(%q) = %+v, want %+v", tool, got, want)
		}

		primary, ok := CapabilityForTool(tool)
		if !ok || primary != want[0] {
			t.Fatalf("CapabilityForTool(%q) = %q, %v; want primary %q, true", tool, primary, ok, want[0])
		}
	}

	got := CapabilitiesForTool("write_wiki")
	want := []Capability{CapabilityMemoryWriteReviewed}
	if !slices.Equal(got, want) {
		t.Fatalf("CapabilitiesForTool(write_wiki) = %+v, want %+v", got, want)
	}

	primary, ok := CapabilityForTool("write_wiki")
	if !ok || primary != CapabilityMemoryWriteReviewed {
		t.Fatalf("CapabilityForTool(write_wiki) = %q, %v; want %q, true", primary, ok, CapabilityMemoryWriteReviewed)
	}
}

func TestFutureOnlyCapabilitiesAreExcludedFromNormalV31Profiles(t *testing.T) {
	futureOnly := []Capability{
		CapabilityBrowserE2E,
		CapabilityDockerRuntime,
		CapabilityReleaseGit,
		CapabilityMCPPlugin,
	}
	normalProfiles := []Profile{
		ProfileDefault,
		ProfileMemory,
		ProfileSwarmResearch,
		ProfileSandboxCompute,
		ProfileDocument,
		ProfileAdminReview,
	}
	for _, capability := range futureOnly {
		def, ok := CapabilityDefinitionFor(capability)
		if !ok {
			t.Fatalf("missing definition for %q", capability)
		}
		if !def.FutureOnly {
			t.Fatalf("%q FutureOnly = false", capability)
		}
		if len(def.Tools) != 0 {
			t.Fatalf("%q exposes v3.1 tools: %+v", capability, def.Tools)
		}
	}
	for _, profile := range normalProfiles {
		got := CapabilitiesForProfile(profile)
		for _, capability := range futureOnly {
			if slices.Contains(got, capability) {
				t.Fatalf("CapabilitiesForProfile(%q) includes future-only %q: %+v", profile, capability, got)
			}
		}
	}
}

func TestCapabilityResultsAreCopies(t *testing.T) {
	all := AllCapabilities()
	all[0] = CapabilityMCPPlugin
	if got := AllCapabilities()[0]; got != CapabilityMemoryRead {
		t.Fatalf("mutating AllCapabilities result changed taxonomy first capability to %q", got)
	}

	def, ok := CapabilityDefinitionFor(CapabilityMemoryRead)
	if !ok {
		t.Fatal("missing memory_read definition")
	}
	def.Profiles[0] = ProfileAdminReview
	def.Tools[0] = "mutated_tool"
	def.SkillHints[0] = "mutated_hint"

	rereadDef, ok := CapabilityDefinitionFor(CapabilityMemoryRead)
	if !ok {
		t.Fatal("missing memory_read definition after mutation")
	}
	if rereadDef.Profiles[0] != ProfileDefault {
		t.Fatalf("mutating CapabilityDefinitionFor Profiles changed taxonomy: %+v", rereadDef.Profiles)
	}
	if rereadDef.Tools[0] != "search_memory" {
		t.Fatalf("mutating CapabilityDefinitionFor Tools changed taxonomy: %+v", rereadDef.Tools)
	}
	if rereadDef.SkillHints[0] != "aura-memory-audit" {
		t.Fatalf("mutating CapabilityDefinitionFor SkillHints changed taxonomy: %+v", rereadDef.SkillHints)
	}

	tools := CapabilityToolNames(CapabilityDocumentGeneration)
	tools[0] = "mutated_tool"
	if got := CapabilityToolNames(CapabilityDocumentGeneration)[0]; got != "create_docx" {
		t.Fatalf("mutating CapabilityToolNames result changed taxonomy first tool to %q", got)
	}

	hints := CapabilitySkillHints(CapabilityDocumentGeneration)
	hints[0] = "mutated_hint"
	if got := CapabilitySkillHints(CapabilityDocumentGeneration)[0]; got != "documents:documents" {
		t.Fatalf("mutating CapabilitySkillHints result changed taxonomy first hint to %q", got)
	}

	profileCapabilities := CapabilitiesForProfile(ProfileDefault)
	profileCapabilities[0] = CapabilityMCPPlugin
	if got := CapabilitiesForProfile(ProfileDefault)[0]; got != CapabilityMemoryRead {
		t.Fatalf("mutating CapabilitiesForProfile result changed taxonomy first capability to %q", got)
	}
}

func TestCapabilityDefinitionsDoNotContainStaleWorkerAliasesOrYamlRefs(t *testing.T) {
	for _, capability := range AllCapabilities() {
		def, ok := CapabilityDefinitionFor(capability)
		if !ok {
			t.Fatalf("missing definition for %q", capability)
		}
		values := []string{string(def.Capability)}
		for _, profile := range def.Profiles {
			values = append(values, string(profile))
		}
		values = append(values, def.Tools...)
		values = append(values, def.SkillHints...)

		joined := strings.ToLower(strings.Join(values, "\n"))
		for _, stale := range []string{"golem", ".yaml"} {
			if strings.Contains(joined, stale) {
				t.Fatalf("%q contains stale reference %q in:\n%s", capability, stale, joined)
			}
		}
	}
}
