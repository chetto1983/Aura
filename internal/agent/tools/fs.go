package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Shared seams for the filesystem tools (fs_read/fs_write/fs_edit/fs_grep/fs_glob), which give
// the agent Claude-Code-style file ergonomics over the caller's per-identity BOX — sliceLines,
// globToRegexp/globMatch, looksBinary, the size cap and the walk filters, so each tool file stays
// small and free of duplication.
//
// resolveFSPath, expandHomePath and withinDir stood here until 2026-08-03: HOST path helpers,
// kept alive by document_index alone once the fs_* tools moved into the box. document_index now
// fences the BOX path and stages the bytes out through the sandbox, so nothing in production
// resolved a host path any more and only their own tests still called them. Deleted with those
// tests rather than left as a host-path primitive sitting in the toolbox of a box-only surface.
//
// One consequence is worth stating because it is a behaviour change, not an omission: "~" no
// longer expands for the fs_* tools. boxPathArg refuses it outright — the box's home is a
// different filesystem, and a box path travels through shellQuoteArg, which POSIX sh does not
// expand.

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

// globMatch reports whether a glob matches a file given its relative path and
// basename. A glob containing "**" or "/" is matched against the **-aware
// relative path (fs_glob semantics); a plain glob is matched against the
// basename (the fs_grep filename-filter intent). This unifies the two tools'
// glob behavior so a model reusing "**/*.go" on grep gets matches (AG-046).
func globMatch(glob, relPath, base string) bool {
	if glob == "" {
		return true
	}
	if strings.Contains(glob, "**") || strings.Contains(glob, "/") {
		re, err := globToRegexp(glob)
		if err != nil {
			return false
		}
		return re.MatchString(filepath.ToSlash(relPath))
	}
	if ok, _ := filepath.Match(glob, base); ok {
		return true
	}
	// A single-segment glob like "*.go" should also match against the basename via
	// the **-aware compiler so "*.go" and "**/*.go" behave consistently.
	re, err := globToRegexp(glob)
	if err != nil {
		return false
	}
	return re.MatchString(base)
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

// walkPruneDirs are the well-known dependency/generated stores an fs_grep/fs_glob
// sweep prunes on top of hidden dot-directories. ONE list, two consumers: skipWalkDir
// tests a single name, boxFindPrune compiles the same rule into the find(1) predicate
// that stops the box sweep descending into them at all.
var walkPruneDirs = []string{"node_modules", "vendor", "__pycache__"}

// skipWalkDir reports whether a SUBdirectory (never the explicit search root — the
// sweep excludes the root with -mindepth 1) should be pruned from an fs_grep/fs_glob
// sweep. It prunes hidden dot-directories (the ripgrep/fd default — .git, .cache,
// .local, .npm, .venv, …) and walkPruneDirs. Without this, a home directory's hidden
// caches silently drain the walk budget before the sweep reaches the operator's own
// files: on the appliance /root/.cache alone held ~66k model/download files, so
// `fs_glob test_aura* path:/root` blew the 50k node cap inside .cache and returned
// "[no matches]" for files that plainly existed (shell_exec ls found them). To search
// a hidden or vendored tree, pass it as the explicit `path` root — the root is never
// pruned.
//
// The rule is enforced at the PRODUCER (boxFindPrune, inside the box) so a pruned
// subtree never consumes the node budget, exactly as the deleted host walk's
// filepath.SkipDir did. boxSkippedPath re-applies it while decoding as a second line
// of defence for a Backend whose find lacks the predicate.
func skipWalkDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	return slices.Contains(walkPruneDirs, name)
}

// fs size cap (AG-014 / D-05): fs_read/fs_write/fs_edit stat-then-reject a file
// over AURA_FS_MAX_READ_BYTES so a multi-GB file cannot OOM the process or wedge
// a turn on the shared mini-PC. The default (10 MiB) is ample for code/config;
// the read error suggests offset/limit paging into the same file. Tunable for an
// operator who deliberately handles a larger file.
const (
	envFSMaxReadBytes     = "AURA_FS_MAX_READ_BYTES"
	defaultFSMaxReadBytes = 10 << 20 // 10 MiB
)

func fsMaxReadBytes() int64 {
	if v := strings.TrimSpace(os.Getenv(envFSMaxReadBytes)); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return defaultFSMaxReadBytes
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

// Walk-budget caps (B-16): fs_grep/fs_glob sweep an arbitrary tree, so a `path:/` (or any huge
// directory) could otherwise scan the whole filesystem and wedge a turn — the maxResults cap only
// bounds MATCHES, not files VISITED, so a sweep that matches nothing still touches every file.
// The node-count cap bounds how many files the sweep decodes (boxListFiles / parseBoxFileFrames)
// and the deadline bounds how long the box exec may run; on hitting either the sweep stops early
// and the tool flags walkTruncatedMarker so the model knows the scan was capped, not exhaustive.
// Both are tunable for an operator who deliberately greps a large tree; the defaults are generous
// enough for ordinary project trees.
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

// withWalkTruncation appends the truncation marker to a walk's rendered output
// when the budget stopped it early, so the model treats the result as partial.
func withWalkTruncation(out string, truncated bool) string {
	if !truncated {
		return out
	}
	return out + "\n" + walkTruncatedMarker
}
