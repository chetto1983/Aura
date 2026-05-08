package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/aura/aura/internal/search"
)

type fakeTool struct{}

func (fakeTool) Name() string        { return "fake" }
func (fakeTool) Description() string { return "Fake tool" }
func (fakeTool) Parameters() map[string]any {
	return map[string]any{"type": "object"}
}
func (fakeTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return "ok", nil
}

func TestRegistryDefinitionsAndExecute(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(fakeTool{})

	defs := reg.Definitions()
	if len(defs) != 1 {
		t.Fatalf("Definitions() length = %d, want 1", len(defs))
	}
	if defs[0].Name != "fake" {
		t.Errorf("definition name = %q, want fake", defs[0].Name)
	}
	if !strings.Contains(defs[0].Description, "Tool call examples:") || !strings.Contains(defs[0].Description, "fake(") {
		t.Fatalf("definition missing tool call example: %q", defs[0].Description)
	}
	if reg.Get("fake") == nil {
		t.Fatal("Get(fake) returned nil")
	}

	result, err := reg.Execute(context.Background(), "fake", map[string]any{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result != "ok" {
		t.Errorf("Execute() = %q, want ok", result)
	}
}

func TestArgKeysSorted(t *testing.T) {
	got := argKeys(map[string]any{"z": 1, "a": 2})
	if len(got) != 2 || got[0] != "a" || got[1] != "z" {
		t.Fatalf("argKeys() = %#v, want [a z]", got)
	}
}

func TestRegistryExecuteMissingTool(t *testing.T) {
	reg := NewRegistry(nil)
	if _, err := reg.Execute(context.Background(), "missing", nil); err == nil {
		t.Fatal("expected missing tool error")
	}
}

func TestRegistryDefinitionsForFiltersAndKeepsAllowlistOrder(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(namedFakeTool{name: "alpha"})
	reg.Register(namedFakeTool{name: "beta"})
	reg.Register(namedFakeTool{name: "gamma"})

	defs := reg.DefinitionsFor([]string{"gamma", "missing", "alpha"})
	if len(defs) != 2 {
		t.Fatalf("DefinitionsFor length = %d, want 2: %+v", len(defs), defs)
	}
	if defs[0].Name != "gamma" || defs[1].Name != "alpha" {
		t.Fatalf("DefinitionsFor names = %q,%q; want gamma,alpha", defs[0].Name, defs[1].Name)
	}
	for _, def := range defs {
		if !strings.Contains(def.Description, "Tool call examples:") {
			t.Fatalf("%s missing tool call examples: %q", def.Name, def.Description)
		}
	}
}

func TestRegistryUsesToolDefinitionProvider(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(definitionFakeTool{})

	defs := reg.DefinitionsFor([]string{"definition_fake"})
	if len(defs) != 1 {
		t.Fatalf("DefinitionsFor length = %d, want 1", len(defs))
	}
	def := defs[0]
	if def.Name != "definition_fake" {
		t.Fatalf("Name = %q, want definition_fake", def.Name)
	}
	if !strings.Contains(def.Description, "Definition-owned description") {
		t.Fatalf("definition description not used: %q", def.Description)
	}
	if !strings.Contains(def.Description, `definition_fake(`) || !strings.Contains(def.Description, `"query":"from provider"`) || !strings.Contains(def.Description, `"limit":2`) {
		t.Fatalf("provider example not rendered: %q", def.Description)
	}
	if _, ok := def.Parameters["properties"]; !ok {
		t.Fatalf("provider parameters not used: %#v", def.Parameters)
	}
}

func TestKnownToolDefinitionsIncludeSpecificExamples(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(NewSearchMemoryTool(fakeMemorySearchForExamples{}, nil))
	reg.Register(NewCreateDOCXTool(nil, nil))

	defs := reg.DefinitionsFor([]string{"search_memory", "create_docx"})
	if len(defs) != 2 {
		t.Fatalf("DefinitionsFor length = %d, want 2", len(defs))
	}
	if !strings.Contains(defs[0].Description, `search_memory(`) || !strings.Contains(defs[0].Description, `"query":"documenti e note disponibili in Aura"`) {
		t.Fatalf("search_memory example missing:\n%s", defs[0].Description)
	}
	if !strings.Contains(defs[1].Description, `create_docx(`) || !strings.Contains(defs[1].Description, `"filename":"riepilogo-aura.docx"`) {
		t.Fatalf("create_docx example missing:\n%s", defs[1].Description)
	}
}

type namedFakeTool struct {
	name string
}

func (t namedFakeTool) Name() string        { return t.name }
func (t namedFakeTool) Description() string { return "Fake tool" }
func (t namedFakeTool) Parameters() map[string]any {
	return map[string]any{"type": "object"}
}
func (t namedFakeTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return t.name, nil
}

type definitionFakeTool struct{}

func (definitionFakeTool) Name() string        { return "definition_fake" }
func (definitionFakeTool) Description() string { return "base description" }
func (definitionFakeTool) Parameters() map[string]any {
	return map[string]any{"type": "object"}
}
func (definitionFakeTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "definition_fake",
		Description: "Definition-owned description",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"limit": map[string]any{"type": "integer"},
			},
			"required": []string{"query"},
		},
		Examples: []ToolCallExample{{Arguments: map[string]any{"query": "from provider", "limit": 2}}},
	}
}
func (definitionFakeTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return "ok", nil
}

type fakeMemorySearchForExamples struct{}

func (fakeMemorySearchForExamples) IsIndexed() bool { return true }
func (fakeMemorySearchForExamples) Search(context.Context, string, int) ([]search.Result, error) {
	return nil, nil
}
