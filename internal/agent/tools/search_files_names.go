package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// searchFileNames implements search_files' target="files": glob match over the box's file tree,
// sorted by modification time (newest first) — hermes' own "also use this instead of ls" contract
// (file_tools.py's SEARCH_FILES_SCHEMA description).
func (t *SearchFiles) searchFileNames(
	ctx context.Context,
	handle usersandbox.BoxHandle,
	root, pattern string,
	limit, offset int,
) (ToolResult, error) {
	re, err := globToRegexp(pattern)
	if err != nil {
		return ToolResult{}, fmt.Errorf("search_files: invalid glob pattern: %w", err)
	}
	relPaths, truncated, err := boxListFilesByMtime(ctx, t.Router, handle, root)
	if err != nil {
		return sandboxUnavailableResult("search_files", err), nil
	}
	var matched []string
	for _, rel := range relPaths {
		if re.MatchString(rel) {
			matched = append(matched, rel)
		}
	}
	page := paginateStrings(matched, offset, limit)
	if len(page) == 0 {
		return NewResult(ctx, withWalkTruncation("[no matches]", truncated))
	}
	return NewResult(ctx, withWalkTruncation(strings.Join(page, "\n"), truncated))
}

// boxListFilesByMtime enumerates regular files under root INSIDE the box, newest-first, using GNU
// find's `-printf '%T@ %p\n'` (mtime as seconds.fraction since epoch) piped through `sort -rn` —
// the sort key never leaves the shell pipeline, so callers only need the resulting path ORDER.
// Shares boxListFiles' prune rules and node cap so the two enumerations never disagree on what
// counts as "in scope" — only the presence of a sort key and the sort itself differ.
func boxListFilesByMtime(
	ctx context.Context,
	router *usersandbox.SandboxRouter,
	handle usersandbox.BoxHandle,
	root string,
) (relPaths []string, truncated bool, err error) {
	nodeCap := fsWalkNodeCap()
	cmd := fmt.Sprintf(
		"find %s -mindepth 1 %s -o -type f -printf '%%T@ %%p\\n' 2>/dev/null | sort -rn | head -n %d",
		ShellQuoteArg(root), boxFindPrune(), nodeCap+1,
	)
	ctx, cancel := boxSweepDeadline(ctx)
	defer cancel()
	res, execErr := router.Exec(ctx, handle, usersandbox.ExecRequest{Command: cmd})
	if execErr != nil {
		return nil, false, execErr
	}
	lines := strings.Split(strings.Trim(string(res.Stdout), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, false, nil
	}
	if len(lines) > nodeCap {
		truncated = true
		lines = lines[:nodeCap]
	}
	for _, line := range lines {
		_, p, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		rel, found := boxRelPath(root, p)
		if !found || boxSkippedPath(rel) {
			continue
		}
		relPaths = append(relPaths, rel)
	}
	return relPaths, truncated, nil
}
