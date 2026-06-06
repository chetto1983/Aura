package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FSWrite writes (creating or overwriting) a file on the host filesystem.
// SkillsDir, when set, fences writes out of the skills library so the gated
// skill-authoring flow cannot be bypassed (#54 / D-43); empty disables the fence.
type FSWrite struct{ WorkspaceRoot, SkillsDir string }

type fsWriteArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *FSWrite) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "File path to write (absolute, or relative to the workspace). Parent directories are created as needed."},
    "content": {"type": "string", "description": "Full file contents. Overwrites any existing file."}
  },
  "required": ["path"]
}`)
	return Spec{
		Name:        "fs_write",
		Summary:     "Write a file to disk (create or overwrite).",
		Description: "Write a file to the host filesystem, creating parent directories as needed and overwriting any existing file. For surgical changes to an existing file prefer fs_edit.",
		Parameters:  params,
		Deferred:    false,
		Mutating:    true,
	}
}

func (t *FSWrite) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a fsWriteArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("fs_write args: %w", err)
	}
	if strings.TrimSpace(a.Path) == "" {
		return ToolResult{}, fmt.Errorf("fs_write: path is required")
	}
	path := resolveFSPath(t.WorkspaceRoot, a.Path)
	if err := deniedSkillsWrite(t.SkillsDir, path, "fs_write"); err != nil {
		return ToolResult{}, err
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return ToolResult{}, fmt.Errorf("fs_write: %w", err)
		}
	}
	if err := os.WriteFile(path, []byte(a.Content), 0o644); err != nil {
		return ToolResult{}, fmt.Errorf("fs_write: %w", err)
	}
	return NewResult(ctx, fmt.Sprintf("wrote %d bytes to %s", len(a.Content), path))
}
