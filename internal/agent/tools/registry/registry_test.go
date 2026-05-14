package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/aura/aura/internal/storage/search"
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

func TestArgKeysRedactsCredentialShapes(t *testing.T) {
	got := argKeys(map[string]any{"name": "x", "api_key": "y", "auth_token": "z", "user_password": "p"})
	for _, key := range got {
		if key == "api_key" || key == "auth_token" || key == "user_password" {
			t.Fatalf("argKeys leaked credential-shaped key %q in %#v", key, got)
		}
	}
}

type fakeDefinedTool struct{ fakeTool }

func (fakeDefinedTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "fake",
		Description: "FakeDefined tool",
		Examples:    []ToolCallExample{{Description: "set x", Arguments: map[string]any{"x": 1}}},
	}
}

func TestWithCategoryPreservesDefinitionProvider(t *testing.T) {
	wrapped := WithCategory(fakeDefinedTool{}, "test_category")
	provider, ok := wrapped.(ToolDefinitionProvider)
	if !ok {
		t.Fatal("WithCategory must preserve ToolDefinitionProvider")
	}
	def := provider.Definition()
	if def.Name != "fake" || len(def.Examples) != 1 || def.Examples[0].Description != "set x" {
		t.Fatalf("wrapped Definition lost examples: %+v", def)
	}
	multi, ok := wrapped.(MultiCategorizedTool)
	if !ok {
		t.Fatal("WithCategory must expose Categories()")
	}
	cats := multi.Categories()
	if len(cats) != 1 || cats[0] != "test_category" {
		t.Fatalf("Categories() = %v, want [test_category]", cats)
	}
}

func TestRegistryRegisterCollisionReturnsTrue(t *testing.T) {
	reg := NewRegistry(nil)
	if existed := reg.Register(fakeTool{}); existed {
		t.Fatal("first Register should not report existing")
	}
	if existed := reg.Register(fakeTool{}); !existed {
		t.Fatal("second Register should report existing")
	}
}

func TestRegistryExecuteMissingTool(t *testing.T) {
	reg := NewRegistry(nil)
	if _, err := reg.Execute(context.Background(), "missing", nil); err == nil {
		t.Fatal("expected missing tool error")
	}
}

func TestRegistryNamesSorted(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(namedFakeTool{name: "zeta"})
	reg.Register(namedFakeTool{name: "alpha"})
	reg.Register(namedFakeTool{name: "mcp_mail_imap_search_messages"})

	got := reg.Names()
	want := []string{"alpha", "mcp_mail_imap_search_messages", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("Names() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names() = %#v, want %#v", got, want)
		}
	}
}

func TestRegistryNamesByCategorySorted(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(categorizedFakeTool{namedFakeTool: namedFakeTool{name: "zeta"}, category: CategoryMCP})
	reg.Register(namedFakeTool{name: "plain"})
	reg.Register(categorizedFakeTool{namedFakeTool: namedFakeTool{name: "alpha"}, category: CategoryMCP})
	reg.Register(categorizedFakeTool{namedFakeTool: namedFakeTool{name: "other"}, category: "other"})

	got := reg.NamesByCategory(CategoryMCP)
	want := []string{"alpha", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("NamesByCategory() = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NamesByCategory() = %#v, want %#v", got, want)
		}
	}
}

