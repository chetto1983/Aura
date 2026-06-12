package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// FSEdit replaces an exact string in a file. Like Claude Code's Edit, old_string
// must match exactly and be unique unless replace_all is set. SkillsDir, when set,
// fences edits out of the skills library (#54 / D-43); empty disables the fence.
type FSEdit struct{ WorkspaceRoot, SkillsDir string }

type fsEditArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

func (t *FSEdit) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "File path to edit (absolute, or relative to the workspace)."},
    "old_string": {"type": "string", "description": "Exact text to replace. Must be unique in the file unless replace_all is true."},
    "new_string": {"type": "string", "description": "Replacement text. Must differ from old_string."},
    "replace_all": {"type": "boolean", "description": "Replace every occurrence instead of requiring a unique match."}
  },
  "required": ["path", "old_string", "new_string"]
}`)
	return Spec{
		Name:        "fs_edit",
		Summary:     "Replace an exact string in a file.",
		Description: "Replace an exact string in a file — the surgical way to change an existing file without rewriting it. Read the file first (fs_read) so `old_string` matches the on-disk bytes exactly, including indentation. `old_string` must be UNIQUE in the file: if it is not, the edit fails — add surrounding context to make it unique, or set `replace_all` to change every occurrence (use this to rename a symbol). `new_string` must differ from `old_string`. Returns the count of replacements. Prefer editing an existing file over overwriting it with fs_write. Example: {\"path\":\"internal/server.go\",\"old_string\":\"port := 8080\",\"new_string\":\"port := 9090\"}; to rename every occurrence, {\"path\":\"app.py\",\"old_string\":\"old_name\",\"new_string\":\"new_name\",\"replace_all\":true}.",
		Parameters:  params,
		Deferred:    false,
		Mutating:    true,
	}
}

func (t *FSEdit) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a fsEditArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("fs_edit args: %w", err)
	}
	if strings.TrimSpace(a.Path) == "" {
		return ToolResult{}, fmt.Errorf("fs_edit: path is required")
	}
	if a.OldString == "" {
		return ToolResult{}, fmt.Errorf("fs_edit: old_string must be non-empty; use fs_write to create or overwrite a file")
	}
	if a.OldString == a.NewString {
		return ToolResult{}, fmt.Errorf("fs_edit: old_string and new_string are identical")
	}
	path := resolveFSPath(t.WorkspaceRoot, a.Path)
	if err := deniedSkillsWrite(t.SkillsDir, path, "fs_edit"); err != nil {
		return ToolResult{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{}, fmt.Errorf("fs_edit: %w", err)
	}
	content := string(b)
	count := strings.Count(content, a.OldString)
	switch {
	case count == 0:
		return ToolResult{}, fmt.Errorf("fs_edit: old_string not found in %s", path)
	case count > 1 && !a.ReplaceAll:
		return ToolResult{}, fmt.Errorf("fs_edit: old_string is not unique in %s (%d matches); add surrounding context or set replace_all", path, count)
	}
	if a.ReplaceAll {
		content = strings.ReplaceAll(content, a.OldString, a.NewString)
	} else {
		content = strings.Replace(content, a.OldString, a.NewString, 1)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return ToolResult{}, fmt.Errorf("fs_edit: %w", err)
	}
	return NewResult(ctx, fmt.Sprintf("replaced %d occurrence(s) in %s", count, path))
}
