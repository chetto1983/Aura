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
	if len(enum) != 14 {
		t.Fatalf("action enum = %v, want 14 file actions", enum)
	}
	examples, _ := params["examples"].([]any)
	if len(examples) < 14 {
		t.Fatalf("examples = %#v, want one example for each action", examples)
	}
	requiredByAction := map[string][]string{
		"list":        {"path"},
		"read":        {"path"},
		"search":      {"pattern"},
		"write":       {"path", "content"},
		"patch":       {"path", "old", "new"},
		"grep":        {"path", "search_text"},
		"path_info":   {"path"},
		"mkdir":       {"path"},
		"rmdir":       {"path"},
		"remove_file": {"path"},
		"move":        {"src", "dst"},
		"copy":        {"src", "dst"},
		"walk":        {"path"},
		"pwd":         {},
	}
	seen := map[string]bool{}
	for _, raw := range examples {
		example, _ := raw.(map[string]any)
		actionName, _ := example["action"].(string)
		requiredKeys, ok := requiredByAction[actionName]
		if !ok {
			continue
		}
		ok = true
		for _, requiredKey := range requiredKeys {
			if _, has := example[requiredKey]; !has {
				ok = false
				break
			}
		}
		if ok {
			seen[actionName] = true
		}
	}
	for actionName := range requiredByAction {
		if !seen[actionName] {
			t.Fatalf("examples = %#v, missing usable %s example", examples, actionName)
		}
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

	t.Run("infer grep from path+search_text", func(t *testing.T) {
		tool := NewFileTool(newWorkspaceToolRoot(t))
		_, _ = tool.Execute(ctx, map[string]any{"action": "write", "path": "infer/grep.txt", "content": "alpha\nneedle\n"})
		out, err := tool.Execute(ctx, map[string]any{"path": "infer/grep.txt", "search_text": "needle"})
		if err != nil {
			t.Fatalf("infer grep: %v", err)
		}
		if !strings.Contains(out, `"line": 2`) {
			t.Fatalf("infer grep output = %s", out)
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
		{"grep path+search_text", map[string]any{"path": "f", "search_text": "needle"}, "grep", 2, false},
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

	// grep one file
	out, err = tool.Execute(ctx, map[string]any{"action": "grep", "path": "notes/probe.md", "search_text": "DELTA", "case_insensitive": true})
	if err != nil {
		t.Fatalf("grep: %v", err)
	}
	if !strings.Contains(out, `"text": "delta"`) {
		t.Fatalf("grep output = %s", out)
	}

	// path_info
	out, err = tool.Execute(ctx, map[string]any{"action": "path_info", "path": "notes/probe.md"})
	if err != nil {
		t.Fatalf("path_info: %v", err)
	}
	if !strings.Contains(out, `"exists": true`) || !strings.Contains(out, `"type": "file"`) {
		t.Fatalf("path_info output = %s", out)
	}

	// mkdir + walk
	out, err = tool.Execute(ctx, map[string]any{"action": "mkdir", "path": "notes/empty"})
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if !strings.Contains(out, `"status": "created"`) {
		t.Fatalf("mkdir output = %s", out)
	}
	out, err = tool.Execute(ctx, map[string]any{"action": "walk", "path": "notes"})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if !strings.Contains(out, `"path": "notes/probe.md"`) || !strings.Contains(out, `"path": "notes/empty"`) {
		t.Fatalf("walk output = %s", out)
	}

	// copy + move + remove_file
	out, err = tool.Execute(ctx, map[string]any{"action": "copy", "src": "notes/probe.md", "dst": "notes/copy.md"})
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if !strings.Contains(out, `"status": "copied"`) || !strings.Contains(out, `"path": "notes/copy.md"`) {
		t.Fatalf("copy output = %s", out)
	}
	out, err = tool.Execute(ctx, map[string]any{"action": "move", "src": "notes/copy.md", "dst": "notes/moved.md"})
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if !strings.Contains(out, `"status": "moved"`) || !strings.Contains(out, `"path": "notes/moved.md"`) {
		t.Fatalf("move output = %s", out)
	}
	out, err = tool.Execute(ctx, map[string]any{"action": "remove_file", "path": "notes/moved.md"})
	if err != nil {
		t.Fatalf("remove_file: %v", err)
	}
	if !strings.Contains(out, `"status": "removed"`) {
		t.Fatalf("remove_file output = %s", out)
	}

	// rmdir + pwd
	out, err = tool.Execute(ctx, map[string]any{"action": "rmdir", "path": "notes/empty"})
	if err != nil {
		t.Fatalf("rmdir: %v", err)
	}
	if !strings.Contains(out, `"type": "dir"`) {
		t.Fatalf("rmdir output = %s", out)
	}
	out, err = tool.Execute(ctx, map[string]any{"action": "pwd"})
	if err != nil {
		t.Fatalf("pwd: %v", err)
	}
	if !strings.Contains(out, `"root": "workspace"`) || !strings.Contains(out, `"physical_root"`) {
		t.Fatalf("pwd output = %s", out)
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