func TestWithCategoryPreservesExistingCategories(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(WithCategory(categorizedFakeTool{
		namedFakeTool: namedFakeTool{name: "mcp_mail_imap_search_messages"},
		category:      CategoryMCP,
	}, CategoryAutonomous))

	if got := reg.NamesByCategory(CategoryMCP); len(got) != 1 || got[0] != "mcp_mail_imap_search_messages" {
		t.Fatalf("MCP category names = %#v", got)
	}
	if got := reg.NamesByCategory(CategoryAutonomous); len(got) != 1 || got[0] != "mcp_mail_imap_search_messages" {
		t.Fatalf("autonomous category names = %#v", got)
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

func TestRegistrySearchFindsToolsByCapability(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(definitionFakeTool{})
	reg.Register(WithCategory(namedDescribedTool{
		name:        "execute_shell",
		description: "Execute shell commands in the Aura container with git rg jq sqlite diagnostics.",
	}, CategoryAutonomous))
	reg.Register(namedDescribedTool{
		name:        "mcp_mail_imap_search_messages",
		description: "Search and read email messages from configured mailboxes.",
	})

	got := reg.Search("container shell command", 5)
	if len(got) == 0 {
		t.Fatal("Search returned no tools")
	}
	if got[0].Name != "execute_shell" {
		t.Fatalf("top result = %q, want execute_shell (all=%+v)", got[0].Name, got)
	}
	if len(got[0].Tags) != 1 || got[0].Tags[0] != CategoryAutonomous {
		t.Fatalf("tags = %+v, want autonomous", got[0].Tags)
	}
}

func TestRegistrySearchClampsLimitAndExcludesTools(t *testing.T) {
	reg := NewRegistry(nil)
	for _, name := range []string{"alpha", "mail_one", "mail_two", "mail_three", "mail_four", "mail_five", "mail_six"} {
		reg.Register(namedDescribedTool{name: name, description: "mail email message search read"})
	}

	got := reg.Search("mail email", 99, "alpha")
	if len(got) != 5 {
		t.Fatalf("Search length = %d, want clamped 5", len(got))
	}
	for _, result := range got {
		if result.Name == "alpha" {
			t.Fatalf("excluded alpha returned: %+v", got)
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

type namedDescribedTool struct {
	name        string
	description string
}

func (t namedDescribedTool) Name() string { return t.name }
func (t namedDescribedTool) Description() string {
	if t.description == "" {
		return "Fake tool"
	}
	return t.description
}
func (t namedDescribedTool) Parameters() map[string]any {
	return map[string]any{"type": "object"}
}
func (t namedDescribedTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return t.name, nil
}

type categorizedFakeTool struct {
	namedFakeTool
	category string
}

func (t categorizedFakeTool) Category() string { return t.category }

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

func TestRegistrySetVectorIndex(t *testing.T) {
	reg := NewRegistry(nil)
	if h := reg.ToolVectorHealth(); h.Backend != "fts" {
		t.Fatalf("nil vector index health = %q, want fts", h.Backend)
	}

	idx := NewToolVectorIndex(ToolVectorConfig{Backend: "hybrid", EmbedModel: "test-model"}, nil)
	reg.SetVectorIndex(idx)

	h := reg.ToolVectorHealth()
	if h.Backend != "hybrid" {
		t.Fatalf("vector index health backend = %q, want hybrid", h.Backend)
	}
	if h.EmbedModel != "test-model" {
		t.Fatalf("vector index health embed model = %q, want test-model", h.EmbedModel)
	}
}

func TestNilRegistryToolVectorHealth(t *testing.T) {
	var reg *Registry
	h := reg.ToolVectorHealth()
	if h.Backend != "fts" || !h.Fallback {
		t.Fatalf("nil registry health = %+v, want fts/fallback", h)
	}
}

func TestNilRegistrySetVectorIndex(t *testing.T) {
	var reg *Registry
	idx := NewToolVectorIndex(ToolVectorConfig{Backend: "hybrid"}, nil)
	reg.SetVectorIndex(idx) // must not panic
}

func TestSearchIncludesVectorResultsWhenAvailable(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(namedDescribedTool{name: "mcp_mail", description: "read and search email messages"})
	reg.Register(namedDescribedTool{name: "execute_code", description: "run python scripts"})

	// Without vector index: lexical only.
	got := reg.Search("email read", 5)
	if len(got) == 0 {
		t.Fatal("lexical search returned no results")
	}

	// With a vector index (fts backend, which no-ops): still lexical only, works fine.
	idx := NewToolVectorIndex(ToolVectorConfig{Backend: "fts"}, nil)
	reg.SetVectorIndex(idx)
	got2 := reg.Search("email read", 5)
	if len(got2) != len(got) {
		t.Fatalf("fts vector index altered results: %d vs %d", len(got2), len(got))
	}
}

func TestSearchWithNilRegistry(t *testing.T) {
	var reg *Registry
	if got := reg.Search("test", 5); got != nil {
		t.Fatalf("nil registry Search = %v, want nil", got)
	}
}
