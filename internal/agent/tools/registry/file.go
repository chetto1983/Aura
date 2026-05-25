package tools

import (
	"context"
	"log/slog"
	"strings"

	"github.com/aura/aura/internal/workspace"
)

// FileTool consolidates the workspace verb-tools into a single action-enum
// surface. Same picobot pattern as search / wiki_page / task / web /
// doc — one tool, one action enum acting as the verb.
//
//	action=list    — directory listing with limit
//	action=read    — file content with truncation + base64 fallback for binaries
//	action=search  — case-insensitive substring across the workspace, glob filter
//	action=write   — atomic UTF-8 write (tmp+rename via kernel-anchored Root)
//	action=patch   — find/replace one or all occurrences inside a file
//	action=remove  — delete a file (empty dir with allow_dir=true)
//	action=move    — rename or relocate within the workspace
//	action=copy    — duplicate a file (no recursive directory copy)
//	action=tree    — nested directory walk with depth + entry caps
//	action=info    — stat-style metadata; returns exists=false for missing
//	action=resolve — find files by basename (case-insensitive)
//
// All operations route through internal/workspace.Root, which uses
// *os.Root (Go 1.24+) for kernel-enforced containment. Sensitive paths
// (.env, .git, *.db, wiki/raw, secrets/) and binary/executable
// extensions are denied at the workspace layer — the kernel only knows
// where the workspace ENDS, the denylist knows what is forbidden inside.
type FileTool struct {
	list    *ListFilesTool
	read    *ReadFileTool
	search  *SearchFilesTool
	write   *WriteFileTool
	patch   *ApplyPatchTool
	remove  *RemoveFileTool
	move    *MoveFileTool
	copyOp  *CopyFileTool
	tree    *TreeFilesTool
	info    *InfoFileTool
	resolve *ResolveFileTool
}

// NewFileTool wires the consolidated tool against a workspace root.
// Returns nil when root is nil so callers can compose registration
// unconditionally.
func NewFileTool(root *workspace.Root) *FileTool {
	if root == nil {
		return nil
	}
	return &FileTool{
		list:    NewListFilesTool(root),
		read:    NewReadFileTool(root),
		search:  NewSearchFilesTool(root),
		write:   NewWriteFileTool(root),
		patch:   NewApplyPatchTool(root),
		remove:  NewRemoveFileTool(root),
		move:    NewMoveFileTool(root),
		copyOp:  NewCopyFileTool(root),
		tree:    NewTreeFilesTool(root),
		info:    NewInfoFileTool(root),
		resolve: NewResolveFileTool(root),
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
	return `Manipulate workspace files: list/read/search/write/patch/remove/move/copy/tree/info/resolve. action=read Returns file bytes, 16384-byte cap. REQUIRED action; write/patch/remove/move/copy can modify files.`
}

func (t *FileTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action"},
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"list", "read", "search", "write", "patch", "remove", "move", "copy", "tree", "info", "resolve"},
				"description": `REQUIRED. "list"=directory listing, "read"=file bytes, "search"=substring across workspace, "write"=create/overwrite, "patch"=find/replace inside a file, "remove"=delete a file (or empty dir with allow_dir=true), "move"=rename/relocate, "copy"=duplicate a file, "tree"=nested walk with depth caps, "info"=stat metadata, "resolve"=find paths by basename.`,
			},
			"path": map[string]any{
				"type":        "string",
				"description": `Required for "read"/"write"/"patch"/"remove"/"info". Optional for "list"/"tree" (default workspace root). Ignored for "search"/"move"/"copy"/"resolve".`,
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": `Optional. action="list": max entries (default 200, max 1000). action="search": max matches (default 50, max 200). action="resolve": max matches (default 50, max 500).`,
			},
			"max_bytes": map[string]any{
				"type":        "integer",
				"description": `Optional, action="read" only. Max bytes to return (default 65536, max 524288). Oversize files are truncated.`,
			},
			"content": map[string]any{
				"type":        "string",
				"description": `Required when action="write". UTF-8 file content.`,
			},
			"pattern": map[string]any{
				"type":        "string",
				"description": `Required when action="search". Case-insensitive substring to find.`,
			},
			"globs": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": `Optional, action="search" only. Relative glob filters (e.g. ["**/*.go", "*.md"]).`,
			},
			"old": map[string]any{
				"type":        "string",
				"description": `Required when action="patch". Exact text to replace.`,
			},
			"new": map[string]any{
				"type":        "string",
				"description": `Required when action="patch". Replacement text. Empty string deletes the match.`,
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": `Optional, action="patch" only. Replace every occurrence (default false; non-unique match errors otherwise).`,
			},
			"src": map[string]any{
				"type":        "string",
				"description": `Required for action="move"/"copy". Source path inside the workspace.`,
			},
			"dst": map[string]any{
				"type":        "string",
				"description": `Required for action="move"/"copy". Destination path inside the workspace. Parent directories are created as needed.`,
			},
			"allow_dir": map[string]any{
				"type":        "boolean",
				"description": `Optional, action="remove" only. When true, an EMPTY directory can be removed. Default false (files only).`,
			},
			"max_depth": map[string]any{
				"type":        "integer",
				"description": `Optional, action="tree" only. Max recursion depth (default 4, max 16). Deeper levels are marked truncated.`,
			},
			"max_entries": map[string]any{
				"type":        "integer",
				"description": `Optional, action="tree" only. Max total entries visited (default 200, max 2000).`,
			},
			"name": map[string]any{
				"type":        "string",
				"description": `Required when action="resolve". Basename to match (case-insensitive, exact match).`,
			},
		},
		"oneOf": ActionDispatchOneOf([]ActionVariant{
			{Name: "list", RequiredKeys: nil},
			{Name: "read", RequiredKeys: []string{"path"}},
			{Name: "search", RequiredKeys: []string{"pattern"}},
			{Name: "write", RequiredKeys: []string{"path", "content"}},
			{Name: "patch", RequiredKeys: []string{"path", "old", "new"}},
			{Name: "remove", RequiredKeys: []string{"path"}},
			{Name: "move", RequiredKeys: []string{"src", "dst"}},
			{Name: "copy", RequiredKeys: []string{"src", "dst"}},
			{Name: "tree", RequiredKeys: nil},
			{Name: "info", RequiredKeys: []string{"path"}},
			{Name: "resolve", RequiredKeys: []string{"name"}},
		}),
		// JSON Schema "examples" - concrete shapes models read before
		// the description, reducing action-field omissions.
		"examples": []any{
			map[string]any{"action": "list", "path": "wiki"},
			map[string]any{"action": "read", "path": "AGENT.md"},
			map[string]any{"action": "search", "pattern": "TODO", "globs": []string{"**/*.go"}},
			map[string]any{"action": "write", "path": "notes/draft.md", "content": "# Title\nbody..."},
			map[string]any{"action": "patch", "path": "notes/draft.md", "old": "old line", "new": "new line"},
			map[string]any{"action": "remove", "path": "notes/scratch.md"},
			map[string]any{"action": "move", "src": "notes/draft.md", "dst": "notes/final.md"},
			map[string]any{"action": "copy", "src": "notes/final.md", "dst": "notes/final.bak.md"},
			map[string]any{"action": "tree", "path": "wiki", "max_depth": 3},
			map[string]any{"action": "info", "path": "AGENTS.md"},
			map[string]any{"action": "resolve", "name": "AGENTS.md"},
		},
	}
}

