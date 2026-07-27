package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// FSGlob finds files by name pattern (supporting **) across a directory tree.
//
// Router mirrors fs_read/fs_write: under a strict profile the tree walked is the one INSIDE the
// per-identity box, never the host's. Only the ENUMERATION moves — the pattern is still compiled
// and matched by the same Go code on both paths, so a box search cannot answer differently from
// a host search for the same tree (fix plan 2.5).
type FSGlob struct {
	WorkspaceRoot string
	Router        *usersandbox.SandboxRouter
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
    "path": {"type": "string", "description": "Optional root directory to search. Defaults to the workspace root."},
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
	// Route decision BEFORE the host root is resolved or walked: routed ⇒ enumerate in-box;
	// routed+err ⇒ deny (fail-CLOSED, D-09/GATE-01), never a host walk fallback.
	boxHandle, routed, routeErr := t.Router.Route(ctx)
	if routed && routeErr != nil {
		return sandboxUnavailableResult("fs_glob", routeErr), nil
	}
	if routed {
		return t.globInBox(ctx, boxHandle, a, re, maxResults)
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
			if p != root && skipWalkDir(d.Name()) {
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

// globInBox matches the SAME compiled pattern against the box's file list. The host branch above
// caps results mid-walk; here the enumeration is already bounded by the node cap, so the cap is
// applied while filtering. Nothing on this path touches the host filesystem.
func (t *FSGlob) globInBox(
	ctx context.Context,
	handle usersandbox.BoxHandle,
	a fsGlobArgs,
	re *regexp.Regexp,
	maxResults int,
) (ToolResult, error) {
	rels, truncated, err := boxListFiles(ctx, t.Router, handle, boxRootOrDefault(a.Path))
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
