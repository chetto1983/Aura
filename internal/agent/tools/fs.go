package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Native in-process filesystem tools (fs_read/fs_write/fs_edit/fs_grep/fs_glob)
// give the agent Claude-Code-style file ergonomics with full host access and no
// path fence — for a single trusted operator on their own machine (amendment #50
// / D-15c). resolveFSPath, sliceLines, globToRegexp, and the walk filters are the
// shared seams so each tool file stays small and free of duplication.

// resolveFSPath returns an absolute path as-is and joins a relative path onto the
// workspace root (or leaves it relative to the process cwd when no root is set).
func resolveFSPath(root, p string) string {
	p = expandHomePath(p)
	root = expandHomePath(root)
	if p == "" || filepath.IsAbs(p) || root == "" {
		return p
	}
	return filepath.Join(root, p)
}

// expandHomePath gives native fs_* tools the same "~" ergonomics the host shell
// already has. Without this, shell_exec can read ~/.aura/... while fs_read treats
// "~" as a literal workspace child, causing pointless fallback shell calls.
func expandHomePath(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

// deniedSkillsWrite returns a redirect error when a resolved write/edit target is
// inside the skills library (skillsDir). The full-host no-fence policy (#50 /
// D-15c) stands everywhere else; this is the surgical exception (#54 / D-43) that
// stops a direct file write from bypassing the gated `skill` authoring flow
// (create→pending→approve). An empty skillsDir disables the fence (unit tests and
// the pool-free manifest paths that construct the tools without a config).
func deniedSkillsWrite(skillsDir, resolved, tool string) error {
	if !withinDir(skillsDir, resolved) {
		return nil
	}
	return fmt.Errorf("%s: %s is inside the skills library; author skills through the gated `skill` tool "+
		"(action=create/update/delete), not direct file writes", tool, resolved)
}

// withinDir reports whether target is dir itself or a path nested inside it,
// comparing cleaned absolute paths via filepath.Rel (a "../" prefix means
// target escaped dir). An empty dir or an unresolvable path → false.
func withinDir(dir, target string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	da, err1 := filepath.Abs(dir)
	ta, err2 := filepath.Abs(target)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(da, ta)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
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

// Walk-budget caps (B-16): fs_grep/fs_glob walk an arbitrary tree, so a `path:/`
// (or any huge directory) could otherwise scan the whole disk and wedge a turn —
// the maxResults cap only bounds MATCHES, not nodes VISITED, so a walk that
// matches nothing still touches every file. The node-count and deadline caps
// below bound the walk itself; on hitting either the walk stops early and the
// tool flags walkTruncatedMarker so the model knows the scan was capped, not
// exhaustive. Both are tunable for an operator who deliberately greps a large
// tree; the defaults are generous enough for ordinary project trees.
const (
	envFSWalkNodeCap   = "AURA_FS_WALK_NODE_CAP"
	envFSWalkTimeoutMs = "AURA_FS_WALK_TIMEOUT_MS"

	defaultFSWalkNodeCap  = 50000
	defaultFSWalkDeadline = 5 * time.Second

	walkTruncatedMarker = "[walk truncated: hit the node/time budget — results are partial, not exhaustive; narrow `path` or `glob`]"
)

func fsWalkNodeCap() int {
	if v := strings.TrimSpace(os.Getenv(envFSWalkNodeCap)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultFSWalkNodeCap
}

func fsWalkDeadline() time.Duration {
	if v := strings.TrimSpace(os.Getenv(envFSWalkTimeoutMs)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return defaultFSWalkDeadline
}

// walkBudget bounds a directory walk by a node-count cap AND a deadline. The
// caller increments it once per visited entry (via step) and checks exceeded()
// to stop early. It also honors an already-threaded ctx deadline/cancellation so
// a turn-level timeout aborts the walk too. nodes is single-goroutine (WalkDir is
// sequential), so no atomics are needed.
type walkBudget struct {
	ctx      context.Context
	nodeCap  int
	deadline time.Time
	nodes    int
}

func newWalkBudget(ctx context.Context) *walkBudget {
	if ctx == nil {
		ctx = context.Background()
	}
	deadline := time.Now().Add(fsWalkDeadline())
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	return &walkBudget{ctx: ctx, nodeCap: fsWalkNodeCap(), deadline: deadline}
}

// step counts one visited entry and reports whether the budget is now exhausted.
func (b *walkBudget) step() bool {
	b.nodes++
	return b.exceeded()
}

// exceeded reports whether the node cap, the deadline, or ctx cancellation has
// been hit. The node-count check (cheap) is tried before the clock read.
func (b *walkBudget) exceeded() bool {
	if b.nodes >= b.nodeCap {
		return true
	}
	if b.ctx.Err() != nil {
		return true
	}
	return !time.Now().Before(b.deadline)
}

// withWalkTruncation appends the truncation marker to a walk's rendered output
// when the budget stopped it early, so the model treats the result as partial.
func withWalkTruncation(out string, truncated bool) string {
	if !truncated {
		return out
	}
	return out + "\n" + walkTruncatedMarker
}
