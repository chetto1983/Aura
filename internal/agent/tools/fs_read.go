package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// FSRead reads a file from INSIDE the caller's per-identity box through boxReadFileCapped, the
// bounded read it shares with fs_edit. There is no host arm: a box that cannot be reached fails
// CLOSED (D-09/GATE-01). This file deliberately imports neither os nor path/filepath — go vet
// rejects an unused import, so their absence is a compile-time proof that no host filesystem call
// survives here.
type FSRead struct {
	Router *usersandbox.SandboxRouter
}

type fsReadArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

func (t *FSRead) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "File path to read inside your workspace container (absolute, or relative to /workspace)."},
    "offset": {"type": "integer", "minimum": 1, "description": "Optional 1-based start line. Omit to read from the top."},
    "limit": {"type": "integer", "minimum": 1, "description": "Optional max number of lines to return."}
  },
  "required": ["path"]
}`)
	return Spec{
		Name:        "fs_read",
		Summary:     "Read a file from disk.",
		Description: "Read a file from your workspace container and return its contents. `path` is absolute (e.g. /workspace/notes.md) or relative to /workspace. By default the whole file is returned; for a long file pass a 1-based `offset` plus a `limit` to read a targeted range instead of pulling the whole file into context. A large result pages to a sidecar you read with read_tool_output rather than flooding the conversation. Read a file with this tool BEFORE editing it — fs_edit matches the exact bytes returned here. A missing or empty file returns an explicit error/notice, never silent empty content. Prefer these structured file tools over shell cat/grep so results come back structured and large files page. Example: {\"path\":\"cmd/aura/main.go\"} for a whole file, or {\"path\":\"app.log\",\"offset\":2000,\"limit\":200} to read just a window.",
		Parameters:  params,
		// Deferred con fs_edit/fs_glob/fs_grep, che lo erano gia': tenere due dei cinque
		// tool di filesystem sempre visibili spingeva a ripiegare su quelli invece del piu'
		// specifico. O tutti o nessuno, e nessuno costa meno.
		Deferred: true,
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

	boxPath, err := boxPathArg("fs_read", a.Path)
	if err != nil {
		return ToolResult{}, err
	}

	// The argument guards above run BEFORE the route on purpose: a malformed path is the model's
	// own error, not a sandbox outage, and must read as one.
	boxHandle, routeErr := t.Router.Route(ctx)
	if routeErr != nil {
		return sandboxUnavailableResult("fs_read", routeErr), nil
	}

	b, deny, err := boxReadFileCapped(ctx, t.Router, boxHandle, "fs_read", boxPath)
	if deny != nil {
		return *deny, nil
	}
	if err != nil {
		return ToolResult{}, err
	}
	content := string(b)
	if a.Offset > 0 || a.Limit > 0 {
		content = sliceLines(content, a.Offset, a.Limit)
	}
	return NewResult(ctx, content)
}
