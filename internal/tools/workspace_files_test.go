package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aura/aura/internal/workspace"
)

func TestWorkspaceFileToolsReadWriteSearchPatch(t *testing.T) {
	root := newWorkspaceToolRoot(t)
	write := NewWriteFileTool(root)
	read := NewReadFileTool(root)
	search := NewSearchFilesTool(root)
	patch := NewApplyPatchTool(root)

	out, err := write.Execute(context.Background(), map[string]any{
		"path":    "notes/a.md",
		"content": "alpha\nbeta\n",
	})
	if err != nil {
		t.Fatalf("write_file: %v", err)
	}
	if !strings.Contains(out, `"status": "written"`) {
		t.Fatalf("write_file output = %s", out)
	}

	out, err = read.Execute(context.Background(), map[string]any{"path": "notes/a.md"})
	if err != nil {
		t.Fatalf("read_file: %v", err)
	}
	var readBody map[string]any
	if err := json.Unmarshal([]byte(out), &readBody); err != nil {
		t.Fatalf("read_file JSON: %v", err)
	}
	if readBody["content"] != "alpha\nbeta\n" {
		t.Fatalf("read_file content = %#v", readBody["content"])
	}

	out, err = search.Execute(context.Background(), map[string]any{"pattern": "BETA", "globs": []any{"**/*.md"}})
	if err != nil {
		t.Fatalf("search_files: %v", err)
	}
	if !strings.Contains(out, `"path": "notes/a.md"`) {
		t.Fatalf("search_files output = %s", out)
	}

	out, err = patch.Execute(context.Background(), map[string]any{
		"path": "notes/a.md",
		"old":  "beta",
		"new":  "gamma",
	})
	if err != nil {
		t.Fatalf("apply_patch: %v", err)
	}
	if !strings.Contains(out, `"replacements": 1`) {
		t.Fatalf("apply_patch output = %s", out)
	}
	got, err := root.Read("notes/a.md", 1024)
	if err != nil {
		t.Fatalf("read patched file: %v", err)
	}
	if string(got) != "alpha\ngamma\n" {
		t.Fatalf("patched content = %q", got)
	}
}

func TestWorkspaceFileToolsDenySensitivePaths(t *testing.T) {
	root := newWorkspaceToolRoot(t)
	for _, tool := range []struct {
		name string
		run  func() error
	}{
		{name: "read .env", run: func() error {
			_, err := NewReadFileTool(root).Execute(context.Background(), map[string]any{"path": ".env"})
			return err
		}},
		{name: "write live db", run: func() error {
			_, err := NewWriteFileTool(root).Execute(context.Background(), map[string]any{"path": "data/aura.db", "content": "x"})
			return err
		}},
		{name: "patch git config", run: func() error {
			_, err := NewApplyPatchTool(root).Execute(context.Background(), map[string]any{"path": ".git/config", "old": "a", "new": "b"})
			return err
		}},
	} {
		t.Run(tool.name, func(t *testing.T) {
			if err := tool.run(); err == nil {
				t.Fatal("expected denial error")
			}
		})
	}
}

func TestListFilesHidesSensitivePaths(t *testing.T) {
	root := newWorkspaceToolRoot(t)
	if err := os.WriteFile(filepath.Join(root.Path(), ".env"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root.Path(), "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := NewListFilesTool(root).Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("list_files: %v", err)
	}
	if strings.Contains(out, ".env") || !strings.Contains(out, "README.md") {
		t.Fatalf("list_files output = %s", out)
	}
}

func TestNewWorkspaceFileToolsReturnsExpectedTools(t *testing.T) {
	got := NewWorkspaceFileTools(newWorkspaceToolRoot(t))
	names := make([]string, 0, len(got))
	for _, tool := range got {
		names = append(names, tool.Name())
	}
	want := strings.Join([]string{"list_files", "read_file", "search_files", "write_file", "apply_patch"}, ",")
	if strings.Join(names, ",") != want {
		t.Fatalf("tool names = %v", names)
	}
}

func newWorkspaceToolRoot(t *testing.T) *workspace.Root {
	t.Helper()
	root, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}
