package tools_test

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/tools"
)

func TestListToolsTool_NilStore(t *testing.T) {
	tool := tools.NewListToolsTool(nil)
	if tool != nil {
		t.Fatal("expected nil tool when store is nil")
	}
}

func TestReadToolTool_NilStore(t *testing.T) {
	tool := tools.NewReadToolTool(nil)
	if tool != nil {
		t.Fatal("expected nil tool when store is nil")
	}
}

func TestSaveToolTool_NilStore(t *testing.T) {
	tool := tools.NewSaveToolTool(nil)
	if tool != nil {
		t.Fatal("expected nil tool when store is nil")
	}
}

func TestToolManagement_Parameters(t *testing.T) {
	reg := &mockToolStore{}
	for _, tool := range []tools.Tool{
		tools.NewListToolsTool(reg),
		tools.NewReadToolTool(reg),
		tools.NewSaveToolTool(reg),
	} {
		if tool.Name() == "" {
			t.Fatal("tool must have a name")
		}
		if tool.Description() == "" {
			t.Fatalf("%s: description must not be empty", tool.Name())
		}
		params := tool.Parameters()
		if params["type"] != "object" {
			t.Fatalf("%s: parameters must be JSON Schema object", tool.Name())
		}
	}
}

func TestListToolsTool_Empty(t *testing.T) {
	reg := &mockToolStore{}
	tool := tools.NewListToolsTool(reg)
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "No tools registered yet." {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestListToolsTool_WithTools(t *testing.T) {
	reg := &mockToolStore{
		tools: []tools.ToolInfo{
			{Name: "csv_cleaner", Description: "Cleans CSV files", Params: "filepath (str)"},
			{Name: "chart_line", Description: "Creates line charts", Params: "data (list), title (str)"},
		},
	}
	tool := tools.NewListToolsTool(reg)
	result, err := tool.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "No tools registered yet." {
		t.Fatal("expected tools in output")
	}
}

func TestReadToolTool(t *testing.T) {
	reg := &mockToolStore{code: "def run(): return 42"}
	tool := tools.NewReadToolTool(reg)
	result, err := tool.Execute(context.Background(), map[string]any{"name": "test_tool"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "def run(): return 42" {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestSaveToolTool(t *testing.T) {
	reg := &mockToolStore{}
	tool := tools.NewSaveToolTool(reg)
	result, err := tool.Execute(context.Background(), map[string]any{
		"name":        "test_tool",
		"description": "A test tool",
		"code":        "def run(): return 42",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Tool 'test_tool' saved to registry." {
		t.Fatalf("unexpected result: %s", result)
	}
}

type mockToolStore struct {
	tools []tools.ToolInfo
	code  string
}

func (m *mockToolStore) ListTools() ([]tools.ToolInfo, error)    { return m.tools, nil }
func (m *mockToolStore) GetToolCode(name string) (string, error) { return m.code, nil }
func (m *mockToolStore) SaveTool(ctx context.Context, name, desc, params, code, usage string) error {
	return nil
}
