package tools

import (
	"context"
	"log/slog"
	"strings"

	"github.com/aura/aura/internal/workspace"
)

// FileTool consolidates the workspace verb-tools (list_files, read_file,
// search_files, write_file, apply_patch) into a single action-enum
// surface. Same picobot pattern as search / wiki_page / task / web /
// create_document -- one tool, one action enum acting as the verb.
//
//	action=list   -- directory listing with limit
//	action=read   -- file content with truncation + base64 fallback for binaries
//	action=search -- case-insensitive substring across the workspace, glob filter
//	action=write  -- atomic UTF-8 write (tmp+rename via kernel-anchored Root)
//	action=patch  -- find/replace one or all occurrences inside a file
//
// All operations route through internal/workspace.Root, which now uses
// *os.Root (Go 1.24+) for kernel-enforced containment. Sensitive paths
// (.env, .git, *.db, wiki/raw, secrets/) and binary/executable
// extensions are denied at the workspace layer -- the kernel only knows
// where the workspace ENDS, the denylist knows what is forbidden inside.
type FileTool struct {
	root   *workspace.Root
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
		root:   root,
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
	return `Operate on workspace files. action=read Returns file bytes, 16384-byte cap. Actions: list/read/search/write/patch/grep/path_info/mkdir/rmdir/remove_file/move/copy/walk/pwd. Write, patch, move, remove_file, and rmdir can modify files.`
}

func (t *FileTool) Parameters() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"action"},
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{
					"list", "read", "search", "write", "patch",
					"grep", "path_info", "mkdir", "rmdir", "remove_file",
					"move", "copy", "walk", "pwd",
				},
				"description": `REQUIRED. "list" = directory listing, "read" = file bytes, "search" = substring across workspace, "write" = create/overwrite, "patch" = find/replace, "grep" = search inside one file, "path_info" = metadata, "mkdir"/"rmdir" = create/remove empty directory, "remove_file" = delete a file, "move"/"copy" = transfer files or directories, "walk" = tree, "pwd" = workspace root marker.`,
			},
			"path": map[string]any{
				"type":        "string",
				"description": `Required for "read"/"write"/"patch"/"grep"/"path_info"/"mkdir"/"rmdir"/"remove_file"/"walk". Optional for "list" (defaults to workspace root). Ignored for "search"/"move"/"copy"/"pwd".`,
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": `Optional. action="list": max entries (default 200, max 1000). action="search"/"grep": max matches (default 50, max 200). action="walk": max nodes (default 500, max 5000).`,
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
			"search_text": map[string]any{
				"type":        "string",
				"description": `Required when action="grep". Text to find inside the file at path.`,
			},
			"case_insensitive": map[string]any{
				"type":        "boolean",
				"description": `Optional, action="grep" only. Match search_text without case sensitivity.`,
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
				"description": `Required when action="move" or action="copy". Source path inside the workspace.`,
			},
			"dst": map[string]any{
				"type":        "string",
				"description": `Required when action="move" or action="copy". Destination path inside the workspace. Existing destinations are not overwritten.`,
			},
		},
		"oneOf": ActionDispatchOneOf([]ActionVariant{
			{Name: "list", RequiredKeys: nil},
			{Name: "read", RequiredKeys: []string{"path"}},
			{Name: "search", RequiredKeys: []string{"pattern"}},
			{Name: "write", RequiredKeys: []string{"path", "content"}},
			{Name: "patch", RequiredKeys: []string{"path", "old", "new"}},
			{Name: "grep", RequiredKeys: []string{"path", "search_text"}},
			{Name: "path_info", RequiredKeys: []string{"path"}},
			{Name: "mkdir", RequiredKeys: []string{"path"}},
			{Name: "rmdir", RequiredKeys: []string{"path"}},
			{Name: "remove_file", RequiredKeys: []string{"path"}},
			{Name: "move", RequiredKeys: []string{"src", "dst"}},
			{Name: "copy", RequiredKeys: []string{"src", "dst"}},
			{Name: "walk", RequiredKeys: []string{"path"}},
			{Name: "pwd", RequiredKeys: nil},
		}),
		// JSON Schema "examples" - concrete shapes models read before
		// the description, reducing action-field omissions.
		"examples": []any{
			map[string]any{"action": "list", "path": "wiki"},
			map[string]any{"action": "read", "path": "AGENT.md"},
			map[string]any{"action": "search", "pattern": "TODO", "globs": []string{"**/*.go"}},
			map[string]any{"action": "write", "path": "notes/draft.md", "content": "# Title\nbody..."},
			map[string]any{"action": "patch", "path": "notes/draft.md", "old": "old line", "new": "new line"},
			map[string]any{"action": "grep", "path": "notes/draft.md", "search_text": "TODO", "case_insensitive": true},
			map[string]any{"action": "path_info", "path": "notes/draft.md"},
			map[string]any{"action": "mkdir", "path": "notes/archive"},
			map[string]any{"action": "rmdir", "path": "notes/empty"},
			map[string]any{"action": "remove_file", "path": "notes/old.md"},
			map[string]any{"action": "move", "src": "notes/draft.md", "dst": "notes/archive/draft.md"},
			map[string]any{"action": "copy", "src": "notes/archive/draft.md", "dst": "notes/draft-copy.md"},
			map[string]any{"action": "walk", "path": "notes"},
			map[string]any{"action": "pwd"},
		},
	}
}

// fileValidActions + fileActionHints are package-level so the
// ActionRequiredError / UnknownActionError helpers stay consistent
// across invocations. The hint list orders most-specific first so the
// scorer prefers a multi-key match (patch over read) when both could
// fit.
var (
	fileValidActions = []string{
		"list", "read", "search", "write", "patch",
		"grep", "path_info", "mkdir", "rmdir", "remove_file",
		"move", "copy", "walk", "pwd",
	}
	fileActionHints = []ActionHint{
		{Name: "patch", RequiredKeys: []string{"old", "new"}},
		{Name: "grep", RequiredKeys: []string{"path", "search_text"}},
		{Name: "search", RequiredKeys: []string{"pattern"}},
		{Name: "write", RequiredKeys: []string{"content"}},
		{Name: "move", RequiredKeys: []string{"src", "dst"}},
		{Name: "copy", RequiredKeys: []string{"src", "dst"}},
		{Name: "mkdir", RequiredKeys: []string{"path"}},
		{Name: "rmdir", RequiredKeys: []string{"path"}},
		{Name: "remove_file", RequiredKeys: []string{"path"}},
		{Name: "path_info", RequiredKeys: []string{"path"}},
		{Name: "walk", RequiredKeys: []string{"path"}},
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
	case "grep":
		return t.executeGrep(ctx, args)
	case "path_info":
		return t.executePathInfo(ctx, args)
	case "mkdir":
		return t.executeMkdir(ctx, args)
	case "rmdir":
		return t.executeRmdir(ctx, args)
	case "remove_file":
		return t.executeRemoveFile(ctx, args)
	case "move":
		return t.executeMove(ctx, args)
	case "copy":
		return t.executeCopy(ctx, args)
	case "walk":
		return t.executeWalk(ctx, args)
	case "pwd":
		return t.executePWD(ctx, args)
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
			case "grep":
				return t.executeGrep(ctx, args)
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
//	grep   — path+search_text       (score 2)
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
	if has("path") && has("search_text") {
		return "grep", 2, false
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
