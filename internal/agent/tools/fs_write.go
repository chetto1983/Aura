package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// FSWrite writes (creating or overwriting) a file on the host filesystem.
// SkillsDir, when set, fences writes out of the skills library so the gated
// skill-authoring flow cannot be bypassed (#54 / D-43); empty disables the fence.
// Router, non-nil under a strict profile, routes the write INSIDE the per-identity box (tar
// copy-in) with a box-relative /skills fence, failing CLOSED on a box error — never a host write.
type FSWrite struct {
	WorkspaceRoot, SkillsDir string
	Router                   *usersandbox.SandboxRouter
}

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

	// Route decision (plan 37-07): routed ⇒ write in-box (tar copy-in, no host write); routed+err ⇒ deny.
	// The content-length cap above is content-based and stays on BOTH branches.
	boxHandle, routed, routeErr := t.Router.Route(ctx)
	if routed && routeErr != nil {
		return sandboxUnavailableResult("fs_write", routeErr), nil
	}
	if routed {
		return t.writeInBox(ctx, boxHandle, a)
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
	// Atomic temp-file + rename (AG-045): a crash mid-write never leaves a truncated
	// file and a reader never sees a partial one — matches fs_edit. An overwrite
	// preserves the existing file's mode; a new file is created 0o644 (F-010).
	if err := atomicWriteFile(path, []byte(a.Content), existingFileMode(path)); err != nil {
		return ToolResult{}, fmt.Errorf("fs_write: %w", err)
	}
	return NewResult(ctx, fmt.Sprintf("wrote %d bytes to %s", len(a.Content), path))
}

// writeInBox tar-copies content to a.Path inside the box (never the host). It carries a box-relative
// skills-write fence (deniedBoxSkillsWrite) equivalent to the host deniedSkillsWrite: a write into
// the materialized /skills mount is refused so the gated skill-authoring flow is not bypassed even
// in-box (the box /skills is a materialized COPY per D-10, so the host library is structurally
// untouchable regardless — this fence guards the in-box copy). A copy-in failure denies (fail-CLOSED).
func (t *FSWrite) writeInBox(ctx context.Context, h usersandbox.BoxHandle, a fsWriteArgs) (ToolResult, error) {
	if deniedBoxSkillsWrite(a.Path) {
		return ToolResult{}, fmt.Errorf("fs_write: %s is inside the sandbox skills mount; author skills through the gated `skill` tool "+
			"(action=create/update/delete), not direct file writes", a.Path)
	}
	if err := t.Router.WriteFile(ctx, h, a.Path, []byte(a.Content)); err != nil {
		return sandboxUnavailableResult("fs_write", err), nil
	}
	return NewResult(ctx, fmt.Sprintf("wrote %d bytes to %s", len(a.Content), a.Path))
}
