package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// FSGrep searches file contents for a regexp across a directory tree, in-process.
type FSGrep struct{ WorkspaceRoot string }

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
    "path": {"type": "string", "description": "Optional file or directory to search. Defaults to the workspace root."},
    "glob": {"type": "string", "description": "Optional filename glob filter, e.g. '*.go'."},
    "max_results": {"type": "integer", "minimum": 1, "description": "Optional cap on matching lines returned (default 200)."}
  },
  "required": ["pattern"]
}`)
	return Spec{
		Name:        "fs_grep",
		Summary:     "Search file contents with a regexp.",
		Description: "Search file CONTENTS across a directory tree with an RE2 regular expression; returns matching lines as `path:line: text`. `pattern` is the regex; optionally restrict to a `path` (file or directory, default workspace root) and a filename `glob` (e.g. `*.go`). Binary files and .git/node_modules/vendor are skipped; results cap at max_results (default 200). Use this for content search instead of shell grep so matches come back structured. To find files by NAME use fs_glob; to read a known range use fs_read.",
		Parameters:  params,
		Deferred:    false,
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
	root := rootOrDefault(t.WorkspaceRoot, a.Path)

	var out []string
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry: skip, don't abort the whole walk
		}
		if d.IsDir() {
			if skipWalkDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if a.Glob != "" {
			if ok, _ := filepath.Match(a.Glob, d.Name()); !ok {
				return nil
			}
		}
		grepFile(p, root, re, maxResults, &out)
		if len(out) >= maxResults {
			return filepath.SkipAll
		}
		return nil
	})
	if walkErr != nil {
		return ToolResult{}, fmt.Errorf("fs_grep: %w", walkErr)
	}
	if len(out) == 0 {
		return NewResult(ctx, "[no matches]")
	}
	return NewResult(ctx, strings.Join(out, "\n"))
}

func grepFile(path, root string, re *regexp.Regexp, maxResults int, out *[]string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	head := make([]byte, 512)
	n, _ := f.Read(head)
	if looksBinary(head[:n]) {
		return
	}
	if _, err := f.Seek(0, 0); err != nil {
		return
	}

	rel, relErr := filepath.Rel(root, path)
	if relErr != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for line := 1; scanner.Scan(); line++ {
		if re.MatchString(scanner.Text()) {
			*out = append(*out, fmt.Sprintf("%s:%d: %s", rel, line, strings.TrimSpace(scanner.Text())))
			if len(*out) >= maxResults {
				return
			}
		}
	}
}
