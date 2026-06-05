package tools

import (
	"path/filepath"
	"regexp"
	"strings"
)

// Native in-process filesystem tools (fs_read/fs_write/fs_edit/fs_grep/fs_glob)
// give the agent Claude-Code-style file ergonomics with full host access and no
// path fence — for a single trusted operator on their own machine (amendment #50
// / D-15c). resolveFSPath, sliceLines, globToRegexp, and the walk filters are the
// shared seams so each tool file stays small and free of duplication.

// resolveFSPath returns an absolute path as-is and joins a relative path onto the
// workspace root (or leaves it relative to the process cwd when no root is set).
func resolveFSPath(root, p string) string {
	if p == "" || filepath.IsAbs(p) || root == "" {
		return p
	}
	return filepath.Join(root, p)
}

// rootOrDefault picks the search root for the walking tools: the resolved path, or
// the workspace root, or the process cwd.
func rootOrDefault(workspaceRoot, p string) string {
	if strings.TrimSpace(p) != "" {
		return resolveFSPath(workspaceRoot, p)
	}
	if workspaceRoot != "" {
		return workspaceRoot
	}
	return "."
}

// sliceLines returns the 1-based [offset, offset+limit) line window of content.
// offset<=1 starts at the top; limit<=0 runs to the end.
func sliceLines(content string, offset, limit int) string {
	lines := strings.Split(content, "\n")
	start := 0
	if offset > 1 {
		start = offset - 1
	}
	if start > len(lines) {
		start = len(lines)
	}
	end := len(lines)
	if limit > 0 && start+limit < end {
		end = start + limit
	}
	return strings.Join(lines[start:end], "\n")
}

// globToRegexp compiles a glob (supporting **, *, ?) into an anchored regexp over
// forward-slash paths. ** crosses directory separators; * and ? do not.
func globToRegexp(glob string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch c {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				b.WriteString(".*")
				i++
				if i+1 < len(glob) && glob[i+1] == '/' {
					i++
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('$')
	return regexp.Compile(b.String())
}

// skipWalkDir is true for directory names not worth walking for grep/glob (huge,
// mostly binary, or VCS internals).
func skipWalkDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor":
		return true
	}
	return false
}

// looksBinary reports whether the first chunk of b contains a NUL byte (the cheap
// heuristic ripgrep uses to skip binary files).
func looksBinary(b []byte) bool {
	n := min(len(b), 512)
	for i := range n {
		if b[i] == 0 {
			return true
		}
	}
	return false
}
