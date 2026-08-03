package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	pathpkg "path"
	"regexp"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// FSGrep searches file contents for a regexp across the tree INSIDE the caller's per-identity
// box. There is no host arm: a box that cannot be reached fails CLOSED (D-09/GATE-01). Only the
// file READ happens in the box — the pattern is compiled and matched by Go's RE2, so handing a
// pattern to the box's GNU grep can never quietly swap the regexp dialect underneath the tool.
// This file deliberately imports neither os, io/fs nor path/filepath.
type FSGrep struct {
	Router *usersandbox.SandboxRouter
}

type fsGrepArgs struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path"`
	Glob       string `json:"glob"`
	MaxResults int    `json:"max_results"`
}

const defaultGrepMax = 200

func (t *FSGrep) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "Go/RE2 regular expression to search for in file contents."},
    "path": {"type": "string", "description": "Optional file or directory to search inside your workspace container. Defaults to /workspace."},
    "glob": {"type": "string", "description": "Optional glob filter. A plain glob ('*.go') matches the filename; a path glob with ** ('**/*.go') matches the path and crosses directories, like fs_glob."},
    "max_results": {"type": "integer", "minimum": 1, "description": "Optional cap on matching lines returned (default 200)."}
  },
  "required": ["pattern"]
}`)
	return Spec{
		Name:        "fs_grep",
		Summary:     "Find text inside file contents with a regexp (grep).",
		Description: "Search file CONTENTS across a directory tree with an RE2 regular expression; returns matching lines as `path:line: text`. `pattern` is the regex; optionally restrict to a `path` (file or directory, default workspace root) and a filename `glob` (e.g. `*.go`). Binary files, hidden dot-directories (.git, .cache, …) and node_modules/vendor/__pycache__ are skipped — to search a hidden or vendored tree pass it as the explicit `path`; results cap at max_results (default 200). Use this for content search instead of shell grep so matches come back structured. To find files by NAME use fs_glob; to read a known range use fs_read.",
		Parameters:  params,
		// Deferred: filesystem content-search is a long-tail capability discoverable via
		// tool_search. Keeping only fs_read/fs_write visible trims the manifest and stops the
		// agent defaulting to fs_grep for uploaded-document questions (use document_search).
		Deferred: true,
	}
}

func (t *FSGrep) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a fsGrepArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("fs_grep args: %w", err)
	}
	if strings.TrimSpace(a.Pattern) == "" {
		return ToolResult{}, fmt.Errorf("fs_grep: pattern is required")
	}
	re, err := regexp.Compile(a.Pattern)
	if err != nil {
		return ToolResult{}, fmt.Errorf("fs_grep: invalid pattern: %w", err)
	}
	maxResults := a.MaxResults
	if maxResults <= 0 {
		maxResults = defaultGrepMax
	}
	root, err := boxPathArg("fs_grep", a.Path)
	if err != nil {
		return ToolResult{}, err
	}

	// Pattern compilation, the result cap and the root resolution run BEFORE the route: an invalid
	// regexp is the model's own error and must read as one, not as a box round-trip that failed
	// obscurely.
	boxHandle, routeErr := t.Router.Route(ctx)
	if routeErr != nil {
		return sandboxUnavailableResult("fs_grep", routeErr), nil
	}
	return t.grepInBox(ctx, boxHandle, root, a.Glob, re, maxResults)
}

// grepInBox searches the box's files with the compiled pattern. The whole sweep is one exec
// (boxReadFiles), then every match decision is made here in Go.
func (t *FSGrep) grepInBox(
	ctx context.Context,
	handle usersandbox.BoxHandle,
	root, glob string,
	re *regexp.Regexp,
	maxResults int,
) (ToolResult, error) {
	files, truncated, err := boxReadFiles(ctx, t.Router, handle, root)
	if err != nil {
		return sandboxUnavailableResult("fs_grep", err), nil
	}
	var out []string
	for _, f := range files {
		if glob != "" && !globMatch(glob, f.Rel, pathpkg.Base(f.Rel)) {
			continue
		}
		if looksBinary(f.Content) {
			continue
		}
		grepContent(bytes.NewReader(f.Content), f.Rel, re, maxResults, &out)
		if len(out) >= maxResults {
			break
		}
	}
	if len(out) == 0 {
		return NewResult(ctx, withWalkTruncation("[no matches]", truncated))
	}
	return NewResult(ctx, withWalkTruncation(strings.Join(out, "\n"), truncated))
}

// grepContent is the ONE line-scanning implementation: same RE2 engine, same line numbering, same
// truncation notice, whatever produced the bytes. The caller does the binary check and supplies
// the display path.
func grepContent(r io.Reader, rel string, re *regexp.Regexp, maxResults int, out *[]string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; scanner.Scan(); line++ {
		if re.MatchString(scanner.Text()) {
			*out = append(*out, fmt.Sprintf("%s:%d: %s", rel, line, strings.TrimSpace(scanner.Text())))
			if len(*out) >= maxResults {
				return
			}
		}
	}
	// A line over the 1 MiB scanner cap (or a mid-read error) stops bufio.Scanner
	// with no more tokens; surface it so a partial file scan is never mistaken for
	// an exhaustive one (same "flag, don't silently truncate" contract as the walk
	// budget marker).
	if err := scanner.Err(); err != nil && len(*out) < maxResults {
		*out = append(*out, fmt.Sprintf("%s: [scan stopped: %v — file not fully searched; open it with fs_read]", rel, err))
	}
}
