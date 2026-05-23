package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fakeToolCatalog implements toolCatalogReader for tool_search tests.
type fakeToolCatalog struct {
	defs []ToolDefinition
}

func (f *fakeToolCatalog) FullDefinitions() []ToolDefinition {
	return append([]ToolDefinition(nil), f.defs...)
}

func newTestToolSearchCatalog() *fakeToolCatalog {
	return &fakeToolCatalog{defs: []ToolDefinition{
		{
			Name:        "execute_code",
			Description: "Execute Python code in the sandbox. Runs arbitrary code.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"code": map[string]any{"type": "string", "description": "Python code to execute."},
				},
				"required": []string{"code"},
			},
			VisibilityTier: VisibilityDeferred,
		},
		{
			Name:        "execute_shell",
			Description: "Execute shell commands in the runtime environment.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string"},
				},
				"required": []string{"command"},
			},
			VisibilityTier: VisibilityDeferred,
		},
		{
			Name:           "search_memory",
			Description:    "Search Aura persistent memory for wiki pages and sources.",
			Parameters:     map[string]any{"type": "object"},
			VisibilityTier: VisibilityActiveTurn,
		},
	}}
}

func parseToolSearchResp(t *testing.T, result string) toolSearchResponse {
	t.Helper()
	var resp toolSearchResponse
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("failed to parse result as JSON: %v\nresult: %s", err, result)
	}
	return resp
}

