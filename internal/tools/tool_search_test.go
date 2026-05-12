package tools

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestToolSearchTool_RequiresRegistry(t *testing.T) {
	if NewToolSearchTool(nil) != nil {
		t.Fatal("NewToolSearchTool(nil) should return nil")
	}
}

func TestToolSearchTool_Definition(t *testing.T) {
	reg := NewRegistry(slog.Default())
	tool := NewToolSearchTool(reg)
	if tool == nil {
		t.Fatal("tool is nil")
	}
	if tool.Name() != "tool_search" {
		t.Fatalf("name: got %q", tool.Name())
	}
	def := tool.Definition()
	if def.Name != "tool_search" {
		t.Fatalf("def.Name: got %q", def.Name)
	}
	if len(def.Examples) == 0 {
		t.Fatal("definition has no examples")
	}
	props, ok := def.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("parameters missing properties")
	}
	if _, ok := props["query"]; !ok {
		t.Fatal("query parameter missing")
	}
}

func TestToolSearchTool_ExecuteRequiresQuery(t *testing.T) {
	reg := NewRegistry(slog.Default())
	tool := NewToolSearchTool(reg)
	_, err := tool.Execute(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error when query is missing")
	}
	if !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("error message: %v", err)
	}
}

func TestToolSearchTool_ExecuteReturnsHits(t *testing.T) {
	reg := NewRegistry(slog.Default())
	// Register a couple of fake tools so Search has something to match.
	reg.Register(&fakeDescribedTool{name: "create_docx", desc: "Generate a Word document from structured blocks"})
	reg.Register(&fakeDescribedTool{name: "web_search", desc: "Search the web for current information"})
	reg.Register(&fakeDescribedTool{name: "task", desc: "Manage scheduled tasks (schedule/list/cancel/run_now)"})

	tool := NewToolSearchTool(reg)
	out, err := tool.Execute(context.Background(), map[string]any{"query": "word document", "limit": 3})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var parsed struct {
		Query string `json:"query"`
		Hits  []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"hits"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not JSON: %v\noutput: %s", err, out)
	}
	if parsed.Query != "word document" {
		t.Fatalf("query echo: got %q", parsed.Query)
	}
	if len(parsed.Hits) == 0 {
		t.Fatalf("expected at least 1 hit, got 0 — output: %s", out)
	}
	// Verify create_docx is in results since query matches its description.
	found := false
	for _, h := range parsed.Hits {
		if h.Name == "create_docx" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("create_docx not in hits for 'word document' query: %s", out)
	}
}

func TestToolSearchTool_NoMatchReturnsHelpfulMessage(t *testing.T) {
	reg := NewRegistry(slog.Default())
	reg.Register(&fakeDescribedTool{name: "task", desc: "Manage scheduled tasks"})
	tool := NewToolSearchTool(reg)

	out, err := tool.Execute(context.Background(), map[string]any{"query": "xyzzy123nothingmatches"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out, "No tools matched") {
		t.Fatalf("expected 'no tools matched' guidance, got: %s", out)
	}
}

// fakeDescribedTool is a minimal Tool implementation for these tests.
// It mirrors the one used by registry_test.go but lives here so we don't
// reach across files for fixtures.
type fakeDescribedTool struct {
	name string
	desc string
}

func (f *fakeDescribedTool) Name() string        { return f.name }
func (f *fakeDescribedTool) Description() string { return f.desc }
func (f *fakeDescribedTool) Parameters() map[string]any {
	return map[string]any{"type": "object"}
}
func (f *fakeDescribedTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	return "ok", nil
}
