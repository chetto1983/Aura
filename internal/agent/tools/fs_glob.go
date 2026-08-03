package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// FSGlob finds files by name pattern (supporting **) across the tree INSIDE the caller's
// per-identity box. There is no host arm: a box that cannot be reached fails CLOSED
// (D-09/GATE-01). Only the ENUMERATION happens in the box — the pattern is compiled and matched
// by Go's own regexp, so the tool's semantics never become the box shell's semantics. This file
// deliberately imports neither io/fs nor path/filepath.
type FSGlob struct {
	Router *usersandbox.SandboxRouter
}

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
    "path": {"type": "string", "description": "Optional root directory to search inside your workspace container. Defaults to /workspace."},
    "max_results": {"type": "integer", "minimum": 1, "description": "Optional cap on paths returned (default 500)."}
  },
  "required": ["pattern"]
}`)
	return Spec{
		Name:        "fs_glob",
		Summary:     "Find files by name pattern.",
		Description: "Find files by NAME across a directory tree; returns matching paths, sorted. `pattern` is a glob over forward-slash paths — `*` and `?` within a path segment, `**` to cross directories (e.g. `**/*.go`, `cmd/*/main.go`); optionally set a `path` root (default workspace). Hidden dot-directories (.git, .cache, …) and node_modules/vendor/__pycache__ are skipped — to search a hidden or vendored tree pass it as the explicit `path`; results cap at max_results (default 500). Use this to locate files by name; use fs_grep to search their contents.",
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
	root, err := boxPathArg("fs_glob", a.Path)
	if err != nil {
		return ToolResult{}, err
	}

	// Pattern compilation, the result cap and the root resolution run BEFORE the route: an invalid
	// glob is the model's own error and must read as one, not as a box round-trip that failed
	// obscurely.
	boxHandle, routeErr := t.Router.Route(ctx)
	if routeErr != nil {
		return sandboxUnavailableResult("fs_glob", routeErr), nil
	}
	return t.globInBox(ctx, boxHandle, root, re, maxResults)
}

// globInBox matches the compiled pattern against the box's file list. The enumeration is already
// bounded by the node cap, so the result cap is applied while filtering.
func (t *FSGlob) globInBox(
	ctx context.Context,
	handle usersandbox.BoxHandle,
	root string,
	re *regexp.Regexp,
	maxResults int,
) (ToolResult, error) {
	rels, truncated, err := boxListFiles(ctx, t.Router, handle, root)
	if err != nil {
		return sandboxUnavailableResult("fs_glob", err), nil
	}
	var out []string
	for _, rel := range rels {
		if re.MatchString(rel) {
			out = append(out, rel)
		}
		if len(out) >= maxResults {
			break
		}
	}
	if len(out) == 0 {
		return NewResult(ctx, withWalkTruncation("[no matches]", truncated))
	}
	sort.Strings(out)
	return NewResult(ctx, withWalkTruncation(strings.Join(out, "\n"), truncated))
}
