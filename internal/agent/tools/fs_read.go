package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// FSRead reads a file from the host filesystem in-process (no sandbox hop). Under a strict profile
// (Router non-nil + Route returns routed=true) it instead reads the file INSIDE the per-identity
// box via a bounded `head -c` exec, failing CLOSED on a box error (D-09/GATE-01) — never a host
// os.ReadFile fallback.
type FSRead struct {
	WorkspaceRoot string
	Router        *usersandbox.SandboxRouter
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
    "path": {"type": "string", "description": "File path to read (absolute, or relative to the workspace)."},
    "offset": {"type": "integer", "minimum": 1, "description": "Optional 1-based start line. Omit to read from the top."},
    "limit": {"type": "integer", "minimum": 1, "description": "Optional max number of lines to return."}
  },
  "required": ["path"]
}`)
	return Spec{
		Name:        "fs_read",
		Summary:     "Read a file from disk.",
		Description: "Read a file from the host filesystem and return its contents. `path` is absolute or workspace-relative. By default the whole file is returned; for a long file pass a 1-based `offset` plus a `limit` to read a targeted range instead of pulling the whole file into context. A large result pages to a sidecar you read with read_tool_output rather than flooding the conversation. Read a file with this tool BEFORE editing it — fs_edit matches the exact bytes returned here. A missing or empty file returns an explicit error/notice, never silent empty content. Prefer these structured file tools over shell cat/grep so results come back structured and large files page. Example: {\"path\":\"cmd/aura/main.go\"} for a whole file, or {\"path\":\"app.log\",\"offset\":2000,\"limit\":200} to read just a window.",
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

	// Route decision (plan 37-07): routed ⇒ read in-box (no host stat/read); routed+err ⇒ deny.
	boxHandle, routed, routeErr := t.Router.Route(ctx)
	if routed && routeErr != nil {
		return sandboxUnavailableResult("fs_read", routeErr), nil
	}
	if routed {
		return t.readInBox(ctx, boxHandle, a)
	}

	path := resolveFSPath(t.WorkspaceRoot, a.Path)
	if err := statSizeWithinCap(path, "fs_read", true); err != nil {
		return ToolResult{}, err
	}
	b, err := os.ReadFile(path)
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

// readInBox reads a.Path from inside the box via a bounded `head -c <cap+1>` exec: the size cap is
// enforced on the returned bytes (content-based, so it holds in-box just as the host statSizeWithinCap
// does host-side — the host stat is NOT run against a box path). A missing file / directory surfaces
// as head's non-zero exit; the NUL/offset/limit handling matches the host path.
func (t *FSRead) readInBox(ctx context.Context, h usersandbox.BoxHandle, a fsReadArgs) (ToolResult, error) {
	cap := fsMaxReadBytes()
	cmd := fmt.Sprintf("head -c %d -- %s", cap+1, shellQuoteArg(a.Path))
	res, err := t.Router.Exec(ctx, h, usersandbox.ExecRequest{Command: cmd})
	if err != nil {
		return sandboxUnavailableResult("fs_read", err), nil
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(string(res.Stderr))
		if msg == "" {
			msg = fmt.Sprintf("cannot read %q inside the sandbox (exit %d)", a.Path, res.ExitCode)
		}
		return ToolResult{}, fmt.Errorf("fs_read: %s", msg)
	}
	b := res.Stdout
	if int64(len(b)) > cap {
		return ToolResult{}, fmt.Errorf("fs_read: %s is over the %d-byte cap (%s); read a window with offset+limit instead of the whole file",
			a.Path, cap, envFSMaxReadBytes)
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
