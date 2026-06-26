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
		Description: "Write a whole file to the host filesystem: parent directories are created as needed and any existing file is OVERWRITTEN (this replaces the file, it does not append). Pass the COMPLETE `content`. For a small change to an existing file prefer fs_edit, which preserves the rest of the file; use fs_write to create a new file or fully replace one. Never author file content through the shell — heredocs and quoted echo/printf break on quoting; this tool stores content exactly. Always report the absolute path of what you wrote. Example: {\"path\":\"results/report.md\",\"content\":\"# Results\\n\\nAll tests passed.\\n\"}.",
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
	if cap := fsMaxReadBytes(); int64(len(a.Content)) > cap {
		return ToolResult{}, fmt.Errorf("fs_write: content is %d bytes, over the %d-byte cap (%s)", len(a.Content), cap, envFSMaxReadBytes)
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
	// Atomic temp-file + rename (AG-045): a crash mid-write never leaves a
	// truncated file and a reader never sees a partial one — matches fs_edit.
	if err := atomicWriteFile(path, []byte(a.Content), 0o644); err != nil {
		return ToolResult{}, fmt.Errorf("fs_write: %w", err)
	}
	return NewResult(ctx, fmt.Sprintf("wrote %d bytes to %s", len(a.Content), path))
}
