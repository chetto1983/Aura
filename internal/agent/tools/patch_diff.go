package tools

import (
	"fmt"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// unifiedDiff renders a standard unified diff between oldContent and newContent, headered on
// path. It is a RENDERER over diffmatchpatch's own line-mode diff (DiffLinesToChars / DiffMain /
// DiffCharsToLines — documented library technique for line-granularity diffing, not a bespoke
// diff algorithm), so patch and write_file report the same diff shape a human `diff -u` would.
//
// Deliberately full-context (one hunk spanning the whole changed range) rather than the
// classic 3-line-context multi-hunk windowing: the fs tools already cap file size at
// AURA_FS_MAX_READ_BYTES (10 MiB default), so a full-context hunk stays small for the edits these
// tools actually make, and skips reimplementing difflib's hunk-splitting for no behavioural gain.
func unifiedDiff(path, oldContent, newContent string) string {
	if oldContent == newContent {
		return ""
	}
	dmp := diffmatchpatch.New()
	a, b, lineArray := dmp.DiffLinesToChars(oldContent, newContent)
	diffs := dmp.DiffCharsToLines(dmp.DiffMain(a, b, false), lineArray)

	var body strings.Builder
	oldNo, newNo := 1, 1
	hunkOldStart, hunkNewStart := 1, 1
	oldCount, newCount := 0, 0
	started := false

	for _, d := range diffs {
		for _, line := range diffLines(d.Text) {
			if !started {
				hunkOldStart, hunkNewStart = oldNo, newNo
				started = true
			}
			switch d.Type {
			case diffmatchpatch.DiffEqual:
				body.WriteString(" " + line + "\n")
				oldCount++
				newCount++
				oldNo++
				newNo++
			case diffmatchpatch.DiffDelete:
				body.WriteString("-" + line + "\n")
				oldCount++
				oldNo++
			case diffmatchpatch.DiffInsert:
				body.WriteString("+" + line + "\n")
				newCount++
				newNo++
			}
		}
	}
	if !started {
		return ""
	}

	var out strings.Builder
	fmt.Fprintf(&out, "--- a/%s\n+++ b/%s\n", path, path)
	fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", hunkOldStart, oldCount, hunkNewStart, newCount)
	out.WriteString(body.String())
	return out.String()
}

// diffLines splits a diffmatchpatch line-mode Diff.Text back into individual lines without the
// trailing newline each line unit carries (DiffLinesToChars treats "line including its \n" as one
// atomic token). Returns nil for an empty Text so an empty diff segment contributes no phantom
// blank line.
func diffLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}
