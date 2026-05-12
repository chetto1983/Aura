package wiki

import "strings"

// ProvenanceMarker returns the canonical inline footnote token Aura
// uses to tag a span of text with its source ID. Format is
// `^[src_xxx]` — Pandoc footnote syntax, also recognized by
// Obsidian's footnotes plugin.
//
// Lives in the wiki package (rather than ingest) because both the
// ingest pipeline AND the runtime wiki_page tool emit these markers.
// Keeping the canonical form in one place prevents the two writers
// from drifting on syntax.
func ProvenanceMarker(sourceID string) string {
	return "^[" + sourceID + "]"
}

// AnnotateProvenance appends a Pandoc-style footnote-shaped marker
// `^[src_xxx]` to the given text. The token is non-rendering in
// most Markdown viewers (Obsidian renders it as a footnote tooltip,
// GitHub renders the raw text) and is grep-friendly for lint
// passes that want to verify every claim in the wiki carries
// provenance.
//
// Idempotent: if the text already ends with the marker for this
// source, returns it unchanged. Whitespace at the end of the input
// is collapsed so the marker glues onto the trailing token cleanly.
func AnnotateProvenance(text, sourceID string) string {
	marker := ProvenanceMarker(sourceID)
	text = strings.TrimRight(text, " \t\n")
	if text == "" {
		return ""
	}
	if strings.HasSuffix(text, marker) {
		return text
	}
	return text + " " + marker
}
