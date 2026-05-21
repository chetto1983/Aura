package wiki

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

// HeadingMatch describes one H2 or H3 heading found in a markdown body.
// ByteStart is the byte offset of the first '#' on the heading line.
// ByteEnd is the byte offset of the last character of the heading text
// (before the line terminator), exclusive.
type HeadingMatch struct {
	Level     int
	Text      string
	ByteStart int // byte offset of the heading line start
	ByteEnd   int // byte offset just past the heading line content (before newline)
}

// SubnodeMeta describes a heading-level subnode derived from a wiki page body.
// ByteStart..ByteEnd covers the full heading section (heading line through
// the start of the next same-or-lower-level heading, or end of body).
type SubnodeMeta struct {
	AnchorSlug   string
	HeadingLevel int
	HeadingText  string
	ByteStart    int // inclusive — first byte of the heading line
	ByteEnd      int // exclusive — first byte of the next same/lower-level heading, or len(body)
}

// h23Re matches H2 or H3 markdown heading lines.
var h23Re = regexp.MustCompile(`^(#{2,3})\s+(.+?)\s*$`)

// ParseHeadings scans body line-by-line and returns H2+H3 headings with byte
// offsets. Headings inside ``` fenced blocks are skipped. H1 and H4+ are not
// returned.
func ParseHeadings(body []byte) []HeadingMatch {
	if len(body) == 0 {
		return nil
	}
	var result []HeadingMatch
	inFence := false
	offset := 0
	remaining := body

	for len(remaining) > 0 {
		nl := bytes.IndexByte(remaining, '\n')
		var lineBytes []byte
		var consumed int
		if nl < 0 {
			lineBytes = remaining
			consumed = len(remaining)
		} else {
			lineBytes = remaining[:nl]
			consumed = nl + 1
		}

		// Trim CR for CRLF line endings (Windows).
		lineText := bytes.TrimRight(lineBytes, "\r")
		stripped := bytes.TrimSpace(lineText)

		// Toggle fenced code block state on opening/closing ``` fence.
		if bytes.HasPrefix(stripped, []byte("```")) {
			inFence = !inFence
			offset += consumed
			remaining = remaining[consumed:]
			continue
		}

		if !inFence {
			if m := h23Re.FindSubmatch(lineText); m != nil {
				result = append(result, HeadingMatch{
					Level:     len(m[1]),
					Text:      string(m[2]),
					ByteStart: offset,
					ByteEnd:   offset + len(lineText),
				})
			}
		}

		offset += consumed
		remaining = remaining[consumed:]
	}
	return result
}

// buildSubnodes converts a page body into SubnodeMeta entries, computing
// section byte ranges from the heading sequence. Returns nil when the body
// has no H2/H3 headings.
func buildSubnodes(body []byte) []SubnodeMeta {
	headings := ParseHeadings(body)
	if len(headings) == 0 {
		return nil
	}

	subnodes := make([]SubnodeMeta, 0, len(headings))
	// usedAnchors tracks the number of times each base anchor has appeared
	// so we can suffix duplicates with -2, -3, etc.
	usedAnchors := make(map[string]int, len(headings))

	for i, h := range headings {
		// Section end = start of next heading at same or lower level (or EOF).
		sectionEnd := len(body)
		for j := i + 1; j < len(headings); j++ {
			if headings[j].Level <= h.Level {
				sectionEnd = headings[j].ByteStart
				break
			}
		}

		base := headingAnchorSlug(h.Text)
		usedAnchors[base]++
		anchor := base
		if usedAnchors[base] > 1 {
			anchor = base + "-" + strconv.Itoa(usedAnchors[base])
		}

		subnodes = append(subnodes, SubnodeMeta{
			AnchorSlug:   anchor,
			HeadingLevel: h.Level,
			HeadingText:  h.Text,
			ByteStart:    h.ByteStart,
			ByteEnd:      sectionEnd,
		})
	}
	return subnodes
}

// headingAnchorSlug converts heading text to a slug suitable as an anchor
// in a subnode ID. Uses the same Slug function as page slugs so the format
// is consistent (e.g. "Extracted Entities" → "extracted-entities").
func headingAnchorSlug(text string) string {
	return Slug(strings.TrimSpace(text))
}

// IsSubnodeID reports whether slug represents a subnode (contains '#').
// Page slugs only contain [a-z0-9-], so '#' unambiguously marks a subnode.
// The unexported alias isSubnodeID is preserved for package-internal callers.
func IsSubnodeID(slug string) bool {
	return strings.ContainsRune(slug, '#')
}

func isSubnodeID(slug string) bool { return IsSubnodeID(slug) }

// subnodeParentSlug returns the page slug portion of a subnode ID.
func subnodeParentSlug(id string) string {
	parent, _, _ := strings.Cut(id, "#")
	return parent
}
