package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestFileTool_NilRoot(t *testing.T) {
	if NewFileTool(nil) != nil {
		t.Fatal("nil root should yield nil tool")
	}
}

func TestFileTool_Schema(t *testing.T) {
	tool := NewFileTool(newWorkspaceToolRoot(t))
	if tool.Name() != "file" {
		t.Fatalf("name = %q, want file", tool.Name())
	}
	params := tool.Parameters()
	required, _ := params["required"].([]string)
	if len(required) != 1 || required[0] != "action" {
		t.Fatalf("required = %v, want [action]", required)
	}
	props, _ := params["properties"].(map[string]any)
	action, _ := props["action"].(map[string]any)
	enum, _ := action["enum"].([]string)
	if len(enum) != 5 {
		t.Fatalf("action enum = %v, want [list read search write patch]", enum)
	}
}

func TestFileTool_MissingAction(t *testing.T) {
	tool := NewFileTool(newWorkspaceToolRoot(t))
	_, err := tool.Execute(context.Background(), map[string]any{})
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Fatalf("missing action: err = %v, want ErrSchemaValidation", err)
	}
}

func TestFileTool_UnknownAction(t *testing.T) {
	tool := NewFileTool(newWorkspaceToolRoot(t))
	_, err := tool.Execute(context.Background(), map[string]any{"action": "delete"})
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Fatalf("unknown action: err = %v, want ErrSchemaValidation", err)
	}
}

func TestFileTool_WriteReadSearchPatchRoundtrip(t *testing.T) {
	tool := NewFileTool(newWorkspaceToolRoot(t))
	ctx := context.Background()

	// write
	out, err := tool.Execute(ctx, map[string]any{
		"action":  "write",
		"path":    "notes/probe.md",
		"content": "alpha\nbeta\ngamma\n",
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if !strings.Contains(out, `"status": "written"`) {
		t.Fatalf("write output = %s", out)
	}

	// read
	out, err = tool.Execute(ctx, map[string]any{"action": "read", "path": "notes/probe.md"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var readBody map[string]any
	if err := json.Unmarshal([]byte(out), &readBody); err != nil {
		t.Fatalf("read JSON: %v", err)
	}
	if readBody["content"] != "alpha\nbeta\ngamma\n" {
		t.Fatalf("read content = %#v", readBody["content"])
	}

	// search
	out, err = tool.Execute(ctx, map[string]any{"action": "search", "pattern": "BETA", "globs": []any{"**/*.md"}})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, `"path": "notes/probe.md"`) {
		t.Fatalf("search output = %s", out)
	}

	// patch
	out, err = tool.Execute(ctx, map[string]any{
		"action": "patch",
		"path":   "notes/probe.md",
		"old":    "beta",
		"new":    "delta",
	})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if !strings.Contains(out, `"replacements": 1`) {
		t.Fatalf("patch output = %s", out)
	}

	// list
	out, err = tool.Execute(ctx, map[string]any{"action": "list", "path": "notes"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, `"path": "notes/probe.md"`) {
		t.Fatalf("list output = %s", out)
	}
}

func TestFileTool_RejectsSensitivePaths(t *testing.T) {
	tool := NewFileTool(newWorkspaceToolRoot(t))
	ctx := context.Background()
	// Direct read of denylisted path must fail at workspace layer.
	if _, err := tool.Execute(ctx, map[string]any{"action": "read", "path": ".env"}); err == nil {
		t.Fatal("read .env should be denied")
	}
	if _, err := tool.Execute(ctx, map[string]any{"action": "write", "path": "data/secrets/token", "content": "x"}); err == nil {
		t.Fatal("write data/secrets/token should be denied")
	}
	if _, err := tool.Execute(ctx, map[string]any{"action": "read", "path": "../outside.txt"}); err == nil {
		t.Fatal("read ../outside.txt should be denied")
	}
}
