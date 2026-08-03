package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// FSEdit replaces an exact string in a file INSIDE the caller's per-identity box. Like Claude
// Code's Edit, old_string must match exactly and be unique unless replace_all is set. There is no
// host arm: a box that cannot be reached fails CLOSED (D-09/GATE-01), and the skills-library fence
// (#54 / D-43) is box-relative over the literal /skills mount. This file deliberately imports
// neither os nor path/filepath — their absence is a compile-time proof that no host write survives.
type FSEdit struct {
	Router *usersandbox.SandboxRouter
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
    "path": {"type": "string", "description": "File path to edit inside your workspace container (absolute, or relative to /workspace)."},
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
	boxPath, err := boxPathArg("fs_edit", a.Path)
	if err != nil {
		return ToolResult{}, err
	}
	if deniedBoxSkillsWrite(boxPath) {
		return ToolResult{}, fmt.Errorf("fs_edit: %s is inside the sandbox skills mount; author skills through the gated `skill` tool "+
			"(action=create/update/delete), not direct file edits", boxPath)
	}

	// Every guard above is argument-based and runs BEFORE the route on purpose: a malformed edit
	// or a fenced target is the model's own error, not a sandbox outage, and must read as one —
	// and the skills fence must refuse without touching the box.
	boxHandle, routeErr := t.Router.Route(ctx)
	if routeErr != nil {
		return sandboxUnavailableResult("fs_edit", routeErr), nil
	}
	return t.editInBox(ctx, boxHandle, boxPath, a)
}

// applyExactEdit is the ONE implementation of fs_edit's match rules. label names the file in the
// errors. Returns the edited content and the number of occurrences replaced (exactly 1 unless
// replace_all).
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

// editInBox performs the replacement INSIDE the box: it reads through boxReadFileCapped — the same
// bounded read fs_read uses, so both tools share one cap, one binary refusal and one
// missing-file-is-not-an-outage classification — applies the shared match rules, and writes back
// through the router's tar copy-in.
//
// The mode and atomicity guarantees the deleted host arm had (F-010, AG-045) do not survive this
// path; FSWrite's doc comment records why and what restoring them would cost.
func (t *FSEdit) editInBox(ctx context.Context, h usersandbox.BoxHandle, boxPath string, a fsEditArgs) (ToolResult, error) {
	b, deny, err := boxReadFileCapped(ctx, t.Router, h, "fs_edit", boxPath)
	if deny != nil {
		return *deny, nil
	}
	if err != nil {
		return ToolResult{}, err
	}
	content, count, err := applyExactEdit(string(b), a, boxPath)
	if err != nil {
		return ToolResult{}, err
	}
	if err := t.Router.WriteFile(ctx, h, boxPath, []byte(content)); err != nil {
		return sandboxUnavailableResult("fs_edit", err), nil
	}
	return NewResult(ctx, fmt.Sprintf("replaced %d occurrence(s) in %s", count, boxPath))
}
