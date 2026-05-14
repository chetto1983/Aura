package ocr

import (
	"fmt"
	"strings"
)

// RenderMeta is the per-source header for the generated ocr.md.
type RenderMeta struct {
	SourceID string
	Filename string
	Model    string // optional override; falls back to OCRResponse.Model
}

// RenderMarkdown serializes an OCR response into the PDR §4 ocr.md layout:
//
//	# Source OCR: <filename>
//
//	Source ID: <source_id>
//	Model: <model>
//
//	## Page 1
//	...
//
// Empty pages are kept (their header is emitted) so the page numbering
// matches the source PDF; this keeps later citations like "page 7" stable
// even when OCR returned no text for that page.
func RenderMarkdown(meta RenderMeta, resp OCRResponse) string {
	var sb strings.Builder
	filename := strings.TrimSpace(meta.Filename)
	if filename == "" {
		filename = "(unknown)"
	}
	fmt.Fprintf(&sb, "# Source OCR: %s\n\n", filename)
	if meta.SourceID != "" {
		fmt.Fprintf(&sb, "Source ID: %s\n", meta.SourceID)
	}
	model := meta.Model
	if model == "" {
		model = resp.Model
	}
	if model != "" {
		fmt.Fprintf(&sb, "Model: %s\n", model)
	}
	sb.WriteString("\n")

	for i, page := range resp.Pages {
		// API index is 0-based per Mistral docs; humans expect 1-based.
		display := page.Index + 1
		if page.Index == 0 && i > 0 {
			// Defensive: some servers return index=0 for every page. Fall
			// back to the slice position so numbering stays unique.
			display = i + 1
		}
		fmt.Fprintf(&sb, "## Page %d\n\n", display)

		if h := strings.TrimSpace(page.Header); h != "" {
			fmt.Fprintf(&sb, "*Header:* %s\n\n", h)
		}

		body := substituteTablePlaceholders(strings.TrimRight(page.Markdown, "\n"), page.Tables)
		if body != "" {
			sb.WriteString(body)
			sb.WriteString("\n")
		}

		if f := strings.TrimSpace(page.Footer); f != "" {
			fmt.Fprintf(&sb, "\n*Footer:* %s\n", f)
		}

		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n") + "\n"
}

// substituteTablePlaceholders replaces Mistral's `[<id>](<id>)` table
// references inside page markdown with the actual table content from the
// page's Tables array. Without this, table content lives only in
// ocr.json and never reaches ocr.md.
func substituteTablePlaceholders(md string, tables []Table) string {
	for _, t := range tables {
		if t.ID == "" || t.Content == "" {
			continue
		}
		placeholder := fmt.Sprintf("[%s](%s)", t.ID, t.ID)
		replacement := "\n\n" + strings.TrimSpace(t.Content) + "\n\n"
		md = strings.ReplaceAll(md, placeholder, replacement)
	}
	return md
}
