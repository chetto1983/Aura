package orchestration

// Capability names the coarse orchestration capability family behind a
// toolset. Capabilities are policy inputs; enforcement happens at tool
// boundaries.
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
// current toolsets, current tools, and future skill-preflight policy hints.
type CapabilityDefinition struct {
	Capability Capability
	Toolsets   []Profile
	Tools      []string
	SkillHints []string
	FutureOnly bool
}

const toolsetPluginReview Profile = "plugin_review"

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
		Toolsets:   []Profile{ProfileDefault, ProfileDocument, ProfileAdmin},
		Tools: []string{
			"search_memory", "list_files", "read_file", "search_files",
			"list_sources", "read_source", "daily_briefing",
		},
		SkillHints: []string{"aura-memory-audit", "memory read evidence"},
	},
	CapabilityMemoryWriteReviewed: {
		Capability: CapabilityMemoryWriteReviewed,
		Toolsets:   []Profile{ProfileDefault, ProfileAdmin},
		Tools:      []string{"write_file", "apply_patch"},
		SkillHints: []string{"aura-memory-audit", "durable memory write", "workspace markdown edit"},
	},
	CapabilitySourceExtraction: {
		Capability: CapabilitySourceExtraction,
		Toolsets:   []Profile{ProfileDefault, ProfileCompute, ProfileDocument, ProfileAdmin},
		Tools:      []string{"list_sources", "read_source", "store_source"},
		SkillHints: []string{"aura-source-extraction", "source extraction", "OCR"},
	},
	CapabilityDocumentGeneration: {
		Capability: CapabilityDocumentGeneration,
		Toolsets:   []Profile{ProfileDocument},
		Tools:      []string{"create_docx", "create_xlsx", "create_pdf"},
		SkillHints: []string{"documents:documents", "docx", "document-pdf", "xlsx"},
	},
	CapabilitySandboxCompute: {
		Capability: CapabilitySandboxCompute,
		Toolsets:   []Profile{ProfileCompute},
		Tools:      []string{"execute_code"},
		SkillHints: []string{"aura-python-sandbox", "systematic-debugging", "test-driven-development", "sandbox compute"},
	},
	CapabilitySwarmResearch: {
		Capability: CapabilitySwarmResearch,
		Toolsets:   []Profile{ProfileDefault, ProfileDocument, ProfileAdmin},
		Tools:      []string{"run_aurabot_swarm", "read_swarm_result", "list_swarm_tasks"},
		SkillHints: []string{"subagent-driven-development", "swarm research", "bounded delegation"},
	},
	CapabilityBrowserE2E: {
		Capability: CapabilityBrowserE2E,
		Toolsets:   []Profile{ProfileAdmin},
		SkillHints: []string{"browser-use", "aura-dashboard-e2e"},
		FutureOnly: true,
	},
	CapabilityDockerRuntime: {
		Capability: CapabilityDockerRuntime,
		Toolsets:   []Profile{ProfileAdmin},
		SkillHints: []string{"docker-compose-orchestration", "aura-release-docker"},
		FutureOnly: true,
	},
	CapabilitySecurityReview: {
		Capability: CapabilitySecurityReview,
		Toolsets:   []Profile{ProfileDefault, ProfileAdmin},
		Tools:      []string{"list_files", "read_file", "search_files", "search_memory"},
		SkillHints: []string{"codex-security:security-scan", "security review", "MCP security"},
	},
	CapabilityReleaseGit: {
		Capability: CapabilityReleaseGit,
		Toolsets:   []Profile{ProfileAdmin},
		SkillHints: []string{"github:yeet", "aura-release-docker", "release git"},
		FutureOnly: true,
	},
	CapabilityMCPPlugin: {
		Capability: CapabilityMCPPlugin,
		Toolsets:   []Profile{toolsetPluginReview},
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

// CapabilityToolsetNames returns toolset names for capability in stable order.
func CapabilityToolsetNames(capability Capability) []string {
	def, ok := CapabilityDefinitionFor(capability)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(def.Toolsets))
	for _, toolset := range def.Toolsets {
		names = append(names, string(toolset))
	}
	return names
}

// CapabilityProfileNames is a compatibility wrapper for older tests/debug code.
func CapabilityProfileNames(capability Capability) []string {
	return CapabilityToolsetNames(capability)
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

// CapabilitiesForToolset returns non-future capabilities exposed by toolset.
func CapabilitiesForToolset(toolset Profile) []Capability {
	toolset = normalizeProfile(string(toolset))
	if toolset == "" {
		toolset = ProfileDefault
	}
	capabilities := make([]Capability, 0)
	for _, capability := range plannedCapabilities {
		def := capabilityDefinitions[capability]
		if def.FutureOnly {
			continue
		}
		for _, candidate := range def.Toolsets {
			if normalizeProfile(string(candidate)) == toolset {
				capabilities = append(capabilities, capability)
				break
			}
		}
	}
	return capabilities
}

// CapabilitiesForProfile is a compatibility wrapper while callers migrate to
// toolset terminology.
func CapabilitiesForProfile(profile Profile) []Capability {
	return CapabilitiesForToolset(profile)
}

func cloneCapabilityDefinition(def CapabilityDefinition) CapabilityDefinition {
	def.Toolsets = append([]Profile(nil), def.Toolsets...)
	def.Tools = append([]string(nil), def.Tools...)
	def.SkillHints = append([]string(nil), def.SkillHints...)
	return def
}
