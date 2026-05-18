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

// TestFileTool_ActionInference verifies that the file tool infers the action
// from arg shape when the "action" field is omitted.
func TestFileTool_ActionInference(t *testing.T) {
	ctx := context.Background()

	t.Run("infer write from content+path", func(t *testing.T) {
		tool := NewFileTool(newWorkspaceToolRoot(t))
		out, err := tool.Execute(ctx, map[string]any{
			"path":    "infer/write.txt",
			"content": "hello inference",
		})
		if err != nil {
			t.Fatalf("infer write: %v", err)
		}
		if !strings.Contains(out, `"status": "written"`) {
			t.Fatalf("infer write output = %s", out)
		}
	})

	t.Run("infer write from content only", func(t *testing.T) {
		tool := NewFileTool(newWorkspaceToolRoot(t))
		out, err := tool.Execute(ctx, map[string]any{
			"path":    "infer/nopath.txt",
			"content": "content only",
		})
		if err != nil {
			t.Fatalf("infer write (content only): %v", err)
		}
		if !strings.Contains(out, `"status": "written"`) {
			t.Fatalf("infer write output = %s", out)
		}
	})

	t.Run("infer read from path only", func(t *testing.T) {
		tool := NewFileTool(newWorkspaceToolRoot(t))
		// write first so the file exists
		_, _ = tool.Execute(ctx, map[string]any{"action": "write", "path": "infer/read.txt", "content": "readable"})
		out, err := tool.Execute(ctx, map[string]any{"path": "infer/read.txt"})
		if err != nil {
			t.Fatalf("infer read: %v", err)
		}
		if !strings.Contains(out, "readable") {
			t.Fatalf("infer read output = %s", out)
		}
	})

	t.Run("infer search from pattern", func(t *testing.T) {
		tool := NewFileTool(newWorkspaceToolRoot(t))
		_, _ = tool.Execute(ctx, map[string]any{"action": "write", "path": "infer/search.txt", "content": "findme"})
		out, err := tool.Execute(ctx, map[string]any{"pattern": "findme"})
		if err != nil {
			t.Fatalf("infer search: %v", err)
		}
		if !strings.Contains(out, "findme") && !strings.Contains(out, "infer/search.txt") {
			t.Fatalf("infer search output = %s", out)
		}
	})

	t.Run("infer patch from old+new+path", func(t *testing.T) {
		tool := NewFileTool(newWorkspaceToolRoot(t))
		_, _ = tool.Execute(ctx, map[string]any{"action": "write", "path": "infer/patch.txt", "content": "before"})
		out, err := tool.Execute(ctx, map[string]any{
			"path": "infer/patch.txt",
			"old":  "before",
			"new":  "after",
		})
		if err != nil {
			t.Fatalf("infer patch: %v", err)
		}
		if !strings.Contains(out, `"replacements": 1`) {
			t.Fatalf("infer patch output = %s", out)
		}
	})

	t.Run("ambiguous: old without new fails with ErrSchemaValidation", func(t *testing.T) {
		tool := NewFileTool(newWorkspaceToolRoot(t))
		_, err := tool.Execute(ctx, map[string]any{"path": "x.txt", "old": "foo"})
		if !errors.Is(err, llm.ErrSchemaValidation) {
			t.Fatalf("partial patch args: err = %v, want ErrSchemaValidation", err)
		}
	})

	t.Run("ambiguous: new without old fails with ErrSchemaValidation", func(t *testing.T) {
		tool := NewFileTool(newWorkspaceToolRoot(t))
		_, err := tool.Execute(ctx, map[string]any{"path": "x.txt", "new": "bar"})
		if !errors.Is(err, llm.ErrSchemaValidation) {
			t.Fatalf("partial patch args: err = %v, want ErrSchemaValidation", err)
		}
	})
}

// TestFileInferAction unit-tests the inference function directly.
func TestFileInferAction(t *testing.T) {
	cases := []struct {
		name      string
		args      map[string]any
		wantAct   string
		wantScore int
		wantAmb   bool
	}{
		{"patch old+new", map[string]any{"old": "x", "new": "y", "path": "f"}, "patch", 2, false},
		{"patch old+new no path", map[string]any{"old": "x", "new": "y"}, "patch", 2, false},
		{"write content+path", map[string]any{"content": "c", "path": "f"}, "write", 1, false},
		{"write content only", map[string]any{"content": "c"}, "write", 1, false},
		{"search pattern", map[string]any{"pattern": "p"}, "search", 1, false},
		{"read path only", map[string]any{"path": "f"}, "read", 1, false},
		{"empty args score 0", map[string]any{}, "", 0, false},
		{"ambiguous old only", map[string]any{"old": "x"}, "", 0, true},
		{"ambiguous new only", map[string]any{"new": "y"}, "", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			act, score, amb := fileInferAction(tc.args)
			if act != tc.wantAct || score != tc.wantScore || amb != tc.wantAmb {
				t.Fatalf("fileInferAction(%v) = (%q, %d, %v), want (%q, %d, %v)",
					tc.args, act, score, amb, tc.wantAct, tc.wantScore, tc.wantAmb)
			}
		})
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
