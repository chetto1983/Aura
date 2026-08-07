package tools

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	pathpkg "path"
	"regexp"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// searchContent implements search_files' target="content": regex search over box file bytes, one
// exec (boxReadFiles) enumerating and reading, then Go-side scanning for all three output_modes.
// Handing the pattern to the box's own grep would silently swap RE2 for POSIX regex semantics —
// the enumeration moves into the box, the matching stays in Go, exactly like the old fs_grep.
func (t *SearchFiles) searchContent(
	ctx context.Context,
	handle usersandbox.BoxHandle,
	root, glob string,
	re *regexp.Regexp,
	outputMode string,
	limit, offset, contextN int,
) (ToolResult, error) {
	files, truncated, err := boxReadFiles(ctx, t.Router, handle, root)
	if err != nil {
		return sandboxUnavailableResult("search_files", err), nil
	}

	var rendered string
	switch outputMode {
	case "count":
		rendered = renderSearchCounts(files, glob, re, limit, offset)
	case "files_only":
		rendered = renderSearchFilesOnly(files, glob, re, limit, offset)
	default:
		rendered = renderSearchContent(files, glob, re, contextN, limit, offset)
	}
	if rendered == "" {
		rendered = "[no matches]"
	}
	return NewResult(ctx, withWalkTruncation(rendered, truncated))
}

// eligibleContentFiles filters the box's enumerated files down to the ones content search
// actually scans: glob-matched (when a filter was given) and non-binary.
func eligibleContentFiles(files []boxFile, glob string) []boxFile {
	out := make([]boxFile, 0, len(files))
	for _, f := range files {
		if glob != "" && !globMatch(glob, f.Rel, pathpkg.Base(f.Rel)) {
			continue
		}
		if looksBinary(f.Content) {
			continue
		}
		out = append(out, f)
	}
	return out
}

func renderSearchCounts(files []boxFile, glob string, re *regexp.Regexp, limit, offset int) string {
	var lines []string
	for _, f := range eligibleContentFiles(files, glob) {
		n := countMatches(f.Content, re)
		if n > 0 {
			lines = append(lines, fmt.Sprintf("%s: %d", f.Rel, n))
		}
	}
	return strings.Join(paginateStrings(lines, offset, limit), "\n")
}

func renderSearchFilesOnly(files []boxFile, glob string, re *regexp.Regexp, limit, offset int) string {
	var paths []string
	for _, f := range eligibleContentFiles(files, glob) {
		if re.Match(f.Content) {
			paths = append(paths, f.Rel)
		}
	}
	return strings.Join(paginateStrings(paths, offset, limit), "\n")
}

func renderSearchContent(files []boxFile, glob string, re *regexp.Regexp, contextN, limit, offset int) string {
	var lines []string
	// Collect up to offset+limit matches: everything beyond the requested page is dropped anyway,
	// and a pathological corpus should not force scanning every file to completion.
	budget := offset + limit
	for _, f := range eligibleContentFiles(files, glob) {
		if len(lines) >= budget {
			break
		}
		grepFileWithContext(f.Rel, f.Content, re, contextN, budget, &lines)
	}
	return strings.Join(paginateStrings(lines, offset, limit), "\n")
}

// countMatches counts regex matches across a file's lines (one file, one exec's worth of bytes
// already in memory — a line-by-line scan mirrors grepFileWithContext's own iteration so "count"
// and "content" agree on what counts as a match).
func countMatches(content []byte, re *regexp.Regexp) int {
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	n := 0
	for scanner.Scan() {
		if re.Match(scanner.Bytes()) {
			n++
		}
	}
	return n
}

// grepFileWithContext scans one file's lines for re, rendering matches as "path:line: text" and,
// when contextN > 0, up to contextN neighboring lines before/after as "path-line- text" (the
// grep -C / rg dash-separator convention file_operations.py's own context parser expects).
// Overlapping context windows are naturally deduplicated: a line already emitted for one match's
// trailing context is not re-emitted as another match's leading context.
func grepFileWithContext(rel string, content []byte, re *regexp.Regexp, contextN, budget int, out *[]string) {
	var lines []string
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	lastEmitted := -1 // 0-based index of the last line already rendered, so windows do not overlap
	for i, line := range lines {
		if len(*out) >= budget {
			return
		}
		if !re.MatchString(line) {
			continue
		}
		start := max(i-contextN, lastEmitted+1)
		end := min(i+contextN, len(lines)-1)
		for j := start; j <= end && len(*out) < budget; j++ {
			sep := "-"
			if j == i {
				sep = ":"
			}
			*out = append(*out, fmt.Sprintf("%s%s%d%s %s", rel, sep, j+1, sep, strings.TrimRight(lines[j], "\r")))
		}
		lastEmitted = end
	}
}
