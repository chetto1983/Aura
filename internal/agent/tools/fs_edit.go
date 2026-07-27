package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// FSEdit replaces an exact string in a file. Like Claude Code's Edit, old_string
// must match exactly and be unique unless replace_all is set. SkillsDir, when set,
// fences edits out of the skills library (#54 / D-43); empty disables the fence.
//
// Router mirrors fs_read/fs_write: under a strict profile the edit happens INSIDE the
// per-identity box and never touches the host filesystem. Without it this tool was a hole
// in the containment fs_read/fs_write establish — reads and writes were confined to the box
// while an edit rewrote host files (fix plan 2.5, a blocker before sandbox enablement).
type FSEdit struct {
	WorkspaceRoot string
	SkillsDir     string
	Router        *usersandbox.SandboxRouter
}

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
		// Deferred: exact-string edit is discoverable via tool_search; only fs_read/fs_write
		// stay always-visible (operator directive — leaner manifest, less context per turn).
		Deferred:       true,
		Mutating:       true,
		OperationScope: OperationScopeAgent, OperationNormalizer: OperationNormalizerCanonical,
		ReplayPolicy: ReplayToolResult,
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

	// Route decision BEFORE any host path resolution or stat, exactly as fs_read does:
	// routed ⇒ edit in-box; routed+err ⇒ deny (fail-CLOSED, D-09/GATE-01), never a host fallback.
	boxHandle, routed, routeErr := t.Router.Route(ctx)
	if routed && routeErr != nil {
		return sandboxUnavailableResult("fs_edit", routeErr), nil
	}
	if routed {
		return t.editInBox(ctx, boxHandle, a)
	}

	path := resolveFSPath(t.WorkspaceRoot, a.Path)
	if err := deniedSkillsWrite(t.SkillsDir, path, "fs_edit"); err != nil {
		return ToolResult{}, err
	}
	if err := statSizeWithinCap(path, "fs_edit", false); err != nil {
		return ToolResult{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ToolResult{}, fmt.Errorf("fs_edit: %w", err)
	}
	content, count, err := applyExactEdit(string(b), a, path)
	if err != nil {
		return ToolResult{}, err
	}
	// Atomic temp-file + rename (AG-045): the edit replaces the file in one rename so
	// a crash mid-write never leaves a truncated file; concurrent edits remain
	// last-writer-wins (the read→edit→write window is not locked). The edited file is
	// known to exist, so its mode is preserved (F-010); the 0o644 fallback is defensive.
	if err := atomicWriteFile(path, []byte(content), existingFileMode(path)); err != nil {
		return ToolResult{}, fmt.Errorf("fs_edit: %w", err)
	}
	return NewResult(ctx, fmt.Sprintf("replaced %d occurrence(s) in %s", count, path))
}

// applyExactEdit is the ONE implementation of fs_edit's match rules, shared by the host and
// in-box paths so a change to either can never leave the other behind. label names the file in
// the errors — the resolved host path, or the box path. Returns the edited content and the
// number of occurrences replaced (exactly 1 unless replace_all).
func applyExactEdit(content string, a fsEditArgs, label string) (string, int, error) {
	count := strings.Count(content, a.OldString)
	switch {
	case count == 0:
		return "", 0, fmt.Errorf("fs_edit: old_string not found in %s", label)
	case count > 1 && !a.ReplaceAll:
		return "", 0, fmt.Errorf("fs_edit: old_string is not unique in %s (%d matches); add surrounding context or set replace_all", label, count)
	}
	if a.ReplaceAll {
		return strings.ReplaceAll(content, a.OldString, a.NewString), count, nil
	}
	return strings.Replace(content, a.OldString, a.NewString, 1), count, nil
}

// editInBox performs the replacement INSIDE the box and never touches the host: no stat, no
// os.ReadFile, no rename. It reads through the same bounded `head -c` exec fs_read uses, applies
// the shared match rules, and writes back through the router (tar upload, like fs_write).
//
// The host path's atomic temp-file+rename is deliberately NOT reproduced here: the router's
// WriteFile is the only write primitive the box exposes, so this path inherits its atomicity.
func (t *FSEdit) editInBox(ctx context.Context, h usersandbox.BoxHandle, a fsEditArgs) (ToolResult, error) {
	if deniedBoxSkillsWrite(a.Path) {
		return ToolResult{}, fmt.Errorf("fs_edit: %s is inside the sandbox skills mount; author skills through the gated `skill` tool "+
			"(action=create/update/delete), not direct file edits", a.Path)
	}
	readCap := fsMaxReadBytes()
	cmd := fmt.Sprintf("head -c %d -- %s", readCap+1, shellQuoteArg(a.Path))
	res, err := t.Router.Exec(ctx, h, usersandbox.ExecRequest{Command: cmd})
	if err != nil {
		return sandboxUnavailableResult("fs_edit", err), nil
	}
	if res.ExitCode != 0 {
		msg := strings.TrimSpace(string(res.Stderr))
		if msg == "" {
			msg = fmt.Sprintf("cannot read %q inside the sandbox (exit %d)", a.Path, res.ExitCode)
		}
		return ToolResult{}, fmt.Errorf("fs_edit: %s", msg)
	}
	b := res.Stdout
	if int64(len(b)) > readCap {
		return ToolResult{}, fmt.Errorf("fs_edit: %s content is %d bytes, over the %d-byte cap (%s)",
			a.Path, len(b), readCap, envFSMaxReadBytes)
	}
	if looksBinary(b) {
		return ToolResult{}, fmt.Errorf("fs_edit: binary file contains NUL bytes; use a binary-aware tool instead")
	}
	content, count, err := applyExactEdit(string(b), a, a.Path)
	if err != nil {
		return ToolResult{}, err
	}
	if err := t.Router.WriteFile(ctx, h, a.Path, []byte(content)); err != nil {
		return sandboxUnavailableResult("fs_edit", err), nil
	}
	return NewResult(ctx, fmt.Sprintf("replaced %d occurrence(s) in %s", count, a.Path))
}
