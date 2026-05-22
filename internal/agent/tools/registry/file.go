package tools

import (
	"context"
	"log/slog"
	"strings"

	"github.com/aura/aura/internal/workspace"
)

// FileTool consolidates the workspace verb-tools (list_files, read_file,
// search_files, write_file, apply_patch) into a single action-enum
// surface. Same picobot pattern as wiki_page / dev_tool / task / web /
// doc — one tool, one action enum acting as the verb.
//
//	action=list   — directory listing with limit
//	action=read   — file content with truncation + base64 fallback for binaries
//	action=search — case-insensitive substring across the workspace, glob filter
//	action=write  — atomic UTF-8 write (tmp+rename via kernel-anchored Root)
//	action=patch  — find/replace one or all occurrences inside a file
//
// All operations route through internal/workspace.Root, which now uses
// *os.Root (Go 1.24+) for kernel-enforced containment. Sensitive paths
// (.env, .git, *.db, wiki/raw, secrets/) and binary/executable
// extensions are denied at the workspace layer — the kernel only knows
// where the workspace ENDS, the denylist knows what is forbidden inside.
type FileTool struct {
	list   *ListFilesTool
	read   *ReadFileTool
	search *SearchFilesTool
	write  *WriteFileTool
	patch  *ApplyPatchTool
}

// NewFileTool wires the consolidated tool against a workspace root.
// Returns nil when root is nil so callers can compose registration
// unconditionally.
func NewFileTool(root *workspace.Root) *FileTool {
	if root == nil {
		return nil
	}
	return &FileTool{
		list:   NewListFilesTool(root),
		read:   NewReadFileTool(root),
		search: NewSearchFilesTool(root),
		write:  NewWriteFileTool(root),
		patch:  NewApplyPatchTool(root),
	}
}

func (t *FileTool) Name() string { return "file" }

func (t *FileTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:            t.Name(),
		Description:     t.Description(),
		Parameters:      t.Parameters(),
		DestructiveHint: true, // write/patch actions can overwrite workspace files
		VisibilityTier:  VisibilityDeferred,
		// file action=read may legitimately return a 50 KB page the user asked
		// for verbatim — use a wider byte cap than the default 8192.
		OutputCap: OutputCap{MaxBytes: 16384},
	}
}

func (t *FileTool) Description() string {
	return "Read, write, list, search, or patch files in Aura's workspace. action=read Returns file bytes, 16384-byte cap. Actions: list, read, search, write, patch. Required: varies by action."
}

func (t *FileTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action"},
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "read", "search", "write", "patch"},
				"description": "Which filesystem operation to perform.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Relative path. Required for read/write/patch. Optional for list (default root). Ignored for search (use globs).",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "list: max entries (default 200, max 1000). search: max matches (default 50, max 200). Ignored for other actions.",
			},
			"max_bytes": map[string]any{
				"type":        "integer",
				"description": "read only: max bytes to return (default 65536, max 524288). Oversize files are truncated.",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "write only: UTF-8 file content.",
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": "search only: case-insensitive substring to find.",
			},
			"globs": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "search only, optional: relative glob filters (e.g. ['**/*.go', '*.md']).",
			},
			"old": map[string]any{
				"type":        "string",
				"description": "patch only: exact text to replace.",
			},
			"new": map[string]any{
				"type":        "string",
				"description": "patch only: replacement text. Empty string deletes the match.",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "patch only: replace every occurrence (default false; non-unique match errors otherwise).",
			},
		},
		"oneOf": ActionDispatchOneOf([]ActionVariant{
			{Name: "list", RequiredKeys: nil},
			{Name: "read", RequiredKeys: []string{"path"}},
			{Name: "search", RequiredKeys: []string{"pattern"}},
			{Name: "write", RequiredKeys: []string{"path", "content"}},
			{Name: "patch", RequiredKeys: []string{"path", "old", "new"}},
		}),
	}
}

// fileValidActions + fileActionHints are package-level so the
// ActionRequiredError / UnknownActionError helpers stay consistent
// across invocations. The hint list orders most-specific first so the
// scorer prefers a multi-key match (patch over read) when both could
// fit.
var (
	fileValidActions = []string{"list", "read", "search", "write", "patch"}
	fileActionHints  = []ActionHint{
		{Name: "patch", RequiredKeys: []string{"old", "new"}},
		{Name: "search", RequiredKeys: []string{"pattern"}},
		{Name: "write", RequiredKeys: []string{"content"}},
		{Name: "read", RequiredKeys: []string{"path"}},
	}
)

func (t *FileTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	action := strings.TrimSpace(stringArg(args, "action"))
	switch action {
	case "list":
		return t.list.Execute(ctx, args)
	case "read":
		return t.read.Execute(ctx, args)
	case "search":
		return t.search.Execute(ctx, args)
	case "write":
		return t.write.Execute(ctx, args)
	case "patch":
		return t.patch.Execute(ctx, args)
	case "":
		inferred, score, ambiguous := fileInferAction(args)
		if !ambiguous && score > 0 {
			slog.Default().Debug("file: action inferred from arg shape", "action", inferred)
			switch inferred {
			case "write":
				return t.write.Execute(ctx, args)
			case "read":
				return t.read.Execute(ctx, args)
			case "search":
				return t.search.Execute(ctx, args)
			case "patch":
				return t.patch.Execute(ctx, args)
			}
		}
		return "", ActionRequiredError("file", fileValidActions, args, fileActionHints, "list")
	default:
		return "", UnknownActionError("file", action, fileValidActions, args)
	}
}

// fileInferAction infers the intended action from arg shape when the caller
// omitted the "action" field. Returns score>0 and ambiguous=false for a
// confident single-winner inference; returns ambiguous=true when args are
// contradictory (e.g. "old" without "new"); returns score=0 when no
// recognisable args are present.
//
// Priority order (most-specific first):
//
//	patch  — old+new both present  (score 2)
//	write  — content present        (score 1, path is optional)
//	search — pattern present        (score 1)
//	read   — path present only      (score 1)
func fileInferAction(args map[string]any) (action string, score int, ambiguous bool) {
	has := func(k string) bool { _, ok := args[k]; return ok }
	hasOld, hasNew := has("old"), has("new")

	// partial patch (exactly one of old/new): unambiguously wrong, let caller error
	if hasOld != hasNew {
		return "", 0, true
	}
	if hasOld && hasNew {
		return "patch", 2, false
	}
	if has("content") {
		return "write", 1, false
	}
	if has("pattern") {
		return "search", 1, false
	}
	if has("path") {
		return "read", 1, false
	}
	return "", 0, false
}
