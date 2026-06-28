package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// FSGlob finds files by name pattern (supporting **) across a directory tree.
type FSGlob struct{ WorkspaceRoot string }

type fsGlobArgs struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path"`
	MaxResults int    `json:"max_results"`
}

const defaultGlobMax = 500

func (t *FSGlob) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "Glob pattern over forward-slash paths, e.g. '**/*.go' or 'cmd/*/main.go'. ** crosses directories."},
    "path": {"type": "string", "description": "Optional root directory to search. Defaults to the workspace root."},
    "max_results": {"type": "integer", "minimum": 1, "description": "Optional cap on paths returned (default 500)."}
  },
  "required": ["pattern"]
}`)
	return Spec{
		Name:        "fs_glob",
		Summary:     "Find files by name pattern.",
		Description: "Find files by NAME across a directory tree; returns matching paths, sorted. `pattern` is a glob over forward-slash paths — `*` and `?` within a path segment, `**` to cross directories (e.g. `**/*.go`, `cmd/*/main.go`); optionally set a `path` root (default workspace). .git/node_modules/vendor are skipped; results cap at max_results (default 500). Use this to locate files by name; use fs_grep to search their contents.",
		Parameters:  params,
		// Deferred: filesystem search is a long-tail capability discoverable via tool_search.
		// Keeping only fs_read/fs_write visible trims the manifest and stops the agent
		// defaulting to fs_glob/fs_grep for uploaded-document questions (use document_search).
		Deferred: true,
	}
}

func (t *FSGlob) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a fsGlobArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("fs_glob args: %w", err)
	}
	if strings.TrimSpace(a.Pattern) == "" {
		return ToolResult{}, fmt.Errorf("fs_glob: pattern is required")
	}
	re, err := globToRegexp(a.Pattern)
	if err != nil {
		return ToolResult{}, fmt.Errorf("fs_glob: invalid pattern: %w", err)
	}
	maxResults := a.MaxResults
	if maxResults <= 0 {
		maxResults = defaultGlobMax
	}
	root := rootOrDefault(t.WorkspaceRoot, a.Path)

	budget := newWalkBudget(ctx)
	var truncated bool
	var out []string
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if budget.step() {
			truncated = true
			return filepath.SkipAll
		}
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipWalkDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			rel = p
		}
		if re.MatchString(filepath.ToSlash(rel)) {
			out = append(out, filepath.ToSlash(rel))
		}
		if len(out) >= maxResults {
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return ToolResult{}, fmt.Errorf("fs_glob: %w", walkErr)
	}
	if len(out) == 0 {
		return NewResult(ctx, withWalkTruncation("[no matches]", truncated))
	}
	sort.Strings(out)
	return NewResult(ctx, withWalkTruncation(strings.Join(out, "\n"), truncated))
}
