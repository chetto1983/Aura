package memoryindex

import (
	"fmt"
	"strings"
)

const operationalOverlayHeader = "## Operational Lessons\n\nLessons from past tool errors — apply these proactively:\n\n"

// OperationalLessonsBlock formats a slice of operational lesson Documents into a
// Markdown section ready to inject into the agent system prompt. Returns empty
// string when docs is nil or empty. Total output is capped at maxBytes (pass 0
// for the 5120-byte default defined in US-OP03). Each lesson is rendered as a
// ### heading (when title is non-empty) followed by its body.
func OperationalLessonsBlock(docs []Document, maxBytes int) string {
	if len(docs) == 0 {
		return ""
	}
	if maxBytes <= 0 {
		maxBytes = 5120
	}
	var sb strings.Builder
	sb.WriteString(operationalOverlayHeader)
	for _, d := range docs {
		title := strings.TrimSpace(d.Title)
		body := strings.TrimSpace(d.Body)
		if title == "" && body == "" {
			continue
		}
		var entry strings.Builder
		if title != "" {
			fmt.Fprintf(&entry, "### %s\n\n", title)
		}
		if body != "" {
			fmt.Fprintf(&entry, "%s\n\n", body)
		}
		if sb.Len()+entry.Len() > maxBytes {
			break
		}
		sb.WriteString(entry.String())
	}
	result := strings.TrimRight(sb.String(), "\n")
	// If nothing was added beyond the header, the docs were all blank — return empty.
	if result == strings.TrimRight(operationalOverlayHeader, "\n") {
		return ""
	}
	return result
}
