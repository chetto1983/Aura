package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// SearchFiles is a Go port of hermes-agent's tools/file_tools.py search_files (schema at
// file_tools.py:2242, handler at file_tools.py:2305, implementation at file_operations.py's
// ShellFileOperations.search). It replaces Aura's fs_glob + fs_grep — hermes folds "find files by
// name" and "search file contents" into ONE tool behind a `target` enum, and a model that reused
// fs_glob's `**` glob semantics on fs_grep already expected that unification (AG-046). It searches
// INSIDE the caller's per-identity box. There is no host arm: a box that cannot be reached fails
// CLOSED (D-09/GATE-01).
type SearchFiles struct {
	Router *usersandbox.SandboxRouter
}

type searchFilesArgs struct {
	Pattern    string `json:"pattern"`
	Target     string `json:"target"`
	Path       string `json:"path"`
	FileGlob   string `json:"file_glob"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
	OutputMode string `json:"output_mode"`
	Context    int    `json:"context"`
}

// defaultSearchLimit mirrors file_operations.DEFAULT_SEARCH_LIMIT (50) — enough for a first look
// without flooding the turn; offset pages past it.
const defaultSearchLimit = 50

func (t *SearchFiles) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "RE2 regular expression for content search (target=content), or a glob pattern (e.g. '*.go', '**/*.py') for file search (target=files)."},
    "target": {"type": "string", "enum": ["content", "files"], "default": "content", "description": "'content' searches inside file contents (grep); 'files' finds files by name (find/ls)."},
    "path": {"type": "string", "description": "Directory or file to search inside your workspace container. Defaults to /workspace."},
    "file_glob": {"type": "string", "description": "Content-search only: restrict to files matching this glob (e.g. '*.go', '**/*.py')."},
    "limit": {"type": "integer", "minimum": 1, "default": 50, "description": "Maximum results to return (default 50)."},
    "offset": {"type": "integer", "minimum": 0, "default": 0, "description": "Skip the first N results, for pagination (default 0)."},
    "output_mode": {"type": "string", "enum": ["content", "files_only", "count"], "default": "content", "description": "Content-search only. 'content': matching lines with line numbers. 'files_only': just the file paths that matched. 'count': match count per file."},
    "context": {"type": "integer", "minimum": 0, "default": 0, "description": "Content-search only: lines of context before and after each match."}
  },
  "required": ["pattern"]
}`)
	return Spec{
		Name:    "search_files",
		Summary: "Search file contents or find files by name.",
		Description: "Search inside your workspace container. Use this instead of grep/rg/find/ls in " +
			"shell_exec.\n\n" +
			"Content search (target='content', default): pattern is an RE2 regex matched against file " +
			"contents. output_mode picks the shape: 'content' (default) returns matching lines as " +
			"'path:line: text'; 'files_only' returns just the file paths; 'count' returns a match count per " +
			"file. file_glob restricts which files are searched (a plain glob like '*.go' matches the " +
			"filename, a path glob with ** like '**/*.go' matches the path and crosses directories). context " +
			"adds N lines before/after each match.\n\n" +
			"File search (target='files'): pattern is a glob over forward-slash paths — '*'/'?' within a " +
			"segment, '**' to cross directories. Also use this instead of ls: results are sorted by " +
			"modification time, newest first.\n\n" +
			"Both targets: path optionally restricts the root (file or directory, default /workspace); " +
			"binary files, hidden dot-directories (.git, .cache, ...) and node_modules/vendor/__pycache__ are " +
			"skipped (pass a hidden/vendored tree as the explicit path to search it anyway); limit/offset " +
			"page through results (default limit 50). Example: {\"pattern\":\"TODO\",\"file_glob\":\"*.go\"}, or " +
			"{\"pattern\":\"**/*.go\",\"target\":\"files\"}.",
		Parameters: params,
		// Always visible, with read_file/write_file/patch: finding a file is the step before
		// every other file operation, so deferring it taxed the common path.
		Deferred: false,
	}
}

func (t *SearchFiles) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a searchFilesArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("search_files args: %w", err)
	}
	if strings.TrimSpace(a.Pattern) == "" {
		return ToolResult{}, fmt.Errorf("search_files: pattern is required")
	}
	target := a.Target
	if target == "" {
		target = "content"
	}
	if target != "content" && target != "files" {
		return ToolResult{}, fmt.Errorf("search_files: target must be 'content' or 'files', got %q", target)
	}
	outputMode := a.OutputMode
	if outputMode == "" {
		outputMode = "content"
	}
	if outputMode != "content" && outputMode != "files_only" && outputMode != "count" {
		return ToolResult{}, fmt.Errorf("search_files: output_mode must be 'content', 'files_only' or 'count', got %q", outputMode)
	}
	limit := a.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	offset := a.Offset
	if offset < 0 {
		offset = 0
	}
	context := a.Context
	if context < 0 {
		context = 0
	}
	root, err := boxPathArg("search_files", a.Path)
	if err != nil {
		return ToolResult{}, err
	}

	// Pattern compilation and argument normalization run BEFORE the route on purpose: an invalid
	// regex/glob is the model's own error and must read as one, not as a box round-trip that
	// failed obscurely.
	boxHandle, routeErr := t.Router.Route(ctx)
	if routeErr != nil {
		return sandboxUnavailableResult("search_files", routeErr), nil
	}

	if target == "files" {
		return t.searchFileNames(ctx, boxHandle, root, a.Pattern, limit, offset)
	}
	re, err := regexp.Compile(a.Pattern)
	if err != nil {
		return ToolResult{}, fmt.Errorf("search_files: invalid pattern: %w", err)
	}
	return t.searchContent(ctx, boxHandle, root, a.FileGlob, re, outputMode, limit, offset, context)
}

// paginateStrings applies offset/limit over an already-ordered slice, mirroring
// normalize_search_pagination's contract (offset/limit are clamped by the caller, never here).
func paginateStrings(items []string, offset, limit int) []string {
	if offset >= len(items) {
		return nil
	}
	end := min(offset+limit, len(items))
	return items[offset:end]
}
