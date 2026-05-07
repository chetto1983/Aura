package orchestration

// Capability names the coarse orchestration capability family behind a tool
// profile. Capabilities are policy inputs; enforcement is added in later slices.
type Capability string

const (
	CapabilityMemoryRead          Capability = "memory_read"
	CapabilityMemoryWriteReviewed Capability = "memory_write_reviewed"
	CapabilitySourceExtraction    Capability = "source_extraction"
	CapabilityDocumentGeneration  Capability = "document_generation"
	CapabilitySandboxCompute      Capability = "sandbox_compute"
	CapabilitySwarmResearch       Capability = "swarm_research"
	CapabilityBrowserE2E          Capability = "browser_e2e"
	CapabilityDockerRuntime       Capability = "docker_runtime"
	CapabilitySecurityReview      Capability = "security_review"
	CapabilityReleaseGit          Capability = "release_git"
	CapabilityMCPPlugin           Capability = "mcp_plugin"
)

// CapabilityDefinition is the declarative bridge from capability families to
// current profiles, current tools, and future skill-preflight policy hints.
type CapabilityDefinition struct {
	Capability Capability
	Profiles   []Profile
	Tools      []string
	SkillHints []string
	FutureOnly bool
}

const profilePluginReview Profile = "plugin_review"

var plannedCapabilities = []Capability{
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

var capabilityDefinitions = map[Capability]CapabilityDefinition{
	CapabilityMemoryRead: {
		Capability: CapabilityMemoryRead,
		Profiles:   []Profile{ProfileDefault, ProfileMemory, ProfileSwarmResearch, ProfileDocument},
		Tools: []string{
			"search_memory", "search_wiki", "read_wiki", "list_wiki",
			"list_sources", "read_source", "daily_briefing",
		},
		SkillHints: []string{"aura-memory-audit", "memory read evidence"},
	},
	CapabilityMemoryWriteReviewed: {
		Capability: CapabilityMemoryWriteReviewed,
		Profiles:   []Profile{ProfileDefault, ProfileMemory, ProfileAdminReview},
		Tools:      []string{"write_wiki", "propose_wiki_change", "propose_skill_change"},
		SkillHints: []string{"aura-memory-audit", "durable memory write", "review-gated memory write"},
	},
	CapabilitySourceExtraction: {
		Capability: CapabilitySourceExtraction,
		Profiles:   []Profile{ProfileDocument, ProfileSandboxCompute, ProfileAdminReview},
		Tools:      []string{"list_sources", "read_source", "store_source"},
		SkillHints: []string{"aura-source-extraction", "source extraction", "OCR"},
	},
	CapabilityDocumentGeneration: {
		Capability: CapabilityDocumentGeneration,
		Profiles:   []Profile{ProfileDocument},
		Tools:      []string{"create_docx", "create_xlsx", "create_pdf"},
		SkillHints: []string{"documents:documents", "docx", "document-pdf", "xlsx"},
	},
	CapabilitySandboxCompute: {
		Capability: CapabilitySandboxCompute,
		Profiles:   []Profile{ProfileSandboxCompute},
		Tools:      []string{"execute_code"},
		SkillHints: []string{"systematic-debugging", "test-driven-development", "sandbox compute"},
	},
	CapabilitySwarmResearch: {
		Capability: CapabilitySwarmResearch,
		Profiles:   []Profile{ProfileSwarmResearch},
		Tools:      []string{"run_aurabot_swarm", "read_swarm_result", "list_swarm_tasks"},
		SkillHints: []string{"subagent-driven-development", "swarm research", "bounded delegation"},
	},
	CapabilityBrowserE2E: {
		Capability: CapabilityBrowserE2E,
		Profiles:   []Profile{ProfileAdminReview},
		SkillHints: []string{"browser-use", "aura-dashboard-e2e"},
		FutureOnly: true,
	},
	CapabilityDockerRuntime: {
		Capability: CapabilityDockerRuntime,
		Profiles:   []Profile{ProfileAdminReview},
		SkillHints: []string{"docker-compose-orchestration", "aura-release-docker"},
		FutureOnly: true,
	},
	CapabilitySecurityReview: {
		Capability: CapabilitySecurityReview,
		Profiles:   []Profile{ProfileSwarmResearch, ProfileAdminReview},
		Tools:      []string{"list_skills", "read_skill", "search_skill_catalog", "propose_wiki_change", "propose_skill_change"},
		SkillHints: []string{"codex-security:security-scan", "security review", "MCP security"},
	},
	CapabilityReleaseGit: {
		Capability: CapabilityReleaseGit,
		Profiles:   []Profile{ProfileAdminReview},
		SkillHints: []string{"github:yeet", "aura-release-docker", "release git"},
		FutureOnly: true,
	},
	CapabilityMCPPlugin: {
		Capability: CapabilityMCPPlugin,
		Profiles:   []Profile{profilePluginReview},
		SkillHints: []string{"aura-mcp-plugin-review", "MCP plugin review"},
		FutureOnly: true,
	},
}

// AllCapabilities returns the complete planned capability list in stable order.
func AllCapabilities() []Capability {
	return append([]Capability(nil), plannedCapabilities...)
}

// CapabilityDefinitionFor returns a copy of the definition for capability.
func CapabilityDefinitionFor(capability Capability) (CapabilityDefinition, bool) {
	def, ok := capabilityDefinitions[capability]
	if !ok {
		return CapabilityDefinition{}, false
	}
	return cloneCapabilityDefinition(def), true
}

// CapabilityProfileNames returns profile names for capability in stable order.
func CapabilityProfileNames(capability Capability) []string {
	def, ok := CapabilityDefinitionFor(capability)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(def.Profiles))
	for _, profile := range def.Profiles {
		names = append(names, string(profile))
	}
	return names
}