// TestToolSearch_SelectExact verifies AC: select: returns tool with full schema.
func TestToolSearch_SelectExact(t *testing.T) {
	tool := NewToolSearchTool(newTestToolSearchCatalog())
	result, err := tool.Execute(context.Background(), map[string]any{"query": "select:execute_code"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseToolSearchResp(t, result)
	if len(resp.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d: %s", len(resp.Tools), result)
	}
	if resp.Tools[0].Name != "execute_code" {
		t.Errorf("expected execute_code, got %q", resp.Tools[0].Name)
	}
	if resp.Tools[0].Parameters == nil {
		t.Fatal("parameters must not be nil for execute_code")
	}
	props, _ := resp.Tools[0].Parameters["properties"].(map[string]any)
	if _, ok := props["code"]; !ok {
		t.Errorf("execute_code schema missing 'code' property: %v", resp.Tools[0].Parameters)
	}
}

// TestToolSearch_KeywordRanking verifies AC: execute_code ranks higher than
// execute_shell for query "code execute" (name match wins on "code").
func TestToolSearch_KeywordRanking(t *testing.T) {
	tool := NewToolSearchTool(newTestToolSearchCatalog())
	result, err := tool.Execute(context.Background(), map[string]any{"query": "code execute"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseToolSearchResp(t, result)
	if len(resp.Tools) < 2 {
		t.Fatalf("expected ≥2 results, got %d: %s", len(resp.Tools), result)
	}
	if resp.Tools[0].Name != "execute_code" {
		t.Errorf("expected execute_code as top result (name match wins on 'code'), got %q", resp.Tools[0].Name)
	}
	var foundShell bool
	for _, e := range resp.Tools {
		if e.Name == "execute_shell" {
			foundShell = true
		}
	}
	if !foundShell {
		t.Errorf("execute_shell missing from results: %+v", resp.Tools)
	}
}

// TestToolSearch_SelectMissing verifies AC: missing names → clear error 'no tools matched'.
func TestToolSearch_SelectMissing(t *testing.T) {
	tool := NewToolSearchTool(newTestToolSearchCatalog())
	result, err := tool.Execute(context.Background(), map[string]any{"query": "select:nonexistent_tool"})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !strings.Contains(result, "no tools matched") {
		t.Errorf("expected 'no tools matched' in result, got: %s", result)
	}
}

// TestToolSearch_RequiredTerm verifies +term filters to tools containing the term.
func TestToolSearch_RequiredTerm(t *testing.T) {
	tool := NewToolSearchTool(newTestToolSearchCatalog())
	result, err := tool.Execute(context.Background(), map[string]any{"query": "+shell"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseToolSearchResp(t, result)
	if len(resp.Tools) == 0 {
		t.Fatal("expected at least execute_shell when +shell required")
	}
	for _, entry := range resp.Tools {
		if !strings.Contains(strings.ToLower(entry.Name+" "+entry.Description), "shell") {
			t.Errorf("result %q does not contain required term 'shell'", entry.Name)
		}
	}
	// execute_code must NOT appear (no 'shell' in name or desc)
	for _, entry := range resp.Tools {
		if entry.Name == "execute_code" {
			t.Errorf("execute_code should be filtered out by +shell requirement")
		}
	}
}

// TestToolSearch_SelectMultiple verifies select: with comma-separated names.
func TestToolSearch_SelectMultiple(t *testing.T) {
	tool := NewToolSearchTool(newTestToolSearchCatalog())
	result, err := tool.Execute(context.Background(), map[string]any{"query": "select:execute_code,execute_shell"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseToolSearchResp(t, result)
	if len(resp.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d: %s", len(resp.Tools), result)
	}
	names := make(map[string]bool, 2)
	for _, e := range resp.Tools {
		names[e.Name] = true
	}
	for _, want := range []string{"execute_code", "execute_shell"} {
		if !names[want] {
			t.Errorf("missing %q from select result", want)
		}
	}
}

// TestToolSearch_MaxResultsLimitsKeyword caps keyword result count.
func TestToolSearch_MaxResultsLimitsKeyword(t *testing.T) {
	tool := NewToolSearchTool(newTestToolSearchCatalog())
	result, err := tool.Execute(context.Background(), map[string]any{"query": "execute", "max_results": 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseToolSearchResp(t, result)
	if len(resp.Tools) > 1 {
		t.Errorf("expected ≤1 result with max_results=1, got %d", len(resp.Tools))
	}
}

// TestToolSearch_NilCatalogReturnsNil verifies nil catalog → nil constructor.
func TestToolSearch_NilCatalogReturnsNil(t *testing.T) {
	if got := NewToolSearchTool(nil); got != nil {
		t.Errorf("expected nil tool from nil catalog, got %T", got)
	}
}

// TestToolSearch_DefinitionIsAlwaysOn verifies VisibilityTier is VisibilityAlwaysOn.
func TestToolSearch_DefinitionIsAlwaysOn(t *testing.T) {
	tool := NewToolSearchTool(newTestToolSearchCatalog())
	def := tool.Definition()
	if def.VisibilityTier != VisibilityAlwaysOn {
		t.Errorf("expected VisibilityAlwaysOn, got %q", def.VisibilityTier)
	}
	if def.Name != "tool_search" {
		t.Errorf("expected name 'tool_search', got %q", def.Name)
	}
}

// TestToolSearch_DefinitionNotDeferred mirrors AC: ToolSpec.Deferred = false.
func TestToolSearch_DefinitionNotDeferred(t *testing.T) {
	tool := NewToolSearchTool(newTestToolSearchCatalog())
	def := tool.Definition()
	if def.IsDeferred() {
		t.Errorf("tool_search must not be deferred (it IS the entry point to deferred tools)")
	}
}

// TestToolSearch_SelectPartialMissing verifies partial match: found tools returned +
// per-missing error in errors list.
func TestToolSearch_SelectPartialMissing(t *testing.T) {
	tool := NewToolSearchTool(newTestToolSearchCatalog())
	result, err := tool.Execute(context.Background(), map[string]any{"query": "select:execute_code,ghost_tool"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseToolSearchResp(t, result)
	if len(resp.Tools) != 1 || resp.Tools[0].Name != "execute_code" {
		t.Errorf("expected execute_code in found list, got %+v", resp.Tools)
	}
	if len(resp.Errors) == 0 {
		t.Error("expected error entry for missing ghost_tool")
	}
	if !strings.Contains(strings.Join(resp.Errors, " "), "ghost_tool") {
		t.Errorf("expected ghost_tool mentioned in errors: %v", resp.Errors)
	}
}

// TestToolSearch_RequiredPlusOptionalRanking verifies +required with optional term ranks correctly.
func TestToolSearch_RequiredPlusOptionalRanking(t *testing.T) {
	tool := NewToolSearchTool(newTestToolSearchCatalog())
	// +execute requires "execute" in name/desc; "shell" is optional for ranking
	result, err := tool.Execute(context.Background(), map[string]any{"query": "+execute shell"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp := parseToolSearchResp(t, result)
	if len(resp.Tools) == 0 {
		t.Fatal("expected results for +execute shell")
	}
	// execute_shell should rank first because "shell" appears in its name
	if resp.Tools[0].Name != "execute_shell" {
		t.Errorf("expected execute_shell first (has 'shell' in name), got %q", resp.Tools[0].Name)
	}
	// search_memory must not appear (no "execute" in name/desc)
	for _, e := range resp.Tools {
		if e.Name == "search_memory" {
			t.Errorf("search_memory must be filtered by +execute requirement")
		}
	}
}

// TestToolSearch_EmptyQueryErrors verifies empty query returns error.
func TestToolSearch_EmptyQueryErrors(t *testing.T) {
	tool := NewToolSearchTool(newTestToolSearchCatalog())
	_, err := tool.Execute(context.Background(), map[string]any{"query": ""})
	if err == nil {
		t.Error("expected error for empty query")
	}
}
