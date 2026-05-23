package tools

import (
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/llm"
)

// ToolCallExample is a concrete, model-facing example for one tool call.
type ToolCallExample struct {
	Description string
	Arguments   map[string]any
}

// VisibilityTier controls when a tool appears in the LLM's active tool pool.
// It is Aura-specific — not part of the MCP spec — and drives deferred-discovery
// prompt-budget optimisation.
type VisibilityTier string

const (
	// VisibilityAlwaysOn tools are loaded in every turn (discovery + control surface).
	VisibilityAlwaysOn VisibilityTier = "always_on"
	// VisibilityActiveTurn tools are loaded in every turn but may be trimmed under budget pressure.
	VisibilityActiveTurn VisibilityTier = "active_turn"
	// VisibilityDeferred tools are excluded from the default pool and discovered on-demand.
	VisibilityDeferred VisibilityTier = "deferred"
)

// ToolDefinition is Aura's canonical tool contract. Tools may provide one
// directly, while older tools are adapted from the Tool interface.
type ToolDefinition struct {
	Name               string
	Description        string
	Parameters         map[string]any
	Examples           []ToolCallExample
	RequiredCapability identity.Capability

	// MCP-standard behavioral hints. All default to false (fail-closed: "could
	// write or delete") so unmigrated tools are treated conservatively.
	ReadOnlyHint    bool
	DestructiveHint bool
	IdempotentHint  bool
	OpenWorldHint   bool

	// VisibilityTier controls when this tool appears in the LLM's active pool.
	// Default: VisibilityActiveTurn (set by normalizeToolDefinition).
	VisibilityTier VisibilityTier

	// OutputCap controls byte/row trimming applied at the registry Execute
	// boundary before results reach the agent loop. Zero values resolve to
	// DefaultOutputMaxBytes / DefaultOutputMaxRows. Bypass=true skips the
	// cap entirely (for tools that return paths, not content).
	OutputCap OutputCap
}

// ToolDefinitionProvider lets tools own their LangChain-style definition
// instead of relying on registry-side prompt patches.
type ToolDefinitionProvider interface {
	Definition() ToolDefinition
}

// ToolCapabilityProvider lets a tool require a narrower capability than the
// default broad tool execution grant.
type ToolCapabilityProvider interface {
	RequiredCapability() identity.Capability
}

// ToolAvailabilityProvider lets dynamic tools disappear from the LLM-facing
// catalogue without tearing down their registered wrapper.
type ToolAvailabilityProvider interface {
	Available() bool
}

func toolAvailable(t Tool) bool {
	if provider, ok := t.(ToolAvailabilityProvider); ok {
		return provider.Available()
	}
	return true
}

func definitionForTool(t Tool) ToolDefinition {
	if provider, ok := t.(ToolDefinitionProvider); ok {
		def := provider.Definition()
		return normalizeToolDefinition(t, def)
	}
	return normalizeToolDefinition(t, ToolDefinition{})
}

func normalizeToolDefinition(t Tool, def ToolDefinition) ToolDefinition {
	if def.Name == "" {
		def.Name = t.Name()
	}
	if def.Description == "" {
		def.Description = t.Description()
	}
	if def.Parameters == nil {
		def.Parameters = t.Parameters()
	}
	if len(def.Examples) == 0 {
		def.Examples = examplesForToolName(def.Name, def.Parameters)
	}
	if def.RequiredCapability == "" {
		if provider, ok := t.(ToolCapabilityProvider); ok {
			def.RequiredCapability = provider.RequiredCapability()
		}
	}
	if def.RequiredCapability == "" {
		def.RequiredCapability = identity.CapabilityToolExecute
	}
	if def.VisibilityTier == "" {
		def.VisibilityTier = VisibilityActiveTurn
	}
	return def
}

func requiredCapabilityForTool(t Tool) identity.Capability {
	return definitionForTool(t).RequiredCapability
}

// IsDeferred reports whether this tool is excluded from the default LLM schema
// pool and must be discovered on-demand via the tool_search tool. Equivalent
// to checking VisibilityTier == VisibilityDeferred.
func (d ToolDefinition) IsDeferred() bool {
	return d.VisibilityTier == VisibilityDeferred
}

func (d ToolDefinition) LLMDefinition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        d.Name,
		Description: renderToolDescription(d.Name, d.Description, d.Examples),
		Parameters:  d.Parameters,
	}
}