// CapabilityToolNames returns tool names for capability in stable order.
func CapabilityToolNames(capability Capability) []string {
	def, ok := CapabilityDefinitionFor(capability)
	if !ok {
		return nil
	}
	return append([]string(nil), def.Tools...)
}

// CapabilitySkillHints returns skill names or capability descriptors that can
// guide later skill preflight without enforcing it yet.
func CapabilitySkillHints(capability Capability) []string {
	def, ok := CapabilityDefinitionFor(capability)
	if !ok {
		return nil
	}
	return append([]string(nil), def.SkillHints...)
}

// CapabilitiesForTool returns all capabilities that declare tool in stable
// planned capability order.
func CapabilitiesForTool(tool string) []Capability {
	capabilities := make([]Capability, 0)
	for _, capability := range plannedCapabilities {
		def := capabilityDefinitions[capability]
		for _, name := range def.Tools {
			if name == tool {
				capabilities = append(capabilities, capability)
				break
			}
		}
	}
	return capabilities
}

// CapabilityForTool returns the first/primary capability that declares tool.
func CapabilityForTool(tool string) (Capability, bool) {
	capabilities := CapabilitiesForTool(tool)
	if len(capabilities) == 0 {
		return "", false
	}
	return capabilities[0], true
}

// CapabilitiesForProfile returns non-future capabilities exposed by profile.
func CapabilitiesForProfile(profile Profile) []Capability {
	if profile == "" {
		profile = ProfileDefault
	}
	capabilities := make([]Capability, 0)
	for _, capability := range plannedCapabilities {
		def := capabilityDefinitions[capability]
		if def.FutureOnly {
			continue
		}
		for _, candidate := range def.Profiles {
			if candidate == profile {
				capabilities = append(capabilities, capability)
				break
			}
		}
	}
	return capabilities
}

func cloneCapabilityDefinition(def CapabilityDefinition) CapabilityDefinition {
	def.Profiles = append([]Profile(nil), def.Profiles...)
	def.Tools = append([]string(nil), def.Tools...)
	def.SkillHints = append([]string(nil), def.SkillHints...)
	return def
}
