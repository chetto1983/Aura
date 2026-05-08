package tools

import "github.com/aura/aura/internal/llm"

// ToolCallExample is a concrete, model-facing example for one tool call.
type ToolCallExample struct {
	Description string
	Arguments   map[string]any
}

// ToolDefinition is Aura's canonical tool contract. Tools may provide one
// directly, while older tools are adapted from the Tool interface.
type ToolDefinition struct {
	Name        string
	Description string
	Parameters  map[string]any
	Examples    []ToolCallExample
}

// ToolDefinitionProvider lets tools own their LangChain-style definition
// instead of relying on registry-side prompt patches.
type ToolDefinitionProvider interface {
	Definition() ToolDefinition
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
	return def
}

func (d ToolDefinition) LLMDefinition() llm.ToolDefinition {
	return llm.ToolDefinition{
		Name:        d.Name,
		Description: renderToolDescription(d.Name, d.Description, d.Examples),
		Parameters:  d.Parameters,
	}
}
