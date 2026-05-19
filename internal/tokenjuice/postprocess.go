package tokenjuice

import (
	"regexp"
	"strings"
)

// Package-level compiled regexes for the git/status post-processor.
var (
	reGitModified  = regexp.MustCompile(`^\s+modified:\s+(.+)$`)
	reGitDeleted   = regexp.MustCompile(`^\s+deleted:\s+(.+)$`)
	reGitNewFile   = regexp.MustCompile(`^\s+new file:\s+(.+)$`)
	reGitRenamed   = regexp.MustCompile(`^\s+renamed:\s+.+ -> (.+)$`)
	reGitUntracked = regexp.MustCompile(`^\s+\?\?\s+(.+)$`)
	reGitHints     = regexp.MustCompile(`^On branch |^\(use |^no changes|^nothing |^Your branch|^HEAD detached`)
	reGitSectionNSC = regexp.MustCompile(`^Changes not staged for commit:`)
	reGitSectionTC  = regexp.MustCompile(`^Changes to be committed:`)
)

// applyPostProcessor dispatches to a family-specific post-processor keyed by rule ID.
// Returns lines unchanged for rules that have no post-processor.
func applyPostProcessor(ruleID string, lines []string) []string {
	if ruleID == "git/status" {
		return rewriteGitStatusLines(lines)
	}
	return lines
}

// rewriteGitStatusLines transforms raw `git status` output into compact notation.
// It removes hint/noise lines, rewrites section headers, and rewrites per-file status
// lines from `\tmodified: path` → `M: path` form (porcelain-friendly, tokens-friendly).
func rewriteGitStatusLines(lines []string) []string {
	result := make([]string, 0, len(lines))
	for _, l := range lines {
		// Drop hint/noise lines.
		if reGitHints.MatchString(l) {
			continue
		}
		// Rewrite section headers.
		if reGitSectionNSC.MatchString(l) {
			result = append(result, "Changes not staged:")
			continue
		}
		if reGitSectionTC.MatchString(l) {
			result = append(result, "Changes to commit:")
			continue
		}
		// Rewrite file status lines.
		if m := reGitModified.FindStringSubmatch(l); m != nil {
			result = append(result, "M: "+strings.TrimSpace(m[1]))
			continue
		}
		if m := reGitDeleted.FindStringSubmatch(l); m != nil {
			result = append(result, "D: "+strings.TrimSpace(m[1]))
			continue
		}
		if m := reGitNewFile.FindStringSubmatch(l); m != nil {
			result = append(result, "A: "+strings.TrimSpace(m[1]))
			continue
		}
		if m := reGitRenamed.FindStringSubmatch(l); m != nil {
			result = append(result, "R: "+strings.TrimSpace(m[1]))
			continue
		}
		if m := reGitUntracked.FindStringSubmatch(l); m != nil {
			result = append(result, "?? "+strings.TrimSpace(m[1]))
			continue
		}
		result = append(result, l)
	}
	return result
}
