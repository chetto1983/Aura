package plugins

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The approval card's bounds. A Telegram message holds 4096 characters; the card stays under
// that with room for the channel's own framing.
const (
	maxQuestionBytes = 3500
	maxFieldRunes    = 120
	maxListedSkills  = 20
	maxListedConns   = 10
	maxListedSkipped = 10
	maxListedNotes   = 5
	truncatedMarker  = "… (truncated)"
)

// Question is the approval card for installing the reviewed plugin, bound to digest. Every
// string the plugin controls is quoted and capped, so no name, version or reason can forge a
// line of the card; a control character anywhere else refuses the card.
func (r Review) Question(digest string) (string, error) {
	skippedItems := make([]string, 0, len(r.SkippedSkills)+len(r.SkippedConnectors))
	for _, s := range append(append(r.SkippedSkills[:0:0], r.SkippedSkills...), r.SkippedConnectors...) {
		skippedItems = append(skippedItems, quote(s.Name)+" — "+quote(s.Reason))
	}
	connectors := make([]string, 0, len(r.Connectors))
	for _, c := range r.Connectors {
		connectors = append(connectors, quote(c.Name)+" ("+c.Placement+"): "+c.target())
	}
	mcpLine := "mcp: enabled"
	if r.MCPDisabled != "" {
		mcpLine = "mcp: disabled — " + quote(r.MCPDisabled)
	}
	notes := make([]string, 0, len(r.Notes))
	for _, n := range r.Notes {
		notes = append(notes, quote(n))
	}
	return card([]string{
		"Approve plugin install?",
		"plugin: " + quote(r.Name) + " " + quote(r.Version),
		"source: " + r.Ref + "@" + r.Commit,
		listLine("skills", skillNames(r.Skills), ", ", maxListedSkills),
		listLine("connectors", connectors, "; ", maxListedConns),
		listLine("skipped", skippedItems, "; ", maxListedSkipped),
		mcpLine,
		listLine("notes", notes, "; ", maxListedNotes),
	}, digest)
}

// RemovalQuestion is the approval card for removing an installed plugin, bound to digest.
func RemovalQuestion(rec Record, deleteData bool, digest string) (string, error) {
	data := "kept"
	if deleteData {
		data = "deleted in your box"
	}
	connectors := make([]string, 0, len(rec.Connectors))
	for _, c := range rec.Connectors {
		connectors = append(connectors, quote(c))
	}
	return card([]string{
		"Approve plugin removal?",
		"plugin: " + quote(rec.Name) + " " + quote(rec.Version),
		"source: " + rec.Ref + "@" + rec.Commit,
		listLine("skills", skillNames(rec.Skills), ", ", maxListedSkills),
		listLine("connectors", connectors, ", ", 0),
		"plugin data: " + data,
	}, digest)
}

// target is what a connector runs, each argv element quoted, or the URL it dials.
func (c Connector) target() string {
	if c.URL != "" {
		return quote(c.URL)
	}
	argv := make([]string, 0, 1+len(c.Args))
	for _, a := range append([]string{c.Command}, c.Args...) {
		argv = append(argv, quote(a))
	}
	return strings.Join(argv, " ")
}

func skillNames(entries []SkillEntry) []string {
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, quote(e.Name))
	}
	return names
}

// quote caps a plugin-controlled string and quotes it, which escapes every control and
// non-printable character, a newline included.
func quote(s string) string {
	if utf8.RuneCountInString(s) > maxFieldRunes {
		s = string([]rune(s)[:maxFieldRunes]) + "…"
	}
	return strconv.Quote(s)
}

// listLine renders `label (n): a, b, and N more`, listing at most limit items (0: all).
func listLine(label string, items []string, sep string, limit int) string {
	head := fmt.Sprintf("%s (%d): ", label, len(items))
	if len(items) == 0 {
		return head + "none"
	}
	if limit > 0 && len(items) > limit {
		return head + strings.Join(items[:limit], sep) + fmt.Sprintf("%sand %d more", sep, len(items)-limit)
	}
	return head + strings.Join(items, sep)
}

// card joins the lines and the closing digest line. A line holding a control character is
// refused; a card over maxQuestionBytes is cut before the digest line, which it always keeps.
func card(lines []string, digest string) (string, error) {
	for i, line := range lines {
		if strings.ContainsFunc(line, unicode.IsControl) {
			return "", fmt.Errorf("approval card line %d holds a control character", i+1)
		}
	}
	last := "\nsha256: " + digest
	if strings.ContainsFunc(digest, unicode.IsControl) {
		return "", fmt.Errorf("approval card digest holds a control character")
	}
	body := strings.Join(lines, "\n")
	if len(body)+len(last) > maxQuestionBytes {
		room := maxQuestionBytes - len(last) - len("\n"+truncatedMarker)
		for room > 0 && !utf8.RuneStart(body[room]) {
			room--
		}
		body = body[:room] + "\n" + truncatedMarker
	}
	return body + last, nil
}
