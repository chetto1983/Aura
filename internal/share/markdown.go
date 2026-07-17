// markdown.go implements the D-07 human-readable export adapter, a pure
// function of Snapshot rendered with strings.Builder — the only in-repo
// precedent for building a large string efficiently is
// internal/agui/content_disposition.go:47-48 (asciiFallbackFilename's
// b.Grow + range-over-runes idiom). There is no Markdown serializer
// anywhere else in this repo to model on; this file follows the OQ4
// signature instead: a method on Snapshot with no other parameter, so a
// redaction fix upstream of Snapshot cannot miss this surface — the same
// D-07 guarantee jsonfmt.go states.
package share

// Markdown renders the D-07 human-readable export.
//
// RED-phase stub (task 2, TDD RED commit): the pre-commit go vet/build hook
// rejects a whole-module non-compiling commit, so this file ships here with
// a deliberately-wrong (but non-panicking) body — every
// TestSnapshotMarkdown*/TestSnapshotFormatsAgree case still fails on a real
// assertion against this sentinel output, not a compile error and not an
// uncaught panic that would abort the rest of the suite. The GREEN commit
// replaces the body with the real strings.Builder implementation.
// longestBacktickRun is declared alongside it so the RED commit's
// format_test.go reference to it also compiles.
func (s Snapshot) Markdown() []byte {
	return []byte("MARKDOWN_NOT_IMPLEMENTED")
}

// longestBacktickRun is the RED-phase stub for the fence-injection helper;
// task 2's GREEN commit implements the real scan.
func longestBacktickRun(text string) int {
	return -1
}
