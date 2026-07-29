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

import (
	"strconv"
	"strings"
)

// mdRoleHeading renders the human-facing label for a snapshot turn's role.
// Only "user" and "assistant" ever reach SnapshotTurn.Role — projectTurns'
// allowlist (redact.go) drops every other role before BuildSnapshot
// returns — so the default arm exists only to degrade an unrecognized
// future role to its raw string instead of panicking; it is not a path any
// current caller can reach. The role strings are hardcoded here rather
// than imported from the message package so this file never references
// that package at all — the grep-gated proof, alongside jsonfmt.go, that
// neither adapter can reach the raw conversation history type.
func mdRoleHeading(role string) string {
	switch role {
	case "user":
		return "You"
	case "assistant":
		return "Assistant"
	default:
		return role
	}
}

// longestBacktickRun scans text for the longest consecutive run of
// backtick runes. Markdown wraps turn text in a fenced code block below;
// see fenceForText's doc for why the emitted fence must be STRICTLY LONGER
// than this value, not merely as long.
func longestBacktickRun(text string) int {
	longest, current := 0, 0
	for _, r := range text {
		if r == '`' {
			current++
			if current > longest {
				longest = current
			}
			continue
		}
		current = 0
	}
	return longest
}

// mdFenceMinLen is the standard Markdown minimum fence length.
const mdFenceMinLen = 3

// fenceForText returns a backtick fence for wrapping text as a code block,
// strictly longer than the longest backtick run text itself contains (with
// the 3-backtick floor). A turn whose text contains a fence would otherwise
// let that embedded run terminate the block EARLY, and the remainder of
// the turn would render as document structure instead of as content — a
// formatting-integrity bug (not an XSS one: the public share page renders
// the Snapshot directly and never this Markdown), but the exported .md is
// a file a human opens and reads as-is, so a truncated turn is a real
// defect worth guarding against structurally rather than hoping no turn
// ever contains a fence.
func fenceForText(text string) string {
	n := max(longestBacktickRun(text)+1, mdFenceMinLen)
	return strings.Repeat("`", n)
}

// Markdown renders the D-07 human-readable export: a title heading, then
// one numbered section per turn (role heading, a tool-provenance line when
// ToolNames is non-empty, the turn text in a fence sized per
// fenceForText), then a trailing artifacts section when Artifacts is
// non-empty. It takes no parameter beyond the receiver — the same
// signature guarantee JSON() states, and for the same reason: Snapshot has
// no field able to hold a tool argument, a tool result, or a filesystem
// path, so this function physically cannot emit one no matter how the
// Markdown is assembled.
func (s Snapshot) Markdown() []byte {
	var b strings.Builder
	b.Grow(64 + len(s.Turns)*128 + len(s.Artifacts)*32)

	b.WriteString("# ")
	b.WriteString(s.Title)
	b.WriteByte('\n')

	for i, turn := range s.Turns {
		b.WriteString("\n## ")
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString(". ")
		b.WriteString(mdRoleHeading(turn.Role))
		b.WriteByte('\n')

		if len(turn.ToolNames) > 0 {
			b.WriteString("\n_Tools used: ")
			b.WriteString(strings.Join(turn.ToolNames, ", "))
			b.WriteString("_\n")
		}

		fence := fenceForText(turn.Text)
		b.WriteByte('\n')
		b.WriteString(fence)
		b.WriteByte('\n')
		b.WriteString(turn.Text)
		b.WriteByte('\n')
		b.WriteString(fence)
		b.WriteByte('\n')
	}

	if len(s.Artifacts) > 0 {
		b.WriteString("\n## Artifacts\n\n")
		for _, a := range s.Artifacts {
			b.WriteString("- ")
			b.WriteString(a.FileName)
			b.WriteString(" (")
			b.WriteString(strconv.FormatInt(a.SizeBytes, 10))
			b.WriteString(" bytes)\n")
		}
	}

	return []byte(b.String())
}
