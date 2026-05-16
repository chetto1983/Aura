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

func TestApplyPatchRejectsAmbiguousMatch(t *testing.T) {
	root := newWorkspaceToolRoot(t)
	if err := root.WriteAtomic("notes/dup.md", []byte("ping\nping\n")); err != nil {
		t.Fatal(err)
	}
	patch := NewApplyPatchTool(root)
	if _, err := patch.Execute(context.Background(), map[string]any{
		"path": "notes/dup.md",
		"old":  "ping",
		"new":  "pong",
	}); err == nil || !strings.Contains(err.Error(), "matches 2 locations") {
		t.Fatalf("expected ambiguous match rejection, got %v", err)
	}
	got, err := root.Read("notes/dup.md", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "ping\nping\n" {
		t.Fatalf("file should be unchanged on ambiguous match, got %q", got)
	}
	out, err := patch.Execute(context.Background(), map[string]any{
		"path":        "notes/dup.md",
		"old":         "ping",
		"new":         "pong",
		"replace_all": true,
	})
	if err != nil {
		t.Fatalf("replace_all should succeed: %v", err)
	}
	if !strings.Contains(out, `"replacements": 2`) {
		t.Fatalf("expected 2 replacements: %s", out)
	}
}

func TestWorkspaceFileToolsBlockServerManagedWikiFiles(t *testing.T) {
	root := newWorkspaceToolRoot(t)
	write := NewWriteFileTool(root)
	patch := NewApplyPatchTool(root)
	for _, p := range []string{"wiki/index.md", "wiki/log.md", "wiki/SCHEMA.md"} {
		if _, err := write.Execute(context.Background(), map[string]any{"path": p, "content": "junk"}); err == nil {
			t.Fatalf("write_file(%q) should be rejected as server-managed", p)
		}
		if _, err := patch.Execute(context.Background(), map[string]any{"path": p, "old": "a", "new": "b"}); err == nil {
			t.Fatalf("apply_patch(%q) should be rejected as server-managed", p)
		}
	}
}

func TestWorkspaceFileToolsValidateWikiWrites(t *testing.T) {
	root := newWorkspaceToolRoot(t)
	write := NewWriteFileTool(root)

	_, err := write.Execute(context.Background(), map[string]any{
		"path":    "wiki/broken.md",
		"content": "# Missing frontmatter\n",
	})
	if err == nil || !strings.Contains(err.Error(), "wiki validation") {
		t.Fatalf("write_file invalid wiki error = %v, want wiki validation", err)
	}
	if _, readErr := root.Read("wiki/broken.md", 1024); readErr == nil {
		t.Fatal("invalid wiki page was written")
	}

	out, err := write.Execute(context.Background(), map[string]any{
		"path": "wiki/valid-page.md",
		"content": strings.Join([]string{
			"---",
			"title: Valid Page",
			"tags: [aura]",
			"category: test",
			"schema_version: 2",
			"prompt_version: ingest_v1",
			"created_at: 2026-05-08T10:00:00Z",
			"updated_at: 2026-05-08T10:00:00Z",
			"---",
			"Valid body with [[links]].",
			"",
		}, "\n"),
	})
	if err != nil {
		t.Fatalf("write_file valid wiki: %v", err)
	}
	if !strings.Contains(out, `"validated": "wiki"`) {
		t.Fatalf("write_file output = %s, want wiki validation marker", out)
	}

	_, err = write.Execute(context.Background(), map[string]any{
		"path": "wiki/wrong-slug.md",
		"content": strings.Join([]string{
			"---",
			"title: Right Slug",
			"category: test",
			"schema_version: 2",
			"prompt_version: ingest_v1",
			"created_at: 2026-05-08T10:00:00Z",
			"updated_at: 2026-05-08T10:00:00Z",
			"---",
			"Valid body.",
			"",
		}, "\n"),
	})
	if err == nil || !strings.Contains(err.Error(), "does not match title slug") {
		t.Fatalf("write_file wiki slug mismatch error = %v", err)
	}
}

func TestWorkspaceFileToolsRejectInvalidWikiPatchWithoutChangingFile(t *testing.T) {
	root := newWorkspaceToolRoot(t)
	original := strings.Join([]string{
		"---",
		"title: Patch Page",
		"category: test",
		"schema_version: 2",
		"prompt_version: ingest_v1",
		"created_at: 2026-05-08T10:00:00Z",
		"updated_at: 2026-05-08T10:00:00Z",
		"---",
		"Patch body.",
		"",
	}, "\n")
	if err := root.WriteAtomic("wiki/patch-page.md", []byte(original)); err != nil {
		t.Fatal(err)
	}

	_, err := NewApplyPatchTool(root).Execute(context.Background(), map[string]any{
		"path": "wiki/patch-page.md",
		"old":  "Patch body.",
		"new":  "",
	})
	if err == nil || !strings.Contains(err.Error(), "wiki validation") {
		t.Fatalf("apply_patch invalid wiki error = %v, want wiki validation", err)
	}
	got, err := root.Read("wiki/patch-page.md", 4096)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("invalid patch changed file:\n%s", got)
	}
}

