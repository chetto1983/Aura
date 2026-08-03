package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// FSWrite writes (creating or overwriting) a file INSIDE the caller's per-identity box, through
// the router's tar copy-in. There is no host arm: a box that cannot be reached fails CLOSED
// (D-09/GATE-01). The skills-library fence (#54 / D-43) is box-relative (deniedBoxSkillsWrite over
// the literal /skills mount) and needs no configured directory. This file deliberately imports
// neither os nor path/filepath — their absence is a compile-time proof that no host write survives.
//
// TWO GUARANTEES THE DELETED HOST ARM HAD AND THIS PATH DOES NOT, recorded rather than hidden:
// F-010 (an overwrite preserved the target's mode, so a 0o755 script stayed executable) and AG-045
// (a temp-file+rename made a partial file unobservable). Router.WriteFile hardcodes 0o644 and
// CopyFileIn tar-extracts in place, and it is the only write primitive the box exposes. Restoring
// F-010 costs a stat round-trip on every write — including the common create, where there is no
// mode to preserve — and inside a single-user root box only the executable bit is load-bearing,
// which the agent can restore with chmod. Restoring AG-045 means widening the write primitive.
// Both are open decisions for the PRD, not accidents of this collapse.
type FSWrite struct {
	Router *usersandbox.SandboxRouter
}

type fsWriteArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *FSWrite) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "path": {"type": "string", "description": "File path to write inside your workspace container (absolute, or relative to /workspace). Parent directories are created as needed."},
    "content": {"type": "string", "description": "Full file contents. Overwrites any existing file."}
  },
  "required": ["path"]
}`)
	return Spec{
		Name:        "fs_write",
		Summary:     "Write a file to disk (create or overwrite).",
		Description: "Write a whole file into your workspace container: parent directories are created as needed and any existing file is OVERWRITTEN (this replaces the file, it does not append). Pass the COMPLETE `content`. For a small change to an existing file prefer fs_edit, which preserves the rest of the file; use fs_write to create a new file or fully replace one. Never author file content through the shell — heredocs and quoted echo/printf break on quoting; this tool stores content exactly. Always report the absolute path of what you wrote. Example: {\"path\":\"results/report.md\",\"content\":\"# Results\\n\\nAll tests passed.\\n\"}.",
		Parameters:  params,
		// Deferred con gli altri quattro tool di filesystem (vedi fs_read).
		Deferred:       true,
		Mutating:       true,
		OperationScope: OperationScopeAgent, OperationNormalizer: OperationNormalizerCanonical,
		ReplayPolicy: ReplayToolResult,
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
	boxPath, err := boxPathArg("fs_write", a.Path)
	if err != nil {
		return ToolResult{}, err
	}
	if deniedBoxSkillsWrite(boxPath) {
		return ToolResult{}, fmt.Errorf("fs_write: %s is inside the sandbox skills mount; author skills through the gated `skill` tool "+
			"(action=create/update/delete), not direct file writes", boxPath)
	}

	// Every guard above is content- or argument-based and runs BEFORE the route on purpose: an
	// oversized write, a bad path or a fenced target is the model's own error, not a sandbox
	// outage, and must read as one — and the skills fence must refuse without touching the box.
	boxHandle, routeErr := t.Router.Route(ctx)
	if routeErr != nil {
		return sandboxUnavailableResult("fs_write", routeErr), nil
	}
	if err := t.Router.WriteFile(ctx, boxHandle, boxPath, []byte(a.Content)); err != nil {
		return sandboxUnavailableResult("fs_write", err), nil
	}
	return NewResult(ctx, fmt.Sprintf("wrote %d bytes to %s", len(a.Content), boxPath))
}