// fileValidActions + fileActionHints are package-level so the
// ActionRequiredError / UnknownActionError helpers stay consistent
// across invocations. The hint list orders most-specific first so the
// scorer prefers a multi-key match (patch over read) when both could
// fit.
var (
	fileValidActions = []string{"list", "read", "search", "write", "patch", "remove", "move", "copy", "tree", "info", "resolve"}
	fileActionHints  = []ActionHint{
		{Name: "patch", RequiredKeys: []string{"old", "new"}},
		{Name: "move", RequiredKeys: []string{"src", "dst"}},
		{Name: "copy", RequiredKeys: []string{"src", "dst"}},
		{Name: "search", RequiredKeys: []string{"pattern"}},
		{Name: "write", RequiredKeys: []string{"content"}},
		{Name: "resolve", RequiredKeys: []string{"name"}},
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
	case "remove":
		return t.remove.Execute(ctx, args)
	case "move":
		return t.move.Execute(ctx, args)
	case "copy":
		return t.copyOp.Execute(ctx, args)
	case "tree":
		return t.tree.Execute(ctx, args)
	case "info":
		return t.info.Execute(ctx, args)
	case "resolve":
		return t.resolve.Execute(ctx, args)
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
			case "move":
				return t.move.Execute(ctx, args)
			case "copy":
				return t.copyOp.Execute(ctx, args)
			case "resolve":
				return t.resolve.Execute(ctx, args)
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
//	patch   — old+new both present     (score 2)
//	move    — src+dst both present     (score 2; tie-break: dispatch routes to move)
//	resolve — name present              (score 1)
//	write   — content present           (score 1, path is optional)
//	search  — pattern present           (score 1)
//	read    — path present only         (score 1)
func fileInferAction(args map[string]any) (action string, score int, ambiguous bool) {
	has := func(k string) bool { _, ok := args[k]; return ok }
	hasOld, hasNew := has("old"), has("new")
	hasSrc, hasDst := has("src"), has("dst")

	// partial patch / partial move: unambiguously wrong, let caller error
	if hasOld != hasNew {
		return "", 0, true
	}
	if hasSrc != hasDst {
		return "", 0, true
	}
	if hasOld && hasNew {
		return "patch", 2, false
	}
	if hasSrc && hasDst {
		// copy vs. move is genuinely ambiguous without action; default to move
		// (rename is irreversible-but-bounded; copy creates a new artifact).
		// Callers that want copy must pass action="copy" explicitly.
		return "move", 2, false
	}
	if has("name") {
		return "resolve", 1, false
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