func TestWorkspaceFileToolsValidateSkillWrites(t *testing.T) {
	root := newWorkspaceToolRoot(t)
	write := NewWriteFileTool(root)

	_, err := write.Execute(context.Background(), map[string]any{
		"path":    ".agents/skills/broken/SKILL.md",
		"content": "no frontmatter\n",
	})
	if err == nil || !strings.Contains(err.Error(), "skill validation") {
		t.Fatalf("write_file invalid skill error = %v, want skill validation", err)
	}
	if _, readErr := root.Read(".agents/skills/broken/SKILL.md", 1024); readErr == nil {
		t.Fatal("invalid skill was written")
	}

	_, err = write.Execute(context.Background(), map[string]any{
		"path": ".agents/skills/wrong-name/SKILL.md",
		"content": strings.Join([]string{
			"---",
			"name: other-name",
			"description: Test skill",
			"---",
			"Use it carefully.",
			"",
		}, "\n"),
	})
	if err == nil || !strings.Contains(err.Error(), "does not match directory") {
		t.Fatalf("write_file skill name mismatch error = %v", err)
	}

	out, err := write.Execute(context.Background(), map[string]any{
		"path": ".agents/skills/good-skill/SKILL.md",
		"content": strings.Join([]string{
			"---",
			"name: good-skill",
			"description: Test skill",
			"---",
			"Use it carefully.",
			"",
		}, "\n"),
	})
	if err != nil {
		t.Fatalf("write_file valid skill: %v", err)
	}
	if !strings.Contains(out, `"validated": "skill"`) {
		t.Fatalf("write_file output = %s, want skill validation marker", out)
	}
}

func TestWorkspaceFileToolsDoNotValidateOrdinaryMarkdown(t *testing.T) {
	root := newWorkspaceToolRoot(t)
	out, err := NewWriteFileTool(root).Execute(context.Background(), map[string]any{
		"path":    "docs/note.md",
		"content": "# Plain markdown without frontmatter\n",
	})
	if err != nil {
		t.Fatalf("write_file ordinary markdown: %v", err)
	}
	if strings.Contains(out, "validated") {
		t.Fatalf("write_file output = %s, did not expect validation marker", out)
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

func TestReadFileOnDirectoryReturnsListingInsteadOfError(t *testing.T) {
	// The previous behaviour was to error with "is a directory" and prod the
	// model via a hint to call list_files. The conversation logs showed the
	// model would then waste a turn translating that error into list_files,
	// often with the wrong path. read_file now resolves the case itself:
	// when the path is a directory, it returns the directory's listing in
	// the same response shape.
	root := newWorkspaceToolRoot(t)
	if err := os.MkdirAll(filepath.Join(root.Path(), "wiki"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root.Path(), "wiki", "note.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := NewReadFileTool(root).Execute(context.Background(), map[string]any{"path": "wiki"})
	if err != nil {
		t.Fatalf("read_file on directory should not error: %v", err)
	}
	if !strings.Contains(result, `"type": "directory"`) {
		t.Fatalf("expected directory type marker, got %s", result)
	}
	if !strings.Contains(result, "note.md") {
		t.Fatalf("expected listing to include note.md, got %s", result)
	}
}

func TestReadFileOversizeReturnsTruncated(t *testing.T) {
	root := newWorkspaceToolRoot(t)
	big := strings.Repeat("x", 10_000)
	if err := os.WriteFile(filepath.Join(root.Path(), "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := NewReadFileTool(root).Execute(context.Background(),
		map[string]any{"path": "big.txt", "max_bytes": 500})
	if err != nil {
		t.Fatalf("oversize read should not error: %v", err)
	}
	if !strings.Contains(result, `"truncated": true`) {
		t.Fatalf("expected truncated: true, got %s", result)
	}
	if !strings.Contains(result, `"total_bytes": 10000`) {
		t.Fatalf("expected total_bytes: 10000, got %s", result)
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
	// os.Root holds a dir FD; on Windows TempDir cleanup fails unless
	// we Close first.
	t.Cleanup(func() { _ = root.Close() })
	return root
}
