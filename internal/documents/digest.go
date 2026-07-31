package documents

import (
	"fmt"
	"sort"
	"strings"
)

// A digest answers one question — WHICH file is this — and deliberately not
// "what does it say". That second question used to be the index's job, and it
// cost 118 chunks and 118 vectors for one spreadsheet while still scoring 0% on
// every aggregate ("how many customers in Torino"), because the answer is a
// property of the whole set and lives in no passage. Since document_open now
// hands the agent the real file, the index only has to route.
//
// So a digest is what a person reads off a filing-cabinet label: the sheets and
// their column headers with row counts, or the heading outline of a report. It
// is small enough to keep every document's in one Postgres row and rank with
// ts_rank, and specific enough that "the customer list with the sales reps"
// picks the right file out of a library.
const (
	maxDigestChars      = 4000
	maxDigestHeadings   = 40
	maxDigestSampleCols = 12
)

// BuildDigest renders the routing description of an extracted document.
func BuildDigest(doc ExtractedDocument) string {
	var b strings.Builder
	writeLine(&b, describeSource(doc))

	if sheets := tabularOutline(doc.Chunks); len(sheets) > 0 {
		writeLine(&b, "Tabular data.")
		for _, sheet := range sheets {
			writeLine(&b, sheet)
		}
	}
	if headings := headingOutline(doc.Chunks); len(headings) > 0 {
		writeLine(&b, "Sections: "+strings.Join(headings, " · "))
	}
	return truncateDigest(b.String())
}

func writeLine(b *strings.Builder, line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	b.WriteString(line)
	b.WriteString("\n")
}

func describeSource(doc ExtractedDocument) string {
	parts := []string{}
	if name := strings.TrimSpace(doc.FileName); name != "" {
		parts = append(parts, name)
	}
	if title := strings.TrimSpace(doc.Title); title != "" && title != strings.TrimSpace(doc.FileName) {
		parts = append(parts, title)
	}
	if mime := strings.TrimSpace(doc.MIMEType); mime != "" {
		parts = append(parts, mime)
	}
	if doc.SizeBytes > 0 {
		parts = append(parts, fmt.Sprintf("%d bytes", doc.SizeBytes))
	}
	return strings.Join(parts, " — ")
}

// tabularOutline describes each sheet by its COLUMN HEADERS and row count. The
// headers are the words a person actually searches by ("ragione sociale",
// "venditore"), and the markitdown sidecar repeats the header row in every
// chunk, so the first chunk of a sheet always carries them.
func tabularOutline(chunks []Chunk) []string {
	type sheet struct {
		header   string
		lastRow  int
		firstRow int
	}
	order := []string{}
	seen := map[string]*sheet{}
	for _, chunk := range chunks {
		if chunk.Kind != "rows" {
			continue
		}
		name := chunk.Locator.Sheet
		current, ok := seen[name]
		if !ok {
			current = &sheet{header: headerLine(chunk.Text), firstRow: chunk.Locator.RowStart}
			seen[name] = current
			order = append(order, name)
		}
		if chunk.Locator.RowEnd > current.lastRow {
			current.lastRow = chunk.Locator.RowEnd
		}
	}

	out := make([]string, 0, len(order))
	for _, name := range order {
		current := seen[name]
		label := name
		if label == "" {
			label = "table"
		}
		line := "  " + label
		if rows := current.lastRow - current.firstRow + 1; rows > 0 {
			line += fmt.Sprintf(" (%d rows)", rows)
		}
		if current.header != "" {
			line += ": " + current.header
		}
		out = append(out, line)
	}
	return out
}

// headerLine pulls the markdown header row out of a table chunk and renders its
// cells as plain words. A chunk that is not a markdown table yields nothing
// rather than a guess.
func headerLine(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := []string{}
		for cell := range strings.SplitSeq(strings.Trim(line, "|"), "|") {
			if cell = strings.TrimSpace(cell); cell != "" {
				cells = append(cells, cell)
			}
		}
		if len(cells) == 0 {
			return ""
		}
		if len(cells) > maxDigestSampleCols {
			cells = append(cells[:maxDigestSampleCols], "…")
		}
		return strings.Join(cells, ", ")
	}
	return ""
}

// headingOutline collects the document's own table of contents. Prose carries
// one, and it is the cheapest possible description of what a report covers.
func headingOutline(chunks []Chunk) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, chunk := range chunks {
		if chunk.Kind == "rows" {
			continue
		}
		for _, heading := range chunk.HeadingPath {
			heading = strings.TrimSpace(heading)
			if heading == "" || seen[heading] {
				continue
			}
			seen[heading] = true
			out = append(out, heading)
		}
		if section := strings.TrimSpace(chunk.Locator.Section); section != "" && !seen[section] {
			seen[section] = true
			out = append(out, section)
		}
	}
	if len(out) > maxDigestHeadings {
		out = out[:maxDigestHeadings]
	}
	return out
}

// truncateDigest bounds the row at a whole line: half a heading is noise in a
// ranking vector, and a digest is a label, not a document.
func truncateDigest(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxDigestChars {
		return text
	}
	cut := strings.LastIndex(text[:maxDigestChars], "\n")
	if cut <= 0 {
		cut = maxDigestChars
	}
	return strings.TrimSpace(text[:cut])
}

// DigestHit is one document matched by digest search, with the id document_open
// takes. It carries no passage: the caller opens the file.
//
// Tags travel with it because they are the one part of a document's description
// a PERSON wrote. The cockpit makes them editable in the details drawer and
// shows them on every file row, so an operator who tagged three spreadsheets
// "fatturato 2026" has already told Aura how they think of those files — a
// signal no extractor produces. They are weighted B in the ranking vector,
// between the title and the generated digest.
type DigestHit struct {
	DocumentID string   `json:"document_id"`
	Title      string   `json:"title"`
	Tags       []string `json:"tags,omitempty"`
	Digest     string   `json:"digest"`
	Rank       float64  `json:"rank"`
}

// SortDigestHits orders by rank descending, then title, so a tie is stable
// rather than whatever order the database happened to return.
func SortDigestHits(hits []DigestHit) {
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].Rank != hits[j].Rank {
			return hits[i].Rank > hits[j].Rank
		}
		return hits[i].Title < hits[j].Title
	})
}
