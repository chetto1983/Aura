package tools_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/agent/tools/registry"
)

func TestDevToolTool_NilStore(t *testing.T) {
	if tools.NewDevToolTool(nil) != nil {
		t.Fatal("expected nil tool when store is nil")
	}
}

func TestDevToolTool_Schema(t *testing.T) {
	tool := tools.NewDevToolTool(&mockToolStore{})
	if tool.Name() != "dev_tool" {
		t.Fatalf("name = %q, want dev_tool", tool.Name())
	}
	if tool.Description() == "" {
		t.Fatal("description must not be empty")
	}
	params := tool.Parameters()
	if params["type"] != "object" {
		t.Fatal("parameters must be a JSON Schema object")
	}
	required, _ := params["required"].([]string)
	if len(required) != 1 || required[0] != "action" {
		t.Fatalf("required = %v, want [action]", required)
	}
	props, _ := params["properties"].(map[string]any)
	action, _ := props["action"].(map[string]any)
	enum, _ := action["enum"].([]string)
	if len(enum) != 3 {
		t.Fatalf("action enum = %v, want [list read save]", enum)
	}
}

func TestDevToolTool_MissingAction(t *testing.T) {
	tool := tools.NewDevToolTool(&mockToolStore{})
	_, err := tool.Execute(context.Background(), map[string]any{})
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Fatalf("missing action: err = %v, want ErrSchemaValidation", err)
	}
}

func TestDevToolTool_UnknownAction(t *testing.T) {
	tool := tools.NewDevToolTool(&mockToolStore{})
	_, err := tool.Execute(context.Background(), map[string]any{"action": "delete"})
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Fatalf("unknown action: err = %v, want ErrSchemaValidation", err)
	}
}

func TestDevToolTool_ListEmpty(t *testing.T) {
	tool := tools.NewDevToolTool(&mockToolStore{})
	out, err := tool.Execute(context.Background(), map[string]any{"action": "list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if out != "No scripts registered yet." {
		t.Fatalf("list empty out = %q", out)
	}
}

func TestDevToolTool_ListWithScripts(t *testing.T) {
	tool := tools.NewDevToolTool(&mockToolStore{
		tools: []tools.ToolInfo{
			{Name: "csv_cleaner", Description: "Cleans CSV files", Params: "filepath (str)"},
			{Name: "chart_line", Description: "Creates line charts", Params: "data (list)"},
		},
	})
	out, err := tool.Execute(context.Background(), map[string]any{"action": "list"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "csv_cleaner") || !strings.Contains(out, "chart_line") {
		t.Fatalf("list out missing entries: %q", out)
	}
}

func TestDevToolTool_ReadMissingName(t *testing.T) {
	tool := tools.NewDevToolTool(&mockToolStore{})
	_, err := tool.Execute(context.Background(), map[string]any{"action": "read"})
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Fatalf("read missing name: err = %v, want ErrSchemaValidation", err)
	}
}

func TestDevToolTool_ReadReturnsCode(t *testing.T) {
	tool := tools.NewDevToolTool(&mockToolStore{code: "def run(): return 42"})
	out, err := tool.Execute(context.Background(), map[string]any{"action": "read", "name": "test"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if out != "def run(): return 42" {
		t.Fatalf("read out = %q", out)
	}
}

func TestDevToolTool_SaveMissingRequired(t *testing.T) {
	tool := tools.NewDevToolTool(&mockToolStore{})
	cases := []map[string]any{
		{"action": "save"},                                            // no name
		{"action": "save", "name": "x"},                               // no description
		{"action": "save", "name": "x", "description": "d"},           // no code
		{"action": "save", "name": "", "description": "d", "code": "c"}, // empty name
	}
	for i, args := range cases {
		_, err := tool.Execute(context.Background(), args)
		if !errors.Is(err, llm.ErrSchemaValidation) {
			t.Fatalf("case %d: err = %v, want ErrSchemaValidation", i, err)
		}
	}
}

func TestDevToolTool_SaveSucceeds(t *testing.T) {
	store := &mockToolStore{}
	tool := tools.NewDevToolTool(store)
	out, err := tool.Execute(context.Background(), map[string]any{
		"action":      "save",
		"name":        "test_tool",
		"description": "A test tool",
		"code":        "def run(): return 42",
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !strings.Contains(out, "test_tool") {
		t.Fatalf("save out = %q, want mention of test_tool", out)
	}
	if !store.saveCalled {
		t.Fatal("save did not call store.SaveTool")
	}
}

type mockToolStore struct {
	tools      []tools.ToolInfo
	code       string
	saveCalled bool
}

func (m *mockToolStore) ListTools() ([]tools.ToolInfo, error)    { return m.tools, nil }
func (m *mockToolStore) GetToolCode(name string) (string, error) { return m.code, nil }
func (m *mockToolStore) SaveTool(ctx context.Context, name, desc, params, code, usage string) error {
	m.saveCalled = true
	return nil
}
