package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// FSRead reads a file from the host filesystem in-process (no sandbox hop).
type FSRead struct{ WorkspaceRoot string }

type fsReadArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

func (t *FSRead) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "File path to read (absolute, or relative to the workspace)."},
    "offset": {"type": "integer", "minimum": 1, "description": "Optional 1-based start line. Omit to read from the top."},
    "limit": {"type": "integer", "minimum": 1, "description": "Optional max number of lines to return."}
  },
  "required": ["path"]
}`)
	return Spec{
		Name:        "fs_read",
		Summary:     "Read a file from disk.",
		Description: "Read a file from the host filesystem and return its contents. `path` is absolute or workspace-relative. By default the whole file is returned; for a long file pass a 1-based `offset` plus a `limit` to read a targeted range instead of pulling the whole file into context. A large result pages to a sidecar you read with read_tool_output rather than flooding the conversation. Read a file with this tool BEFORE editing it — fs_edit matches the exact bytes returned here. A missing or empty file returns an explicit error/notice, never silent empty content. Prefer these structured file tools over shell cat/grep so results come back structured and large files page.",
		Parameters:  params,
		Deferred:    false,
	}
}

func (t *FSRead) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a fsReadArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("fs_read args: %w", err)
	}
	if strings.TrimSpace(a.Path) == "" {
		return ToolResult{}, fmt.Errorf("fs_read: path is required")
	}
	b, err := os.ReadFile(resolveFSPath(t.WorkspaceRoot, a.Path))
	if err != nil {
		return ToolResult{}, fmt.Errorf("fs_read: %w", err)
	}
	if looksBinary(b) {
		return ToolResult{}, fmt.Errorf("fs_read: binary file contains NUL bytes; use a binary-aware tool instead")
	}
	content := string(b)
	if a.Offset > 0 || a.Limit > 0 {
		content = sliceLines(content, a.Offset, a.Limit)
	}
	return NewResult(ctx, content)
}
